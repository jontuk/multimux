import { afterEach, expect, test, vi } from "vitest";
import { retireServiceWorker } from "../retire-sw";

/** Installs a fake navigator.serviceWorker returning `registrations`. */
function stubServiceWorker(registrations: { unregister: () => Promise<boolean> }[]) {
  const getRegistrations = vi.fn(() => Promise.resolve(registrations));
  Object.defineProperty(navigator, "serviceWorker", {
    configurable: true,
    value: { getRegistrations },
  });
  return getRegistrations;
}

/** Installs a fake CacheStorage and returns its delete spy. */
function stubCaches() {
  const del = vi.fn(() => Promise.resolve(true));
  Object.defineProperty(globalThis, "caches", { configurable: true, value: { delete: del } });
  return del;
}

afterEach(() => {
  Reflect.deleteProperty(navigator, "serviceWorker");
  Reflect.deleteProperty(globalThis, "caches");
  vi.restoreAllMocks();
});

test("a leftover worker is unregistered and its cache dropped", async () => {
  const unregister = vi.fn(() => Promise.resolve(true));
  stubServiceWorker([{ unregister }]);
  const del = stubCaches();

  await retireServiceWorker();

  expect(unregister).toHaveBeenCalledOnce();
  expect(del).toHaveBeenCalledWith("multimux-shell-v1");
});

test("every registration is unregistered, not just the first", async () => {
  const a = vi.fn(() => Promise.resolve(true));
  const b = vi.fn(() => Promise.resolve(true));
  stubServiceWorker([{ unregister: a }, { unregister: b }]);
  stubCaches();

  await retireServiceWorker();

  expect(a).toHaveBeenCalledOnce();
  expect(b).toHaveBeenCalledOnce();
});

test("a browser without service workers still clears the legacy cache", async () => {
  const del = stubCaches();

  await retireServiceWorker();

  expect(del).toHaveBeenCalledWith("multimux-shell-v1");
});

test("a rejecting service-worker API does not throw", async () => {
  Object.defineProperty(navigator, "serviceWorker", {
    configurable: true,
    value: { getRegistrations: () => Promise.reject(new Error("denied")) },
  });

  await expect(retireServiceWorker()).resolves.toBeUndefined();
});
