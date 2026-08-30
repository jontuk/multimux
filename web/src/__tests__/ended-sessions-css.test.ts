import { readFileSync } from "node:fs";
import { expect, test } from "vitest";

const styles = readFileSync("src/index.css", "utf8");

function rule(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return styles.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

test("the recovery notice stays above a maximized tile", () => {
  const banner = rule(".ended-sessions-banner");
  const maximized = rule(".tile-maximized");
  expect(banner).toContain("position: relative");
  expect(Number(banner.match(/z-index:\s*(\d+)/)?.[1])).toBeGreaterThan(
    Number(maximized.match(/z-index:\s*(\d+)/)?.[1]),
  );
});

test("the recovery notice wraps long server names without clipping its action", () => {
  expect(rule(".ended-sessions-banner")).toContain("flex-wrap: wrap");
  expect(rule(".ended-sessions-banner span")).toContain("overflow-wrap: anywhere");
});
