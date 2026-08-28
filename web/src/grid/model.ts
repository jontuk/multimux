import { equalSizes, normalizeSizes } from "./sizes";

export type GridShape = { rows: number; cols: number };
export type Tile = { serverId: string; sessionId: number } | null;
export type Layout = {
  shape: GridShape;
  tiles: Tile[];
  /** One fraction per row, summing to 1. Absent in documents written before splitters. */
  rowSizes?: number[];
  /** One fraction array per row, each summing to 1. */
  colSizes?: number[][];
};

export const MIN_COLS = 1;
export const MAX_COLS = 4;

export function clampCols(cols: number): number {
  return Math.min(MAX_COLS, Math.max(MIN_COLS, Math.floor(cols)));
}

// Canonical layout form: occupied tiles packed to the front in row-major
// order, rows derived as just enough to hold them (min 1), trailing cells
// padded with nulls. Sessions are never dropped — the grid grows instead.
// Sizes ride along: they are kept when their track count survives and reset
// to equal otherwise (see normalizeSizes), so this is the one place a layout
// can acquire sizes that disagree with its shape.
export function normalize(tiles: Tile[], cols: number, rowSizes?: number[], colSizes?: number[][]): Layout {
  const c = clampCols(cols);
  const occupied = tiles.filter((t): t is NonNullable<Tile> => t !== null);
  const rows = Math.max(1, Math.ceil(occupied.length / c));
  const padded: Tile[] = [...occupied];
  while (padded.length < rows * c) padded.push(null);
  const shape = { rows, cols: c };
  return { shape, tiles: padded, ...normalizeSizes(shape, rowSizes, colSizes) };
}

export function emptyLayout(): Layout {
  return normalize([], 2);
}

export function setCols(layout: Layout, cols: number): Layout {
  return normalize(layout.tiles, cols, layout.rowSizes, layout.colSizes);
}

// Removing a tile can leave a whole column empty: tiles pack row-major, so
// every column holds something unless the occupied tiles fit in a single row
// (rows === 1, count < cols). Narrow the grid to what is left so the survivors
// grow into the space instead of a dead column sitting there. Only shrinks —
// the column count the user picked is kept whenever it is still in use.
export function removeTile(layout: Layout, index: number): Layout {
  const tiles = layout.tiles.slice();
  tiles[index] = null;
  return packAfterRemoval(layout, tiles);
}

// The same removal for a whole set of tiles at once. Removing them one index at
// a time would be wrong: each pass repacks, so every index after the first goes
// stale. Naming the tiles instead of their positions sidesteps that.
export function removeTilesWhere(layout: Layout, drop: (tile: NonNullable<Tile>) => boolean): Layout {
  return packAfterRemoval(
    layout,
    layout.tiles.map((t) => (t && drop(t) ? null : t)),
  );
}

function packAfterRemoval(layout: Layout, tiles: Tile[]): Layout {
  const count = tiles.filter((t) => t !== null).length;
  return normalize(tiles, Math.min(layout.shape.cols, Math.max(1, count)), layout.rowSizes, layout.colSizes);
}

// The mirror of removeTile's shrink: dropping to one session narrows the grid
// to a single column, so the second session added back would stack under it
// rather than beside it. A lone session is never a column count the user
// chose — it is what removeTile left behind — so widen to two and put the pair
// side by side. Only this one step: beyond two, the stored column count stands.
export function addTile(layout: Layout, tile: NonNullable<Tile>): Layout {
  const count = layout.tiles.filter((t) => t !== null).length;
  const cols = count === 1 && layout.shape.cols === 1 ? 2 : layout.shape.cols;
  return normalize([...layout.tiles, tile], cols, layout.rowSizes, layout.colSizes);
}

export function swapTiles(layout: Layout, a: number, b: number): Layout {
  const tiles = layout.tiles.slice();
  [tiles[a], tiles[b]] = [tiles[b], tiles[a]];
  return normalize(tiles, layout.shape.cols, layout.rowSizes, layout.colSizes);
}

/** Replace the row heights, re-normalized against the current shape. */
export function setRowSizes(layout: Layout, rowSizes: number[]): Layout {
  return normalize(layout.tiles, layout.shape.cols, rowSizes, layout.colSizes);
}

/** Replace one row's column widths, leaving every other row untouched. */
export function setColSizes(layout: Layout, row: number, sizes: number[]): Layout {
  const cols = (layout.colSizes ?? []).slice();
  while (cols.length < layout.shape.rows) cols.push(equalSizes(layout.shape.cols));
  cols[row] = sizes;
  return normalize(layout.tiles, layout.shape.cols, layout.rowSizes, cols);
}

/** Stable identity for a tile: its session, on its server. */
export function tileKey(t: NonNullable<Tile>): string {
  return `${t.serverId}:${t.sessionId}`;
}
