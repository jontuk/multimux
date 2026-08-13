// Per-directory view layout. Like the solo in dirFilter.ts, this is a view and
// not state the daemon knows about: it says how *this browser* arranges the
// tiles of one soloed directory. The stored layout (/api/layout) stays the
// record of which sessions are placed at all, and of the unfiltered grid's own
// arrangement.

import { normalize, tileKey, type Layout, type Tile } from "./model";

const KEY = "multimux.viewLayout";

/**
 * One directory's arrangement. `order` holds tileKey strings rather than
 * indices: an index into a filtered list goes stale the moment a session is
 * launched or terminated, a key does not.
 */
export type Overlay = { cols: number; order: string[]; rowSizes: number[]; colSizes: number[][] };

const isNumArray = (v: unknown): v is number[] => Array.isArray(v) && v.every((n) => typeof n === "number");

// Shape check only. Values are not range-checked here because every overlay is
// fed through normalize() before it renders, and normalizeSizes already
// discards track arrays that do not fit the shape.
function isOverlay(v: unknown): v is Overlay {
  if (typeof v !== "object" || v === null) return false;
  const o = v as Record<string, unknown>;
  if (typeof o.cols !== "number") return false;
  if (!Array.isArray(o.order) || !o.order.every((k) => typeof k === "string")) return false;
  if (!isNumArray(o.rowSizes)) return false;
  return Array.isArray(o.colSizes) && o.colSizes.every(isNumArray);
}

function readAll(): Record<string, unknown> {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? "null");
    return typeof raw === "object" && raw !== null && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/** The stored arrangement for one directory, or null to render it as-is. */
export function viewOverlay(dir: string): Overlay | null {
  const entry = readAll()[dir];
  return isOverlay(entry) ? entry : null;
}

export function setViewOverlay(dir: string, overlay: Overlay) {
  localStorage.setItem(KEY, JSON.stringify({ ...readAll(), [dir]: overlay }));
}

/** The keys of a rendered layout's occupied tiles, in the order they appear. */
export function orderOf(view: Layout): string[] {
  return view.tiles.filter((t): t is NonNullable<Tile> => t !== null).map(tileKey);
}

// A directory has no overlay until its first edit; that edit seeds one from
// what is already on screen, so the view inherits the stored column count and
// current sizes instead of jumping to a default.
export function seedOverlay(view: Layout): Overlay {
  return {
    cols: view.shape.cols,
    order: orderOf(view),
    rowSizes: view.rowSizes ?? [],
    colSizes: view.colSizes ?? [],
  };
}

// Reorder a filtered view by its directory's overlay, then re-normalize with
// the overlay's columns and sizes. `map` (each view slot's index in the stored
// layout) is rebuilt alongside the tiles: it is what remove, terminate and
// swap translate through, so it has to follow them.
//
// Keys the overlay does not name sort last and keep their relative stored
// order — Array.prototype.sort is stable and equal ranks compare 0 — which is
// what puts a newly launched session at the end of the view instead of
// somewhere arbitrary. Order entries naming no visible tile simply match
// nothing.
export function applyOverlay(packed: Layout, map: number[], overlay: Overlay | null): { view: Layout; map: number[] } {
  if (!overlay) return { view: packed, map };
  const rank = new Map(overlay.order.map((key, i) => [key, i]));
  const pairs = packed.tiles
    .map((tile, i) => ({ tile, real: map[i] }))
    .filter((p): p is { tile: NonNullable<Tile>; real: number } => p.tile !== null)
    .sort((a, b) => (rank.get(tileKey(a.tile)) ?? Infinity) - (rank.get(tileKey(b.tile)) ?? Infinity));
  return {
    view: normalize(
      pairs.map((p) => p.tile),
      overlay.cols,
      overlay.rowSizes,
      overlay.colSizes,
    ),
    map: pairs.map((p) => p.real),
  };
}

/** Trade two positions in an order. Out-of-range indices are a no-op. */
export function swapOrder(order: string[], a: number, b: number): string[] {
  if (a < 0 || b < 0 || a >= order.length || b >= order.length) return order.slice();
  const out = order.slice();
  [out[a], out[b]] = [out[b], out[a]];
  return out;
}
