import { describe, expect, it } from "vitest";
import { dirHue, dirTint, dirTintStyle } from "../grid/dirColor";

describe("dirHue", () => {
  it("is stable for the same path", () => {
    expect(dirHue("/Users/jon/Repos/multimux")).toBe(dirHue("/Users/jon/Repos/multimux"));
  });

  it("stays inside the hue circle", () => {
    for (const p of ["", "/", "/a", "/Users/jon/Repos/multimux", "~/x/y/z", "/tmp/" + "d".repeat(500)]) {
      const h = dirHue(p);
      expect(Number.isInteger(h)).toBe(true);
      expect(h).toBeGreaterThanOrEqual(0);
      expect(h).toBeLessThan(360);
    }
  });

  it("separates paths that differ only in their parent", () => {
    expect(dirHue("/Users/jon/Repos/multimux")).not.toBe(dirHue("/Users/jon/old/multimux"));
  });

  it("spreads a realistic set of directories across distinct hues", () => {
    const dirs = ["/Users/jon/Repos/multimux", "/Users/jon/Repos/web", "/Users/jon/notes", "/tmp", "/"];
    expect(new Set(dirs.map(dirHue)).size).toBe(dirs.length);
  });
});

describe("dirTint", () => {
  it("renders a fixed-weight hsl colour", () => {
    expect(dirTint("/Users/jon/Repos/multimux")).toBe(`hsl(${dirHue("/Users/jon/Repos/multimux")} 60% 55%)`);
  });

  it("exposes the tint as the --dir-tint custom property", () => {
    expect(dirTintStyle("/tmp")).toEqual({ "--dir-tint": dirTint("/tmp") });
  });
});
