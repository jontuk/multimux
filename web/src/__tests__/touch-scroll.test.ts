import { vi } from "vitest";
import { installTouchScroll } from "../term/touchScroll";

type PointerValues = {
  pointerId: number;
  pointerType: string;
  isPrimary: boolean;
  clientX: number;
  clientY: number;
};

const primaryTouch: PointerValues = {
  pointerId: 1,
  pointerType: "touch",
  isPrimary: true,
  clientX: 0,
  clientY: 0,
};

function dispatchPointer(
  element: HTMLElement,
  type: "pointerdown" | "pointermove" | "pointerup" | "pointercancel" | "lostpointercapture",
  values: Partial<PointerValues> = {},
) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  const pointer = { ...primaryTouch, ...values };
  for (const [name, value] of Object.entries(pointer)) {
    Object.defineProperty(event, name, { configurable: true, value });
  }
  element.dispatchEvent(event);
  return event;
}

function setup(ready = () => true) {
  const element = document.createElement("div");
  const captured = new Set<number>();
  const setPointerCapture = vi.fn((pointerId: number) => captured.add(pointerId));
  const releasePointerCapture = vi.fn((pointerId: number) => captured.delete(pointerId));
  Object.assign(element, {
    hasPointerCapture: (pointerId: number) => captured.has(pointerId),
    releasePointerCapture,
    setPointerCapture,
  });
  const wheels: WheelEvent[] = [];
  element.addEventListener("wheel", (event) => wheels.push(event));
  const dispose = installTouchScroll(element, ready);
  return { element, wheels, setPointerCapture, releasePointerCapture, dispose };
}

test("movement below the activation distance remains a tap candidate", () => {
  const { element, wheels, setPointerCapture } = setup();

  dispatchPointer(element, "pointerdown");
  const move = dispatchPointer(element, "pointermove", { clientX: 6, clientY: 8 });
  dispatchPointer(element, "pointerup", { clientX: 6, clientY: 8 });

  expect(move.defaultPrevented).toBe(false);
  expect(setPointerCapture).not.toHaveBeenCalled();
  expect(wheels).toEqual([]);
});

test("a vertical touch movement activates, captures, and cancels native movement", () => {
  const { element, setPointerCapture } = setup();

  dispatchPointer(element, "pointerdown", { clientX: 10, clientY: 20 });
  const move = dispatchPointer(element, "pointermove", { clientX: 16, clientY: 32 });

  expect(setPointerCapture).toHaveBeenCalledOnce();
  expect(setPointerCapture).toHaveBeenCalledWith(1);
  expect(move.defaultPrevented).toBe(true);
});

test.each([
  ["horizontal", 13, 1],
  ["diagonal", 9, 9],
])("%s movement is rejected for the rest of the pointer sequence", (_name, clientX, clientY) => {
  const { element, wheels, setPointerCapture } = setup();

  dispatchPointer(element, "pointerdown");
  const rejected = dispatchPointer(element, "pointermove", { clientX, clientY });
  const laterVertical = dispatchPointer(element, "pointermove", { clientX, clientY: 60 });

  expect(rejected.defaultPrevented).toBe(false);
  expect(laterVertical.defaultPrevented).toBe(false);
  expect(setPointerCapture).not.toHaveBeenCalled();
  expect(wheels).toEqual([]);
});

test.each([
  ["mouse", { pointerType: "mouse" }],
  ["pen", { pointerType: "pen" }],
  ["non-primary touch", { pointerType: "touch", isPrimary: false }],
] as const)("ignores %s input", (_name, pointer) => {
  const { element, wheels, setPointerCapture } = setup();

  dispatchPointer(element, "pointerdown", pointer);
  dispatchPointer(element, "pointermove", { ...pointer, clientY: 48 });

  expect(setPointerCapture).not.toHaveBeenCalled();
  expect(wheels).toEqual([]);
});

test("ignores an additional pointer without disturbing the primary candidate", () => {
  const { element, setPointerCapture } = setup();

  dispatchPointer(element, "pointerdown", { pointerId: 1 });
  dispatchPointer(element, "pointerdown", { pointerId: 2, isPrimary: false });
  dispatchPointer(element, "pointermove", { pointerId: 2, isPrimary: false, clientY: 48 });
  dispatchPointer(element, "pointermove", { pointerId: 1, clientY: 13 });

  expect(setPointerCapture).toHaveBeenCalledOnce();
  expect(setPointerCapture).toHaveBeenCalledWith(1);
});
