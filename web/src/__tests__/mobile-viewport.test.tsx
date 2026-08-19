import { act, render, screen } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, vi } from "vitest";
import { useVisualViewportHeight } from "../useVisualViewportHeight";

class FakeVisualViewport extends EventTarget {
  height: number;

  constructor(height: number) {
    super();
    this.height = height;
  }
}

function Probe() {
  const height = useVisualViewportHeight();
  return <output role="status">{height ?? "none"}</output>;
}

const originalVisualViewport = Object.getOwnPropertyDescriptor(window, "visualViewport");

function installVisualViewport(viewport: FakeVisualViewport | undefined) {
  Object.defineProperty(window, "visualViewport", { configurable: true, value: viewport });
}

afterEach(() => {
  vi.useRealTimers();
  if (originalVisualViewport) Object.defineProperty(window, "visualViewport", originalVisualViewport);
  else Reflect.deleteProperty(window, "visualViewport");
});

test("viewport metadata enables safe areas and interactive keyboard resizing", () => {
  const html = readFileSync(resolve(process.cwd(), "index.html"), "utf8");
  const content = html.match(/<meta\s+name="viewport"\s+content="([^"]+)"/i)?.[1];

  expect(content?.split(/,\s*/)).toEqual([
    "width=device-width",
    "initial-scale=1.0",
    "viewport-fit=cover",
    "interactive-widget=resizes-content",
  ]);
});

test("reads the visual viewport immediately and publishes only its settled height", () => {
  vi.useFakeTimers();
  const viewport = new FakeVisualViewport(780);
  installVisualViewport(viewport);
  render(<Probe />);

  expect(screen.getByRole("status")).toHaveTextContent("780");

  viewport.height = 520;
  act(() => viewport.dispatchEvent(new Event("resize")));
  viewport.height = 460;
  act(() => viewport.dispatchEvent(new Event("resize")));
  expect(screen.getByRole("status")).toHaveTextContent("780");

  act(() => vi.advanceTimersByTime(149));
  expect(screen.getByRole("status")).toHaveTextContent("780");
  act(() => vi.advanceTimersByTime(1));
  expect(screen.getByRole("status")).toHaveTextContent("460");
});

test("cancels a pending viewport update on unmount", () => {
  vi.useFakeTimers();
  const viewport = new FakeVisualViewport(780);
  const remove = vi.spyOn(viewport, "removeEventListener");
  installVisualViewport(viewport);
  const { unmount } = render(<Probe />);

  viewport.height = 420;
  act(() => viewport.dispatchEvent(new Event("resize")));
  unmount();
  act(() => vi.runAllTimers());

  expect(remove).toHaveBeenCalledWith("resize", expect.any(Function));
});

test("returns no override when visualViewport is unavailable", () => {
  installVisualViewport(undefined);
  render(<Probe />);
  expect(screen.getByRole("status")).toHaveTextContent("none");
});
