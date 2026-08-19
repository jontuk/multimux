const ACTIVATION_DISTANCE = 12;
const PIXELS_PER_STEP = 24;

type Gesture = {
  pointerId: number;
  startX: number;
  startY: number;
  lastY: number;
  remainder: number;
  phase: "pending" | "scrolling" | "rejected";
};

export function installTouchScroll(element: HTMLElement, isReady: () => boolean): () => void {
  let gesture: Gesture | null = null;

  function clear() {
    gesture = null;
  }

  function emitMovement(event: PointerEvent) {
    if (!gesture || gesture.phase !== "scrolling") return;
    gesture.remainder += event.clientY - gesture.lastY;
    gesture.lastY = event.clientY;
    const steps = Math.trunc(gesture.remainder / PIXELS_PER_STEP);
    gesture.remainder -= steps * PIXELS_PER_STEP;

    // Completed steps while tmux mouse tracking is inactive are deliberately
    // dropped: xterm's no-scrollback fallback would turn them into cursor keys,
    // and retaining them would produce a delayed burst after negotiation.
    if (steps === 0 || !isReady()) return;
    const deltaY = steps > 0 ? -1 : 1;
    for (let index = 0; index < Math.abs(steps); index += 1) {
      element.dispatchEvent(
        new WheelEvent("wheel", {
          bubbles: true,
          cancelable: true,
          clientX: event.clientX,
          clientY: event.clientY,
          deltaMode: WheelEvent.DOM_DELTA_LINE,
          deltaX: 0,
          deltaY,
          deltaZ: 0,
        }),
      );
    }
  }

  function onPointerDown(event: PointerEvent) {
    if (gesture || event.pointerType !== "touch" || !event.isPrimary) return;
    gesture = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      lastY: event.clientY,
      remainder: 0,
      phase: "pending",
    };
  }

  function onPointerMove(event: PointerEvent) {
    if (!gesture || event.pointerId !== gesture.pointerId || gesture.phase === "rejected") return;

    if (gesture.phase === "pending") {
      const dx = event.clientX - gesture.startX;
      const dy = event.clientY - gesture.startY;
      if (Math.hypot(dx, dy) < ACTIVATION_DISTANCE) return;
      if (Math.abs(dy) <= Math.abs(dx)) {
        gesture.phase = "rejected";
        return;
      }
      gesture.phase = "scrolling";
      element.setPointerCapture(event.pointerId);
    }

    event.preventDefault();
    emitMovement(event);
  }

  function onPointerEnd(event: PointerEvent) {
    if (gesture?.pointerId === event.pointerId) clear();
  }

  element.addEventListener("pointerdown", onPointerDown);
  element.addEventListener("pointermove", onPointerMove, { passive: false });
  element.addEventListener("pointerup", onPointerEnd);
  element.addEventListener("pointercancel", onPointerEnd);
  element.addEventListener("lostpointercapture", onPointerEnd);

  return () => {
    const pointerId = gesture?.phase === "scrolling" ? gesture.pointerId : null;
    clear();
    if (pointerId !== null && element.hasPointerCapture(pointerId)) {
      element.releasePointerCapture(pointerId);
    }
    element.removeEventListener("pointerdown", onPointerDown);
    element.removeEventListener("pointermove", onPointerMove);
    element.removeEventListener("pointerup", onPointerEnd);
    element.removeEventListener("pointercancel", onPointerEnd);
    element.removeEventListener("lostpointercapture", onPointerEnd);
  };
}
