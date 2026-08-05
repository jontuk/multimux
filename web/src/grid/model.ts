export type GridShape = { rows: number; cols: number };
export type Tile = { serverId: string; sessionId: number } | null;
export type Layout = { shape: GridShape; tiles: Tile[] };

export const MIN_COLS = 1;
export const MAX_COLS = 4;

export function clampCols(cols: number): number {
  return Math.min(MAX_COLS, Math.max(MIN_COLS, Math.floor(cols)));
}

// Canonical layout form: occupied tiles packed to the front in row-major
// order, rows derived as just enough to hold them (min 1), trailing cells
// padded with nulls. Sessions are never dropped — the grid grows instead.
export function normalize(tiles: Tile[], cols: number): Layout {
  const c = clampCols(cols);
  const occupied = tiles.filter((t): t is NonNullable<Tile> => t !== null);
  const rows = Math.max(1, Math.ceil(occupied.length / c));
  const padded: Tile[] = [...occupied];
  while (padded.length < rows * c) padded.push(null);
  return { shape: { rows, cols: c }, tiles: padded };
}

export function emptyLayout(): Layout {
  return normalize([], 2);
}

export function setCols(layout: Layout, cols: number): Layout {
  return normalize(layout.tiles, cols);
}

// Removing a tile can leave a whole column empty: tiles pack row-major, so
// every column holds something unless the occupied tiles fit in a single row
// (rows === 1, count < cols). Narrow the grid to what is left so the survivors
// grow into the space instead of a dead column sitting there. Only shrinks —
// the column count the user picked is kept whenever it is still in use.
export function removeTile(layout: Layout, index: number): Layout {
  const tiles = layout.tiles.slice();
  tiles[index] = null;
  const count = tiles.filter((t) => t !== null).length;
  return normalize(tiles, Math.min(layout.shape.cols, Math.max(1, count)));
}

export function addTile(layout: Layout, tile: NonNullable<Tile>): Layout {
  return normalize([...layout.tiles, tile], layout.shape.cols);
}

export function swapTiles(layout: Layout, a: number, b: number): Layout {
  const tiles = layout.tiles.slice();
  [tiles[a], tiles[b]] = [tiles[b], tiles[a]];
  return normalize(tiles, layout.shape.cols);
}
