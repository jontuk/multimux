import "@testing-library/jest-dom";

// Node ≥22 ships its own global `localStorage` (undefined unless the process
// sets --localstorage-file), which shadows jsdom's working implementation.
// Provide an in-memory Storage so app code and tests see a real one.
if (globalThis.localStorage === undefined) {
  const store = new Map<string, string>();
  const stub: Storage = {
    get length() {
      return store.size;
    },
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
  };
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: stub });
}

// xterm probes canvas support when the app shell mounts. jsdom returns null
// here but also emits a noisy "not implemented" diagnostic unless the method
// is supplied by the test environment.
Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  configurable: true,
  value: () => null,
});

// jsdom does not implement pointer capture. Real browsers need it so a
// splitter drag survives the pointer leaving the handle; tests just need the
// call not to throw.
if (!HTMLElement.prototype.setPointerCapture) {
  Object.defineProperty(HTMLElement.prototype, "setPointerCapture", {
    configurable: true,
    writable: true,
    value: () => {},
  });
}
if (!HTMLElement.prototype.releasePointerCapture) {
  Object.defineProperty(HTMLElement.prototype, "releasePointerCapture", {
    configurable: true,
    writable: true,
    value: () => {},
  });
}
