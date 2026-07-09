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
