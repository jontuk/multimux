import { useEffect } from "react";
import { wsURL } from "./api";
import type { Server } from "./servers";

export function useEvents(server: Server, onEvent: (type: string) => void) {
  useEffect(() => {
    let ws: WebSocket | null = null;
    let closed = false;
    let backoff = 1000;
    function connect() {
      if (closed) return;
      ws = new WebSocket(wsURL(server, "/ws/events"));
      ws.onopen = () => (backoff = 1000);
      ws.onmessage = (ev) => {
        try {
          const { type } = JSON.parse(ev.data);
          if (type) onEvent(type);
        } catch {
          /* ignore */
        }
      };
      ws.onclose = () => {
        if (closed) return;
        setTimeout(connect, backoff);
        backoff = Math.min(backoff * 2, 15000);
      };
    }
    connect();
    return () => {
      closed = true;
      ws?.close();
    };
  }, [server.id, server.origin, server.token, onEvent]);
}
