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

test("the mobile grid reserves the top safe area before banners and chrome", () => {
  const styles = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");
  const gridRoute = styles.match(/\.app\.grid-route\s*\{([^}]*)\}/s)?.[1];
  const mobileHeader = styles.match(/\.app\.grid-route \.mobile-session-header\s*\{([^}]*)\}/s)?.[1];

  expect(gridRoute).toMatch(/padding-top:\s*env\(safe-area-inset-top\)/);
  expect(mobileHeader).not.toMatch(/safe-area-inset-top/);
});

test("long mobile host labels shrink and ellipsize before controls", () => {
  const styles = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");
  const hostLabel = styles.match(/\.app\.grid-route \.mobile-host-label\s*\{([^}]*)\}/s)?.[1];

  expect(hostLabel).toMatch(/max-width:/);
  expect(hostLabel).toMatch(/min-width:\s*0/);
  expect(hostLabel).toMatch(/overflow:\s*hidden/);
  expect(hostLabel).toMatch(/text-overflow:\s*ellipsis/);
});

test("Compose stays below the shrinking terminal and above the keyboard safe area", () => {
  const styles = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");
  const mobileTerminal = [...styles.matchAll(/\.app\.grid-route \.mobile-terminal\s*\{([^}]*)\}/gs)]
    .map((match) => match[1])
    .join("\n");
  const terminalTile = styles.match(/\.app\.grid-route \.mobile-terminal > \.terminal-tile\s*\{([^}]*)\}/s)?.[1];
  const compose = styles.match(/\.app\.grid-route \.mobile-compose\s*\{([^}]*)\}/s)?.[1];
  const textarea = styles.match(/\.app\.grid-route \.mobile-compose textarea\s*\{([^}]*)\}/s)?.[1];

  expect(mobileTerminal).toMatch(/flex-direction:\s*column/);
  expect(mobileTerminal).toMatch(/padding-bottom:\s*env\(safe-area-inset-bottom\)/);
  expect(terminalTile).toMatch(/flex:\s*1 1 auto/);
  expect(terminalTile).toMatch(/min-height:\s*0/);
  expect(compose).toMatch(/flex:\s*none/);
  expect(textarea).toMatch(/max-height:/);
  expect(textarea).toMatch(/resize:\s*vertical/);
});

test("the essential key bar consumes space only while the mobile terminal contains focus", () => {
  const styles = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");
  const hiddenKeyBar = styles.match(/\.app\.grid-route \.mobile-key-bar\s*\{([^}]*)\}/s)?.[1];
  const focusedKeyBar = styles.match(
    /\.app\.grid-route \.mobile-terminal:focus-within > \.mobile-key-bar\s*\{([^}]*)\}/s,
  )?.[1];
  const keyButton = styles.match(/\.app\.grid-route \.mobile-key-bar button\s*\{([^}]*)\}/s)?.[1];

  expect(hiddenKeyBar).toMatch(/display:\s*none/);
  expect(focusedKeyBar).toMatch(/display:\s*grid/);
  expect(focusedKeyBar).toMatch(/flex:\s*none/);
  expect(focusedKeyBar).toMatch(/grid-template-columns:\s*repeat\(8,\s*minmax\(0,\s*1fr\)\)/);
  expect(focusedKeyBar).toMatch(/safe-area-inset-left/);
  expect(focusedKeyBar).toMatch(/safe-area-inset-right/);
  expect(keyButton).toMatch(/min-height:\s*2\.75rem/);
  expect(keyButton).toMatch(/min-width:\s*0/);
  expect(keyButton).toMatch(/touch-action:\s*manipulation/);
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
