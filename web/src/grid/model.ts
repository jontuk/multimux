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

export function setTile(layout: Layout, index: number, tile: Tile): Layout {
  const tiles = layout.tiles.slice();
  tiles[index] = tile;
  return normalize(tiles, layout.shape.cols);
}

export function addTile(layout: Layout, tile: NonNullable<Tile>): Layout {
  return normalize([...layout.tiles, tile], layout.shape.cols);
}

export function swapTiles(layout: Layout, a: number, b: number): Layout {
  const tiles = layout.tiles.slice();
  [tiles[a], tiles[b]] = [tiles[b], tiles[a]];
  return normalize(tiles, layout.shape.cols);
}
