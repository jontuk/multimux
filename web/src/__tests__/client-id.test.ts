import { beforeEach, expect, test, vi } from "vitest";
import { clientId } from "../clientId";

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
});

test("clientId is stable across calls and persisted", async () => {
  const first = clientId();
  expect(first).not.toBe("");
  expect(clientId()).toBe(first);

  // A fresh module (a reload) must recover the same id from localStorage.
  vi.resetModules();
  const reloaded = (await import("../clientId")).clientId();
  expect(reloaded).toBe(first);
});

test("clientId survives unusable storage", async () => {
  const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("denied");
  });
  const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("denied");
  });
  const mod = await import("../clientId");
  const id = mod.clientId();
  expect(id).not.toBe("");
  expect(mod.clientId()).toBe(id); // still stable for this page's lifetime
  get.mockRestore();
  set.mockRestore();
});
