// The dir filter is a view, not state the daemon knows about: soloing a
// directory changes what this browser renders and nothing else. The stored
// layout keeps every tile, so another tab (or this one after a clear) still
// sees the full grid. Mirrors how servers.ts keeps its list browser-local.

import type { Server } from "../servers";
import type { Dir, Session } from "./types";
import { normalize, type Layout, type Tile } from "./model";

const KEY = "multimux.soloDir";

/**
 * The one full path this browser is showing on its own, or null for all of
 * them. Null is the default, so a directory seen for the first time shows
 * without being enumerated anywhere.
 */
export function soloDir(): string | null {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? "null");
    return typeof raw === "string" ? raw : null;
  } catch {
    return null;
  }
}

export function setSoloDir(path: string | null) {
  localStorage.setItem(KEY, JSON.stringify(path));
}

// Last path segment, trailing slashes ignored. Root (and anything else that
// leaves nothing behind) falls back to the raw path so a button always has a
// label.
export function leafName(path: string): string {
  const parts = path.replace(/\/+$/, "").split("/");
  return parts[parts.length - 1] || path;
}

export type DirButton = { path: string; name: string; count: number };

// One entry per distinct directory of a running session, on any server. Dead
// sessions alone earn no button: they cannot be launched into, and soloing
// them is not what the user is reaching for.
export function dirButtons(servers: Server[], sessionsByServer: Record<string, Session[]>): DirButton[] {
  const counts = new Map<string, number>();
  for (const server of servers) {
    for (const sess of sessionsByServer[server.id] ?? []) {
      if (sess.status === "running") counts.set(sess.dir, (counts.get(sess.dir) ?? 0) + 1);
    }
  }
  return (
    [...counts]
      .map(([path, count]) => ({ path, name: leafName(path), count }))
      // Leaf name is what the user reads; the full path breaks ties so two
      // repos with the same leaf keep a stable order.
      .sort((a, b) => a.name.localeCompare(b.name) || a.path.localeCompare(b.path))
  );
}

// Split a session's working directory back into the launch directory it lives
// under and the subdir below it — the two halves the launcher needs to open
// another session in the same place. The longest matching directory wins, so a
// dir configured inside another dir is preferred over its parent (the subdir
// the user would have to type is then the shorter, more specific one).
//
// Matching is on path segments: "/repos/multi" is not a parent of
// "/repos/multimux". Returns null when no configured directory contains the
// path — a working directory on another daemon, or one whose dir was since
// removed. The daemon resolves symlinks when it records a session's dir but not
// when it stores a launch dir, so a symlinked dir can also miss here; null is
// the same answer in every case, and the caller leaves its selection alone.
export function splitUnderDir(dirs: Dir[], path: string): { dirId: number; subdir: string } | null {
  const target = path.replace(/\/+$/, "");
  let best: { dirId: number; subdir: string } | null = null;
  for (const d of dirs) {
    const base = d.path.replace(/\/+$/, "");
    if (target !== base && !target.startsWith(base + "/")) continue;
    const subdir = target === base ? "" : target.slice(base.length + 1);
    if (!best || subdir.length < best.subdir.length) best = { dirId: d.id, subdir };
  }
  return best;
}

// The solo that is actually in effect for this render. A stored path that
// names no button shows everything instead — and is left in storage, so the
// selection returns intact when its directory does (a page load before
// sessions arrive, a remote daemon briefly unreachable, the last session in
// the directory ending). That derivation is also why no button has to be
// invented for a stale entry: a solo can never outlive its button, because a
// solo with no button is not in effect, and so is never a filter the user
// cannot see or dismiss.
export function effectiveSolo(solo: string | null, dirs: DirButton[]): string | null {
  return solo !== null && dirs.some((d) => d.path === solo) ? solo : null;
}

// Keyboard rotation through the same buttons the bar shows, in the same order.
// "Show all" is a slot in the ring rather than a separate key, so holding one
// direction walks every directory and passes back through the unfiltered grid
// instead of stopping at an end the user cannot see. `solo` must be the
// effective solo: a stored path with no button is not in effect, and starting
// the walk from it would skip a step.
export function cycleSolo(solo: string | null, dirs: DirButton[], step: 1 | -1): string | null {
  if (dirs.length === 0) return null;
  const ring: (string | null)[] = [null, ...dirs.map((d) => d.path)];
  // A path with no button is show-all on screen, so it starts from that slot.
  const at = Math.max(ring.indexOf(solo), 0);
  return ring[(at + step + ring.length) % ring.length];
}

// Hidden tiles are dropped and the survivors re-packed through the layout's
// own canonical form, so the filtered grid has the same shape rules as any
// other — no gaps, rows derived from the count. `map` carries each view slot
// back to its index in the real layout: mutations (remove, swap) must be
// applied to the stored layout, which still holds the hidden tiles.
export function filterLayout(
  layout: Layout,
  isVisible: (tile: NonNullable<Tile>) => boolean,
): { view: Layout; map: number[] } {
  const visible: NonNullable<Tile>[] = [];
  const map: number[] = [];
  layout.tiles.forEach((t, i) => {
    if (t && isVisible(t)) {
      visible.push(t);
      map.push(i);
    }
  });
  return { view: normalize(visible, layout.shape.cols, layout.rowSizes, layout.colSizes), map };
}
