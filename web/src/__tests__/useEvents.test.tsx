import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useEvents } from "../useEvents";
import type { Server } from "../servers";

const server: Server = { id: "r1", origin: "https://otherbox:8686", name: "other", token: "tok" };

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  close = vi.fn();
  url: string;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
}

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
