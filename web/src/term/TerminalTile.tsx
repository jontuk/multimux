import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { ClipboardAddon } from "@xterm/addon-clipboard";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { apiFetch, wsURL } from "../api";
import type { Server } from "../servers";
import type { Session } from "../grid/types";
import { encodeResize, parseServerText } from "./protocol";

type Props = { server: Server; sessionId: number; onClose: () => void; autoFocus?: boolean };

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

export default function TerminalTile({ server, sessionId, onClose, autoFocus }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
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
  const url = wsURL(server, `/ws/pty/${sessionId}`);
  const serverRef = useRef(server);
  useEffect(() => {
    serverRef.current = server;
  });

  useEffect(() => {
    const term = new Terminal({
      scrollback: 0, // tmux owns scrollback (mouse mode)
      fontSize: 13,
      // On Mac, xterm.js only bypasses app mouse-mode for Option+drag (never
      // Shift — that's hardcoded Linux/Windows-only in SelectionService).
      // shiftDragCapture below makes Shift+drag work too.
      macOptionClickForcesSelection: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new ClipboardAddon()); // OSC 52: tmux copy-mode yank → system clipboard
    term.loadAddon(new WebLinksAddon());
    term.open(containerRef.current!);
    if (autoFocusRef.current && !didAutoFocus.current) {
      didAutoFocus.current = true;
      term.focus();
    }
    const encoder = new TextEncoder();

    let ws: WebSocket | null = null;
    let closed = false;
    let backoff = 500;
    let reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;

    function sendResize() {
      if (ws?.readyState === WebSocket.OPEN) {
        const active = document.visibilityState === "visible" && document.hasFocus();
        ws.send(encodeResize(term.cols, term.rows, active));
      }
    }

    function connect() {
      if (closed) return;
      setState("connecting");
      ws = new WebSocket(url);
      ws.binaryType = "arraybuffer";
      ws.onopen = () => {
        setState("open");
        backoff = 500;
        fit.fit();
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

    // Shift+drag selection. tmux mouse mode owns click-drag, and xterm.js's
    // escape hatch (shouldForceSelection) is Option-only on Mac — Shift is
    // hardcoded Linux/Windows-only. Both call sites read event.altKey off the
    // mousedown, so shadow it on the instance in the capture phase, before
    // xterm.js's bubble-phase listeners see the event. Mac only: elsewhere
    // Shift already works, and a forced altKey would trip column-select.
    const shiftDragCapture = (e: MouseEvent) => {
      if (isMac && e.button === 0 && e.shiftKey && !e.altKey) {
        Object.defineProperty(e, "altKey", { value: true, configurable: true });
      }
    };
    captureContainer.addEventListener("mousedown", shiftDragCapture, true);

    const dataSub = term.onData((data) => {
      if (suppressNextCR && data === "\r") {
        suppressNextCR = false;
        return;
      }
      if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode(data));
    });
    const ro = new ResizeObserver(() => {
      fit.fit();
      sendResize();
    });
    ro.observe(containerRef.current!);
    window.addEventListener("focus", sendResize);
    document.addEventListener("visibilitychange", sendResize);

    return () => {
      closed = true;
      if (reconnectTimeoutId) clearTimeout(reconnectTimeoutId);
      if (ws) {
        ws.onmessage = null;
        ws.onclose = null;
        ws.close();
      }
      captureContainer.removeEventListener("keydown", shiftEnterCapture, true);
      captureContainer.removeEventListener("mousedown", shiftDragCapture, true);
      dataSub.dispose();
      ro.disconnect();
      window.removeEventListener("focus", sendResize);
      document.removeEventListener("visibilitychange", sendResize);
      term.dispose();
    };
  }, [url, sessionId, retryNonce]);

  return (
    <div className="terminal-tile" style={{ position: "relative", height: "100%" }}>
      <div ref={containerRef} style={{ height: "100%" }} />
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
}
