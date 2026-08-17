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

// A WebSocket can die without the browser ever saying so: a phone that slept
// through a network change wakes with a socket still in OPEN that will never
// carry another byte and never fire `close`. Nothing then reconnects, so the
// page stops hearing about sessions while looking perfectly connected — the
// state where the dir filter bar empties out and only a reload brings it back.
//
// WS ping frames are invisible to JS, so the daemon also sends a `keepalive`
// event every pingInterval (internal/server/ws.go); silence past this is a dead
// socket whatever readyState claims. 2.5× the daemon's 30s interval, so one
// dropped or late keepalive is not enough to churn a healthy connection.
const staleAfter = 75_000;
// How often that silence is checked. Background tabs throttle timers, which is
// fine: coming back to the tab checks immediately.
const liveCheckInterval = 20_000;

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
    // When the socket last carried anything — a keepalive counts.
    let lastFrameAt = Date.now();

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
      lastFrameAt = Date.now();
      ws = new WebSocket(url);
      ws.onopen = () => {
        backoff = 1000;
        failsSinceOpen = 0;
        lastFrameAt = Date.now();
        onStatusRef.current?.("open");
      };
      ws.onmessage = (ev) => {
        lastFrameAt = Date.now();
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

    // Replace the socket now rather than waiting out the backoff. The old one
    // is disowned first, so its `close` neither counts as a failure nor
    // schedules a second reconnect on top of this one.
    function reconnectNow() {
      if (closed) return;
      if (reconnectTimeoutId) {
        clearTimeout(reconnectTimeoutId);
        reconnectTimeoutId = null;
      }
      backoff = 1000;
      if (ws) {
        ws.onopen = ws.onmessage = ws.onclose = null;
        ws.close();
        ws = null;
      }
      connect();
    }

    const stale = () => Date.now() - lastFrameAt > staleAfter;

    // A socket the daemon has stopped talking through is dead however open it
    // looks. Only that case is handled on a timer: a socket that is genuinely
    // down already has a backoff retry pending, and shortening it here would
    // turn an unreachable daemon into a poll every liveCheckInterval.
    const liveCheck = setInterval(() => {
      if (!closed && ws?.readyState === WebSocket.OPEN && stale()) reconnectNow();
    }, liveCheckInterval);

    // Coming back — tab visible, network back, window refocused — is the one
    // moment the user is waiting on fresh data, so a socket that is down or
    // silent is replaced immediately instead of on the backoff's schedule. The
    // reconnect's `hello` is what makes the page resync.
    const onResume = () => {
      if (closed || document.visibilityState === "hidden") return;
      if (ws?.readyState !== WebSocket.OPEN || stale()) reconnectNow();
    };
    document.addEventListener("visibilitychange", onResume);
    window.addEventListener("online", onResume);
    window.addEventListener("focus", onResume);

    connect();
    return () => {
      closed = true;
      clearInterval(liveCheck);
      document.removeEventListener("visibilitychange", onResume);
      window.removeEventListener("online", onResume);
      window.removeEventListener("focus", onResume);
      if (reconnectTimeoutId) clearTimeout(reconnectTimeoutId);
      ws?.close();
    };
  }, [url]);
}
