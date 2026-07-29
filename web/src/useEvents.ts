import { useEffect, useRef } from "react";
import { apiFetch, wsURL } from "./api";
import type { Server } from "./servers";

// Why the events socket won't stay up. The browser WS API hides the HTTP
// status of a failed upgrade, so after repeated connect failures we probe an
// authenticated API endpoint to classify: auth-expired (401 — stale session
// cookie, e.g. the daemon's data dir changed), forbidden (403 — setup pending),
// ws-blocked (API fine but the WS upgrade is refused — origin not allowed),
// unreachable (daemon down).
export type EventsStatus = "open" | "auth-expired" | "forbidden" | "ws-blocked" | "unreachable";

export function useEvents(
  server: Server,
  onEvent: (type: string) => void,
  onStatus?: (s: EventsStatus) => void,
  // Fired with the daemon's frontend build id on every connect ("" when the
  // daemon ships no assets). Lets a caller notice, after a reconnect, that the
  // daemon now serves a different build than the one this tab is running.
  onHello?: (build: string) => void,
) {
  // Hold the latest handler in a ref so the effect doesn't depend on its
  // identity; the WebSocket then survives re-renders that pass a new callback.
  const onEventRef = useRef(onEvent);
  const onStatusRef = useRef(onStatus);
  const onHelloRef = useRef(onHello);
  const serverRef = useRef(server);
  useEffect(() => {
    onEventRef.current = onEvent;
    onStatusRef.current = onStatus;
    onHelloRef.current = onHello;
    serverRef.current = server;
  });

  // Depend on the URL string, not the server object: listServers() returns
  // fresh objects each render, but the string only changes when it matters.
  const url = wsURL(server, "/ws/events");
  useEffect(() => {
    let ws: WebSocket | null = null;
    let closed = false;
    let backoff = 1000;
    let reconnectTimeoutId: ReturnType<typeof setTimeout> | null = null;
    let failsSinceOpen = 0;

    async function classify() {
      let status: EventsStatus;
      try {
        const r = await apiFetch(serverRef.current, "/api/auth/me");
        status =
          r.status === 401 ? "auth-expired" : r.status === 403 ? "forbidden" : r.ok ? "ws-blocked" : "unreachable";
      } catch {
        status = "unreachable";
      }
      if (!closed) onStatusRef.current?.(status);
    }

    function connect() {
      if (closed) return;
      ws = new WebSocket(url);
      ws.onopen = () => {
        backoff = 1000;
        failsSinceOpen = 0;
        onStatusRef.current?.("open");
      };
      ws.onmessage = (ev) => {
        try {
          const { type, build } = JSON.parse(ev.data);
          if (type === "hello") onHelloRef.current?.(typeof build === "string" ? build : "");
          if (type) onEventRef.current(type);
        } catch {
          /* ignore */
        }
      };
      ws.onclose = () => {
        if (closed) return;
        failsSinceOpen++;
        if (failsSinceOpen >= 2) classify();
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
  }, [url]);
}
