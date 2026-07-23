import { vi } from "vitest";
import { ApiError, apiFetch, del, errorText, getJSON, isUnauthorized, isUnreachable, postJSON, wsURL } from "../api";
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

test("a rejected fetch becomes ApiError status 0, not a TypeError", async () => {
  const spy = vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
  const err = await getJSON(local, "/api/tools").catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(0);
  expect(isUnreachable(err)).toBe(true);
  expect(isUnauthorized(err)).toBe(false);
  expect(errorText(err)).toBe("Can't reach the daemon.");
  spy.mockRestore();
});

test("an HTTP 500 becomes ApiError status 500 carrying the daemon's error text", async () => {
  const spy = vi
    .spyOn(globalThis, "fetch")
    .mockResolvedValue(new Response(JSON.stringify({ error: "database is locked" }), { status: 500 }));
  const err = await getJSON(local, "/api/tools").catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(500);
  expect((err as ApiError).detail).toBe("database is locked");
  expect((err as Error).message).toContain("database is locked");
  expect(isUnreachable(err)).toBe(false);
  expect(errorText(err)).toContain("database is locked");
  spy.mockRestore();
});

test("a plain-text error body survives into the message", async () => {
  const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("web assets missing", { status: 501 }));
  const err = await del(local, "/api/tools/1").catch((e: unknown) => e);
  expect((err as ApiError).status).toBe(501);
  expect((err as ApiError).detail).toBe("web assets missing");
  spy.mockRestore();
});

test("a 401 is reported as unauthorized, never as unreachable", async () => {
  const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("", { status: 401 }));
  const err = await postJSON(local, "/api/tools", {}).catch((e: unknown) => e);
  expect(isUnauthorized(err)).toBe(true);
  expect(isUnreachable(err)).toBe(false);
  spy.mockRestore();
});
