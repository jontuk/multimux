import type { GridShape } from "./model";

/** Smallest a tile may be squeezed to on the drag axis (~15 cols / 4 rows). */
export const MIN_TILE_PX = 120;
/** A boundary within this many pixels of its equal split takes the exact equal value. */
export const SNAP_PX = 8;

const EPS = 1e-6;

export function equalSizes(n: number): number[] {
  const count = Math.max(1, Math.floor(n));
  return Array.from({ length: count }, () => 1 / count);
}

// A stored track array is usable only if it has the expected length, every
// entry is a positive finite number, and the whole thing still sums to 1.
// Anything else is a layout document we did not write (or one we truncated),
// and equal sizes are always a safe answer.
function usable(sizes: number[] | undefined, count: number): sizes is number[] {
  if (!Array.isArray(sizes) || sizes.length !== count) return false;
  if (!sizes.every((v) => typeof v === "number" && Number.isFinite(v) && v > 0)) return false;
  return Math.abs(sizes.reduce((a, b) => a + b, 0) - 1) < EPS;
}

// Keep what still fits: row heights survive an unchanged row count, and each
// row's widths survive an unchanged column count. Rows are identified by
// index, so a row appended at the bottom starts equal while the rows above it
// keep the widths the user dragged.
export function normalizeSizes(
  shape: GridShape,
  rowSizes?: number[],
  colSizes?: number[][],
): { rowSizes: number[]; colSizes: number[][] } {
  const rows = usable(rowSizes, shape.rows) ? rowSizes.slice() : equalSizes(shape.rows);
  const cols = Array.from({ length: shape.rows }, (_, r) => {
    const stored = Array.isArray(colSizes) ? colSizes[r] : undefined;
    return usable(stored, shape.cols) ? stored.slice() : equalSizes(shape.cols);
  });
  return { rowSizes: rows, colSizes: cols };
}

// Move the boundary between track `boundary` and `boundary + 1`. The delta is
// taken from exactly those two tracks so no other tile shifts, and is clamped
// so neither of them drops below `min`.
export function resizeTracks(sizes: number[], boundary: number, delta: number, min: number, snap = 0): number[] {
  if (boundary < 0 || boundary + 1 >= sizes.length) return sizes.slice();
  const a = sizes[boundary];
  const b = sizes[boundary + 1];
  const pair = a + b;
  const lo = Math.min(min, pair / 2);
  const hi = pair - lo;
  let next = Math.min(hi, Math.max(lo, a + delta));
  // Snap to equal *between the two adjacent tracks* (pair / 2), not an equal
  // split of the whole axis — that's the boundary the user is holding.
  if (snap > 0 && Math.abs(next - pair / 2) <= snap) next = pair / 2;
  const out = sizes.slice();
  out[boundary] = next;
  out[boundary + 1] = pair - next;
  return out;
}

/** Geometry for one tile, in percent of the grid container. */
export function tileRect(
  shape: GridShape,
  rowSizes: number[],
  colSizes: number[][],
  index: number,
): { left: number; top: number; width: number; height: number } {
  const row = Math.floor(index / shape.cols);
  const col = index % shape.cols;
  const widths = colSizes[row] ?? equalSizes(shape.cols);
  const top = rowSizes.slice(0, row).reduce((a, b) => a + b, 0);
  const left = widths.slice(0, col).reduce((a, b) => a + b, 0);
  return {
    left: left * 100,
    top: top * 100,
    width: widths[col] * 100,
    height: rowSizes[row] * 100,
  };
}
