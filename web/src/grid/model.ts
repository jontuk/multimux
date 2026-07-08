export type GridShape = { rows: number; cols: number };
export type Tile = { serverId: string; sessionId: number } | null;
export type Layout = { shape: GridShape; tiles: Tile[] };

export const SHAPES: GridShape[] = [
  { rows: 1, cols: 1 },
  { rows: 1, cols: 2 },
  { rows: 2, cols: 1 },
  { rows: 2, cols: 2 },
  { rows: 2, cols: 3 },
  { rows: 3, cols: 3 },
];

export function emptyLayout(): Layout {
  return { shape: { rows: 1, cols: 1 }, tiles: [null] };
}

// reshape keeps tiles in row-major order. On shrink, occupied tiles are
// packed to the front so sessions are not silently dropped while empty slots
// remain; overflow beyond the new capacity is dropped.
export function reshape(layout: Layout, shape: GridShape): Layout {
  const capacity = shape.rows * shape.cols;
  let tiles = layout.tiles.slice(0, capacity);
  const dropped = layout.tiles.slice(capacity).filter((t) => t !== null);
  if (dropped.length > 0) {
    tiles = [...layout.tiles.filter((t) => t !== null)].slice(0, capacity);
  }
  while (tiles.length < capacity) tiles.push(null);
  return { shape, tiles };
}

export function setTile(layout: Layout, index: number, tile: Tile): Layout {
  const tiles = layout.tiles.slice();
  tiles[index] = tile;
  return { ...layout, tiles };
}

export function swapTiles(layout: Layout, a: number, b: number): Layout {
  const tiles = layout.tiles.slice();
  [tiles[a], tiles[b]] = [tiles[b], tiles[a]];
  return { ...layout, tiles };
}
