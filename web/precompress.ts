import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { extname, join, resolve } from "node:path";
import { brotliCompressSync, constants, gzipSync } from "node:zlib";
import type { Plugin } from "vite";

// Formats worth compressing. Everything else the build emits (PNGs) already is.
const COMPRESSIBLE = new Set([".css", ".html", ".js", ".json", ".map", ".svg", ".txt"]);

function compressibleFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return compressibleFiles(path);
    return COMPRESSIBLE.has(extname(name)) ? [path] : [];
  });
}

// Writes .br and .gz siblings next to each compressible file under dir. The
// daemon embeds them and picks one per request (internal/server/static.go),
// which is why this happens at build time: maximum-quality brotli takes seconds,
// and no request should wait for that.
export function precompressDir(dir: string): void {
  for (const path of compressibleFiles(dir)) {
    const content = readFileSync(path);
    const variants: [string, Buffer][] = [
      [
        ".br",
        brotliCompressSync(content, { params: { [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY } }),
      ],
      [".gz", gzipSync(content, { level: constants.Z_BEST_COMPRESSION })],
    ];
    for (const [suffix, compressed] of variants) {
      if (compressed.length < content.length) writeFileSync(path + suffix, compressed);
    }
  }
}

export function precompress(): Plugin {
  let outDir = "dist";
  return {
    name: "multimux-precompress",
    apply: "build",
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir);
    },
    closeBundle() {
      precompressDir(outDir);
    },
  };
}
