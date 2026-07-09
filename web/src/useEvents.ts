import { useEffect, useRef } from "react";
import { wsURL } from "./api";
import type { Server } from "./servers";

export function useEvents(server: Server, onEvent: (type: string) => void) {
  // Hold the latest handler in a ref so the effect doesn't depend on its
  // identity; the WebSocket then survives re-renders that pass a new callback.
  const onEventRef = useRef(onEvent);
  useEffect(() => {
    onEventRef.current = onEvent;
  });

  useEffect(() => {
    let ws: WebSocket | null = null;
    let closed = false;
    let backoff = 1000;
    let reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;
    function connect() {
      if (closed) return;
      ws = new WebSocket(wsURL(server, "/ws/events"));
      ws.onopen = () => (backoff = 1000);
      ws.onmessage = (ev) => {
        try {
          const { type } = JSON.parse(ev.data);
          if (type) onEventRef.current(type);
        } catch {
          /* ignore */
        }
      };
      ws.onclose = () => {
        if (closed) return;
        reconnectTimeoutId = setTimeout(connect, backoff);
        backoff = Math.min(backoff * 2, 15000);
      };
    }
    connect();
    return () => {
      closed = true;
      if (reconnectTimeoutId) clearTimeout(reconnectTimeoutId);
      ws?.close();
    };
  }, [server.id, server.origin, server.token]);
}
