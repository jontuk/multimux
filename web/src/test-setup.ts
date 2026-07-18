import "@testing-library/jest-dom";

// xterm probes canvas support when the app shell mounts. jsdom returns null
// here but also emits a noisy "not implemented" diagnostic unless the method
// is supplied by the test environment.
Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  configurable: true,
  value: () => null,
});
