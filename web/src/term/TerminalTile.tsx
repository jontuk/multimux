import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { ClipboardAddon } from "@xterm/addon-clipboard";
import "@xterm/xterm/css/xterm.css";
import { apiFetch, wsURL } from "../api";
import type { Server } from "../servers";
import type { Session } from "../grid/types";
import { clientId } from "../clientId";
import { encodeResize, parseServerText } from "./protocol";
import { isReflowHeld, onReflowRelease } from "./reflowGate";
import { wrapAwareLinkProvider } from "./links";
import { installTouchScroll } from "./touchScroll";
import { selectedText } from "./wrap";

export type TerminalSizePolicy = "follow-input" | "passive";

export type TerminalHandle = {
  input(data: string): boolean;
  paste(data: string): boolean;
  focus(): void;
  setFontSize(size: number): void;
  fit(): void;
};

const inertTerminalOperations: TerminalHandle = {
  input: () => false,
  paste: () => false,
  focus: () => {},
  setFontSize: () => {},
  fit: () => {},
};

type Props = {
  server: Server;
  sessionId: number;
  onClose: () => void;
  autoFocus?: boolean;
  sizePolicy?: TerminalSizePolicy;
  controlsSlot?: HTMLElement | null;
  touchScrollback?: boolean;
};

// Mirrors xterm.js's own isMac (common/Platform.ts) — it gates selection
// behaviour on exactly this list, so shiftDragCapture must agree with it or
// the bypass fires on a platform where Shift already works.
const isMac = ["Macintosh", "MacIntel", "MacPPC", "Mac68K"].includes(navigator.platform);

// "offline" retries automatically; "exited", "missing", and "auth" are
// terminal — the loop stops and the overlay offers dismiss/reconnect.
type ConnState = "connecting" | "open" | "offline" | "exited" | "missing" | "auth";

// The browser WS API hides the HTTP status of a failed upgrade, so ask the
// sessions API which failure this is (same trick as useEvents' classify).
async function classifyClose(server: Server, sessionId: number): Promise<"retry" | "exited" | "missing" | "auth"> {
  try {
    const res = await apiFetch(server, "/api/sessions");
    if (res.status === 401 || res.status === 403) return "auth";
    if (!res.ok) return "retry";
    const sessions = (await res.json()) as Session[];
    const sess = sessions.find((s) => s.id === sessionId);
    if (!sess) return "missing";
    return sess.status === "running" ? "retry" : "exited";
  } catch {
    return "retry"; // daemon unreachable — transient
  }
}

const TerminalTile = forwardRef<TerminalHandle, Props>(function TerminalTile(
  { server, sessionId, onClose, autoFocus, sizePolicy = "follow-input", controlsSlot, touchScrollback = false },
  ref,
) {
  const containerRef = useRef<HTMLDivElement>(null);
  const fitSharedSizeRef = useRef<() => void>(() => {});
  const fontSizeRef = useRef(13);
  const operationsRef = useRef<TerminalHandle>(inertTerminalOperations);
  useImperativeHandle(
    ref,
    () => ({
      input: (data) => operationsRef.current.input(data),
      paste: (data) => operationsRef.current.paste(data),
      focus: () => operationsRef.current.focus(),
      setFontSize: (size) => operationsRef.current.setFontSize(size),
      fit: () => operationsRef.current.fit(),
    }),
    [],
  );
  // Fire the initial focus once; reconnects (retryNonce/url) re-run the effect
  // but must not steal focus back from wherever the user has since moved.
  const didAutoFocus = useRef(false);
  // Capture the mount-time value so autoFocus isn't an effect dep — only the
  // initial focus matters, and a later prop flip must not reconnect the term.
  const autoFocusRef = useRef(autoFocus);
  const [state, setState] = useState<ConnState>("connecting");
  // Bumped by the auth overlay's reconnect button to restart the effect.
  const [retryNonce, setRetryNonce] = useState(0);

  // Depend on the URL string, not the server object: listServers() returns
  // fresh objects each render, but the string only changes when it matters.
  // client= keys window-size ownership to this browser, so our reconnects don't
  // hand the shared tmux window to another machine's next passive resize.
  const url = wsURL(server, `/ws/pty/${sessionId}`, {
    client: clientId(),
    ...(sizePolicy === "passive" ? { size: "passive" } : {}),
  });
  const serverRef = useRef(server);
  useEffect(() => {
    serverRef.current = server;
  });

  useEffect(() => {
    const term = new Terminal({
      scrollback: 0, // tmux owns scrollback (mouse mode)
      fontSize: fontSizeRef.current,
      // On Mac, xterm.js only bypasses app mouse-mode for Option+drag (never
      // Shift — that's hardcoded Linux/Windows-only in SelectionService).
      // selectDragCapture below drives that flag, so this must stay on.
      macOptionClickForcesSelection: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new ClipboardAddon()); // OSC 52: tmux copy-mode yank → system clipboard
    // Our own provider, not WebLinksAddon: it joins rows the way tmux wraps
    // them, which the addon can't see (see wrap.ts).
    const linkProvider = term.registerLinkProvider(wrapAwareLinkProvider(term));
    term.open(containerRef.current!);
    const disposeTouchScroll = touchScrollback
      ? installTouchScroll(term.element!, () => term.modes.mouseTrackingMode !== "none")
      : () => {};
    if (autoFocusRef.current && !didAutoFocus.current) {
      didAutoFocus.current = true;
      term.focus();
    }
    const encoder = new TextEncoder();

    let ws: WebSocket | null = null;
    let closed = false;
    let backoff = 500;
    let reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;

    // active claims ownership of the shared tmux window size (see Arbiter), so
    // only real interaction with THIS terminal may set it. Document-level focus
    // is not enough: a dormant machine's PWA still reports visible+focused, and
    // every reconnect there would re-claim the window and shrink it under the
    // person actually typing. Passive resizes still size our own attach PTY,
    // and are applied to the window when we already own it.
    function sendResize(active = false) {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(encodeResize(term.cols, term.rows, active));
      }
    }

    function claimForInteraction() {
      if (sizePolicy === "follow-input") sendResize(true);
    }

    // A tile the dir filter has hidden is display:none, so it has no box to
    // measure. Fitting to it would size the terminal — and, through
    // sendResize, this connection's PTY — to nothing, and that size would
    // still be there when the tile came back. The ResizeObserver fires again
    // the moment it does, which is when the refit belongs.
    function fitToBox() {
      const el = containerRef.current;
      if (!el || el.clientWidth === 0 || el.clientHeight === 0) return;
      fit.fit();
    }

    let pasteAccepted: boolean | null = null;

    function sendInput(data: string) {
      if (ws?.readyState !== WebSocket.OPEN) {
        if (pasteAccepted !== null) pasteAccepted = false;
        return false;
      }
      try {
        ws.send(encoder.encode(data));
        return true;
      } catch {
        if (pasteAccepted !== null) pasteAccepted = false;
        return false;
      }
    }

    operationsRef.current = {
      input(data) {
        if (ws?.readyState !== WebSocket.OPEN) return false;
        claimForInteraction();
        return sendInput(data);
      },
      paste(data) {
        if (ws?.readyState !== WebSocket.OPEN) return false;
        claimForInteraction();
        pasteAccepted = true;
        try {
          // xterm adds bracketed-paste markers only when the foreground app
          // enabled that mode. Without it, embedded newlines remain live input.
          term.paste(data);
          return pasteAccepted;
        } catch {
          return false;
        } finally {
          pasteAccepted = null;
        }
      },
      focus: () => term.focus(),
      setFontSize(size) {
        fontSizeRef.current = size;
        term.options.fontSize = size;
        fitToBox();
        sendResize();
      },
      fit() {
        fitToBox();
        sendResize();
      },
    };

    fitSharedSizeRef.current = () => {
      fitToBox();
      sendResize(true);
    };

    function connect() {
      if (closed) return;
      setState("connecting");
      ws = new WebSocket(url);
      ws.binaryType = "arraybuffer";
      ws.onopen = () => {
        setState("open");
        backoff = 500;
        fitToBox();
        sendResize();
      };
      ws.onmessage = (ev) => {
        if (closed) return;
        if (ev.data instanceof ArrayBuffer) {
          term.write(new Uint8Array(ev.data));
        } else if (parseServerText(ev.data)?.type === "exit") {
          setState("exited");
          closed = true;
          ws?.close();
        }
      };
      ws.onclose = async () => {
        if (closed) return;
        setState("offline");
        const kind = await classifyClose(serverRef.current, sessionId);
        if (closed) return;
        if (kind === "retry") {
          reconnectTimeoutId = setTimeout(connect, backoff);
          backoff = Math.min(backoff * 2, 10000);
        } else {
          closed = true;
          setState(kind);
        }
      };
    }
    connect();

    // Shift+Enter: xterm.js has no CSI-u encoder, so it emits a bare \r that is
    // indistinguishable from Enter. Intercept at the DOM capture phase — before
    // the event reaches xterm.js's textarea — and send the extended-key
    // sequence ourselves. tmux forwards it via terminal-features xterm*:extkeys.
    let suppressNextCR = false;
    const captureContainer = containerRef.current!;
    const shiftEnterCapture = (e: KeyboardEvent) => {
      claimForInteraction();
      if (e.key === "Enter" && e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault();
        e.stopPropagation();
        e.stopImmediatePropagation();
        // Belt-and-braces: if the keypress still produces onData, drop that \r.
        suppressNextCR = true;
        setTimeout(() => {
          suppressNextCR = false;
        }, 0);
        if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode("\x1b[13;2u"));
      }
    };
    captureContainer.addEventListener("keydown", shiftEnterCapture, true);

    // Browser gestures are the evidence of a human at this tile. xterm's
    // onData cannot provide that evidence because it also carries automatic
    // terminal-protocol replies generated while parsing server output.
    const claimInteractionCapture = () => claimForInteraction();
    captureContainer.addEventListener("paste", claimInteractionCapture, true);
    captureContainer.addEventListener("pointerdown", claimInteractionCapture, true);
    captureContainer.addEventListener("wheel", claimInteractionCapture, true);

    // Drag-to-select without a modifier, Shift+drag to reach tmux. tmux mouse
    // mode normally owns click-drag; xterm.js's escape hatch
    // (shouldForceSelection) is Option-only on Mac and Shift-only elsewhere,
    // both read off the mousedown. Shadow that property on the instance in the
    // capture phase, before xterm.js's bubble-phase listeners see it, and set
    // it from the inverse of Shift: plain drag selects, Shift+drag (pane
    // focus/resize, copy-mode drag) passes through to tmux.
    const forceProp = isMac ? "altKey" : "shiftKey";
    const selectDragCapture = (e: MouseEvent) => {
      if (e.button !== 0) return;
      Object.defineProperty(e, forceProp, { value: !e.shiftKey, configurable: true });
    };
    captureContainer.addEventListener("mousedown", selectDragCapture, true);

    // Copy-on-select, so a drag is the whole gesture — no Cmd/Ctrl+C. Deferred
    // to the end of the drag: onSelectionChange fires per mousemove, and each
    // write would race the last. Empty selections are skipped so clearing one
    // doesn't wipe the clipboard.
    let copyTimeoutId: ReturnType<typeof setTimeout> | null = null;
    const selectionSub = term.onSelectionChange(() => {
      if (copyTimeoutId) clearTimeout(copyTimeoutId);
      copyTimeoutId = setTimeout(() => {
        const text = selectedText(term);
        // Insecure contexts (plain-http dev on a phone) have no clipboard API.
        if (text) void navigator.clipboard?.writeText(text).catch(() => {});
      }, 150);
    });

    // Cmd/Ctrl+C copies xterm's own serialization of the selection, which
    // breaks a tmux-wrapped line at every row edge. Same rewrite as above,
    // applied before the browser reads the clipboard.
    const copyCapture = (e: ClipboardEvent) => {
      const text = selectedText(term);
      if (!text) return;
      e.clipboardData?.setData("text/plain", text);
      e.preventDefault();
    };
    captureContainer.addEventListener("copy", copyCapture, true);

    // Focusing this terminal claims the window size at our dims — the cheap way
    // to reclaim a window some other client shrank, without having to type.
    // Deliberate input gestures claim it through claimForInteraction too.
    // Only a focus the user caused counts: a machine waking with this tab
    // frontmost re-fires focus on the element that already had it, and a claim
    // from there resizes the window under whoever is really typing. Browsers
    // without userActivation (and jsdom) keep the unconditional behaviour.
    const claimOnFocus = () => {
      if (sizePolicy !== "follow-input") return;
      const activation = navigator.userActivation as UserActivation | undefined;
      if (activation && !activation.isActive) return;
      sendResize(true);
    };
    captureContainer.addEventListener("focusin", claimOnFocus);

    const dataSub = term.onData((data) => {
      if (suppressNextCR && data === "\r") {
        suppressNextCR = false;
        return;
      }
      sendInput(data);
    });
    // A splitter drag moves this tile's box every frame; defer to one refit on
    // release instead of reflowing xterm and resizing the PTY per frame. The
    // catch-up call is a passive sendResize(), so ownership is unaffected.
    let reflowPending = false;
    const reflow = () => {
      fitToBox();
      sendResize();
    };
    const ro = new ResizeObserver(() => {
      if (isReflowHeld()) {
        reflowPending = true;
        return;
      }
      reflow();
    });
    ro.observe(containerRef.current!);
    const unsubscribeReflow = onReflowRelease(() => {
      if (!reflowPending) return;
      reflowPending = false;
      reflow();
    });
    // Re-sync dims after the tab comes back; the listener args must not leak
    // into sendResize's active parameter.
    const resyncSize = () => sendResize();
    window.addEventListener("focus", resyncSize);
    document.addEventListener("visibilitychange", resyncSize);

    return () => {
      operationsRef.current = inertTerminalOperations;
      closed = true;
      if (reconnectTimeoutId) clearTimeout(reconnectTimeoutId);
      if (ws) {
        ws.onmessage = null;
        ws.onclose = null;
        ws.close();
      }
      captureContainer.removeEventListener("keydown", shiftEnterCapture, true);
      captureContainer.removeEventListener("paste", claimInteractionCapture, true);
      captureContainer.removeEventListener("pointerdown", claimInteractionCapture, true);
      captureContainer.removeEventListener("wheel", claimInteractionCapture, true);
      captureContainer.removeEventListener("mousedown", selectDragCapture, true);
      if (copyTimeoutId) clearTimeout(copyTimeoutId);
      selectionSub.dispose();
      linkProvider?.dispose();
      captureContainer.removeEventListener("copy", copyCapture, true);
      captureContainer.removeEventListener("focusin", claimOnFocus);
      dataSub.dispose();
      ro.disconnect();
      unsubscribeReflow();
      window.removeEventListener("focus", resyncSize);
      document.removeEventListener("visibilitychange", resyncSize);
      fitSharedSizeRef.current = () => {};
      disposeTouchScroll();
      term.dispose();
    };
  }, [url, sessionId, retryNonce, sizePolicy, touchScrollback]);

  const fitButton = sizePolicy === "passive" && (
    <button
      className="fit-session-button"
      aria-label="Fit session to phone"
      disabled={state !== "open"}
      onClick={() => {
        fitSharedSizeRef.current();
      }}
    >
      Fit
    </button>
  );

  return (
    <div className="terminal-tile" style={{ position: "relative", height: "100%" }}>
      <div ref={containerRef} className={touchScrollback ? "touch-scrollback" : undefined} style={{ height: "100%" }} />
      {controlsSlot && fitButton ? createPortal(fitButton, controlsSlot) : fitButton}
      {state === "offline" && <div className="overlay">daemon unreachable — retrying…</div>}
      {state === "exited" && (
        <div className="overlay">
          session ended <button onClick={onClose}>dismiss</button>
        </div>
      )}
      {state === "missing" && (
        <div className="overlay">
          session not found on this daemon <button onClick={onClose}>dismiss</button>
        </div>
      )}
      {state === "auth" && (
        <div className="overlay">
          not logged in — log in, then <button onClick={() => setRetryNonce((n) => n + 1)}>reconnect</button>
        </div>
      )}
    </div>
  );
});

export default TerminalTile;
