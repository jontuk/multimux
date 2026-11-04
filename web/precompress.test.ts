import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { brotliDecompressSync, gunzipSync } from "node:zlib";
import { afterEach, beforeEach, describe, expect, test } from "vitest";
import { precompressDir } from "./precompress.ts";

describe("precompressDir", () => {
  let dir: string;
  const bundle = "export const answer = 42;\n".repeat(200);

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "precompress-"));
    mkdirSync(join(dir, "assets"));
  });
  afterEach(() => rmSync(dir, { recursive: true, force: true }));

  test("writes siblings that decode to the original, including in subdirectories", () => {
    writeFileSync(join(dir, "assets", "app.js"), bundle);
    precompressDir(dir);
    const br = readFileSync(join(dir, "assets", "app.js.br"));
    const gz = readFileSync(join(dir, "assets", "app.js.gz"));
    expect(brotliDecompressSync(br).toString()).toBe(bundle);
    expect(gunzipSync(gz).toString()).toBe(bundle);
  });

  test("compresses every text type the app ships", () => {
    for (const name of ["index.html", "icon.svg", "assets/app.css", "assets/data.json", "assets/app.js.map"]) {
      writeFileSync(join(dir, name), bundle);
    }
    precompressDir(dir);
    for (const name of ["index.html", "icon.svg", "assets/app.css", "assets/data.json", "assets/app.js.map"]) {
      expect(existsSync(join(dir, `${name}.br`)), name).toBe(true);
      expect(existsSync(join(dir, `${name}.gz`)), name).toBe(true);
    }
  });

  test("leaves already-compressed formats alone", () => {
    writeFileSync(join(dir, "icon.png"), bundle);
    precompressDir(dir);
    expect(existsSync(join(dir, "icon.png.br"))).toBe(false);
    expect(existsSync(join(dir, "icon.png.gz"))).toBe(false);
  });

  // gzip's header alone makes a tiny file bigger, and the daemon would then
  // serve the larger variant to every browser.
  test("skips a sibling that would not be smaller", () => {
    writeFileSync(join(dir, "tiny.js"), "1");
    precompressDir(dir);
    expect(existsSync(join(dir, "tiny.js.gz"))).toBe(false);
    expect(existsSync(join(dir, "tiny.js.br"))).toBe(false);
  });

  test("does not compress its own output on a second run", () => {
    writeFileSync(join(dir, "assets", "app.js"), bundle);
    precompressDir(dir);
    precompressDir(dir);
    expect(existsSync(join(dir, "assets", "app.js.br.br"))).toBe(false);
    expect(existsSync(join(dir, "assets", "app.js.gz.gz"))).toBe(false);
  });
});
