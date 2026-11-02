// Image paste/drop into a terminal tile. A terminal can only receive text, and
// programs that take a pasted image (Claude Code's Ctrl+V) read the clipboard
// of the machine they run on — not this browser's, when the tile is on a
// remote daemon. So the image is uploaded to the tile's own daemon and the
// saved file's path is pasted instead, which those programs accept.

import { apiFetch, ApiError } from "../api";
import type { Server } from "../servers";

// Mirrors uploadExt in internal/server/uploads.go.
const uploadTypes = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

/** The uploadable images a paste or drop carries; empty for anything else. */
export function imageFiles(data: DataTransfer | null): File[] {
  if (!data) return [];
  const files = Array.from(data.files ?? []);
  // Some browsers expose a clipboard image only as an item, not in files.
  const fromItems =
    files.length > 0
      ? []
      : Array.from(data.items ?? [])
          .filter((item) => item.kind === "file")
          .map((item) => item.getAsFile())
          .filter((f): f is File => f !== null);
  return [...files, ...fromItems].filter((f) => uploadTypes.has(f.type));
}

/** Whether a drag carries files (as opposed to text being dragged). */
export function dragHasFiles(data: DataTransfer | null): boolean {
  return Array.from(data?.types ?? []).includes("Files");
}

/**
 * The path as a shell word, backslash-escaping specials the way terminals
 * do for a dragged-in file — the form path-accepting programs expect.
 */
export function quotePath(path: string): string {
  return path.replace(/[^A-Za-z0-9_./+:@%,=-]/g, (c) => `\\${c}`);
}

/** Uploads one image to the daemon and returns the absolute path it was saved at. */
export async function uploadImage(server: Server, file: File): Promise<string> {
  const path = "/api/uploads";
  const res = await apiFetch(server, path, {
    method: "POST",
    headers: { "Content-Type": file.type },
    body: file,
  });
  if (!res.ok) {
    let detail = "";
    try {
      const body = (await res.json()) as { error?: unknown };
      if (typeof body.error === "string") detail = body.error;
    } catch {
      // No JSON error body; the status is enough.
    }
    throw new ApiError(res.status, path, detail);
  }
  const { path: saved } = (await res.json()) as { path: string };
  return saved;
}
