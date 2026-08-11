// While a grid splitter is being dragged, every tile's box changes on every
// frame. Letting each tile's ResizeObserver run means an xterm reflow and a
// PTY resize message per tile per frame, so the drag holds this gate and the
// observers defer instead: one refit per affected tile when the drag ends.
let held = false;
const listeners = new Set<() => void>();

export function beginReflowHold(): void {
  held = true;
}

export function endReflowHold(): void {
  if (!held) return;
  held = false;
  for (const fn of [...listeners]) fn();
}

export function isReflowHeld(): boolean {
  return held;
}

/** Called once when a hold is released. Returns an unsubscribe function. */
export function onReflowRelease(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
