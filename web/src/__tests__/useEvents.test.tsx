import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useEvents } from "../useEvents";
import type { Server } from "../servers";

const server: Server = { id: "r1", origin: "https://otherbox:8686", name: "other", token: "tok" };

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  readyState = FakeWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  close = vi.fn(() => {
    this.readyState = FakeWebSocket.CLOSED;
  });
  url: string;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
  // The two transitions the daemon drives, as the browser would report them.
  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }
  drop() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }
}

const last = () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1];

beforeEach(() => {
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("reports open when the socket connects", () => {
  const onStatus = vi.fn();
  renderHook(() => useEvents(server, () => {}, onStatus));
  FakeWebSocket.instances[0].onopen?.();
  expect(onStatus).toHaveBeenCalledWith("open");
});

test("repeated connect failures probe the API and classify the error", async () => {
  vi.useFakeTimers();
  const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 401 });
  vi.stubGlobal("fetch", fetchMock);
  const onStatus = vi.fn();
  renderHook(() => useEvents(server, () => {}, onStatus));

  FakeWebSocket.instances[0].onclose?.(); // fail #1 — no probe yet
  expect(fetchMock).not.toHaveBeenCalled();

  await vi.advanceTimersByTimeAsync(1000); // reconnect
  FakeWebSocket.instances[1].onclose?.(); // fail #2 — probe fires
  await vi.advanceTimersByTimeAsync(0); // flush the probe promise

  expect(fetchMock).toHaveBeenCalledWith(
    "https://otherbox:8686/api/auth/me",
    expect.objectContaining({ credentials: "omit" }),
  );
  expect(onStatus).toHaveBeenCalledWith("auth-expired");
  vi.useRealTimers();
});

test("probe failure classifies as unreachable", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("refused")));
  const onStatus = vi.fn();
  renderHook(() => useEvents(server, () => {}, onStatus));

  FakeWebSocket.instances[0].onclose?.();
  await vi.advanceTimersByTimeAsync(1000);
  FakeWebSocket.instances[1].onclose?.();
  await vi.advanceTimersByTimeAsync(0);

  expect(onStatus).toHaveBeenCalledWith("unreachable");
  vi.useRealTimers();
});

test("onHello receives the build id from the hello frame", () => {
  const onHello = vi.fn();
  renderHook(() => useEvents(server, () => {}, undefined, onHello));

  FakeWebSocket.instances[0].onmessage?.({ data: JSON.stringify({ type: "hello", build: "abc123def456" }) });

  expect(onHello).toHaveBeenCalledWith("abc123def456");
});

test("onHello receives an empty build when the daemon ships no assets", () => {
  const onHello = vi.fn();
  renderHook(() => useEvents(server, () => {}, undefined, onHello));

  FakeWebSocket.instances[0].onmessage?.({ data: JSON.stringify({ type: "hello" }) });

  expect(onHello).toHaveBeenCalledWith("");
});

test("socket survives onEvent identity changes across re-renders", () => {
  const { rerender } = renderHook(({ onEvent }) => useEvents(server, onEvent), {
    initialProps: { onEvent: () => {} },
  });
  expect(FakeWebSocket.instances).toHaveLength(1);
  const ws = FakeWebSocket.instances[0];

  // Re-render with a brand new callback identity — the WS must not be torn down.
  rerender({ onEvent: () => {} });
  expect(FakeWebSocket.instances).toHaveLength(1);
  expect(ws.close).not.toHaveBeenCalled();
});

test("a tab coming back reconnects a downed socket instead of waiting out the backoff", async () => {
  vi.useFakeTimers();
  renderHook(() => useEvents(server, () => {}));
  last().open();
  last().drop(); // died while the tab was in the background

  expect(FakeWebSocket.instances).toHaveLength(1);
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(FakeWebSocket.instances).toHaveLength(2);

  // The backoff timer the drop scheduled was cancelled, not left to fire a
  // second, redundant socket a moment later.
  await vi.advanceTimersByTimeAsync(5000);
  expect(FakeWebSocket.instances).toHaveLength(2);
  vi.useRealTimers();
});

test("the network coming back reconnects a downed socket", () => {
  renderHook(() => useEvents(server, () => {}));
  last().open();
  last().drop();

  act(() => {
    window.dispatchEvent(new Event("online"));
  });
  expect(FakeWebSocket.instances).toHaveLength(2);
});

test("a healthy socket is left alone when the tab comes back", () => {
  renderHook(() => useEvents(server, () => {}));
  last().open();

  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(FakeWebSocket.instances).toHaveLength(1);
});

test("a socket that stops carrying keepalives is replaced", async () => {
  // A phone that slept through a network change keeps its WebSocket in OPEN
  // forever: no close event, no reconnect, and the page silently stops hearing
  // about sessions. Ping frames are invisible to JS, so the daemon's keepalive
  // frames are the only liveness signal there is — silence means gone.
  vi.useFakeTimers();
  renderHook(() => useEvents(server, () => {}));
  last().open();

  await vi.advanceTimersByTimeAsync(80_000);

  expect(FakeWebSocket.instances.length).toBeGreaterThan(1);
  vi.useRealTimers();
});

test("keepalive frames keep the socket in place", async () => {
  vi.useFakeTimers();
  renderHook(() => useEvents(server, () => {}));
  last().open();

  for (let i = 0; i < 5; i++) {
    await vi.advanceTimersByTimeAsync(30_000);
    act(() => last().onmessage?.({ data: JSON.stringify({ type: "keepalive" }) }));
  }

  expect(FakeWebSocket.instances).toHaveLength(1);
  vi.useRealTimers();
});

test("latest onEvent handler receives messages after re-render", () => {
  const first = vi.fn();
  const second = vi.fn();
  const { rerender } = renderHook(({ onEvent }) => useEvents(server, onEvent), {
    initialProps: { onEvent: first },
  });
  rerender({ onEvent: second });

  const ws = FakeWebSocket.instances[0];
  ws.onmessage?.({ data: JSON.stringify({ type: "session_started" }) });

  expect(first).not.toHaveBeenCalled();
  expect(second).toHaveBeenCalledWith("session_started");
});
