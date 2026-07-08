import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { wsURL } from "../api";
import type { Server } from "../servers";
import { encodeResize, parseServerText } from "./protocol";

type Props = { server: Server; sessionId: number; onClose: () => void };
type ConnState = "connecting" | "open" | "offline" | "exited";

export default function TerminalTile({ server, sessionId, onClose }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<ConnState>("connecting");

  useEffect(() => {
    const term = new Terminal({ scrollback: 0, fontSize: 13 }); // tmux owns scrollback (mouse mode)
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(containerRef.current!);
    const encoder = new TextEncoder();

    let ws: WebSocket | null = null;
    let closed = false;
    let backoff = 500;
    let reconnectTimeoutId: NodeJS.Timeout | null = null;

    function sendResize() {
      if (ws?.readyState === WebSocket.OPEN) ws.send(encodeResize(term.cols, term.rows));
    }

    function connect() {
      if (closed) return;
      setState("connecting");
      ws = new WebSocket(wsURL(server, `/ws/pty/${sessionId}`));
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
      ws.onclose = () => {
        if (closed) return;
        setState("offline");
        reconnectTimeoutId = setTimeout(connect, backoff);
        backoff = Math.min(backoff * 2, 10000);
      };
    }
    connect();

    const dataSub = term.onData((data) => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode(data));
    });
    const ro = new ResizeObserver(() => {
      fit.fit();
      sendResize();
    });
    ro.observe(containerRef.current!);

    return () => {
      closed = true;
      if (reconnectTimeoutId) clearTimeout(reconnectTimeoutId);
      if (ws) {
        ws.onmessage = null;
        ws.onclose = null;
        ws.close();
      }
      dataSub.dispose();
      ro.disconnect();
      term.dispose();
    };
  }, [server.id, server.origin, server.token, sessionId]);

  return (
    <div className="terminal-tile" style={{ position: "relative", height: "100%" }}>
      <div ref={containerRef} style={{ height: "100%" }} />
      {state === "offline" && <div className="overlay">daemon unreachable — retrying…</div>}
      {state === "exited" && (
        <div className="overlay">
          session ended <button onClick={onClose}>dismiss</button>
        </div>
      )}
    </div>
  );
}
