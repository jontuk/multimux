import { vi } from "vitest";
import { apiFetch, wsURL } from "../api";
import type { Server } from "../servers";

const remote: Server = { id: "r1", origin: "https://otherbox:8686", name: "other", token: "tok" };
const local: Server = { id: "local", origin: window.location.origin, name: "local" };

test("remote requests carry bearer token", async () => {
  const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}"));
  await apiFetch(remote, "/api/tools");
  const [url, init] = spy.mock.calls[0];
  expect(url).toBe("https://otherbox:8686/api/tools");
  expect((init!.headers as Record<string, string>)["Authorization"]).toBe("Bearer tok");
  spy.mockRestore();
});

test("local requests use cookies, no auth header", async () => {
  const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}"));
  await apiFetch(local, "/api/tools");
  const [, init] = spy.mock.calls[0];
  expect((init!.headers as Record<string, string>)["Authorization"]).toBeUndefined();
  expect(init!.credentials).toBe("same-origin");
  spy.mockRestore();
});

test("wsURL swaps scheme and appends token", () => {
  expect(wsURL(remote, "/ws/pty/3")).toBe("wss://otherbox:8686/ws/pty/3?token=tok");
});
