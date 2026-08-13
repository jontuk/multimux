import {
  addTile,
  removeTile,
  emptyLayout,
  normalize,
  setCols,
  setColSizes,
  setRowSizes,
  swapTiles,
  tileKey,
  MAX_COLS,
  MIN_COLS,
} from "../grid/model";

const sess = (id: number) => ({ serverId: "local", sessionId: id });

test("emptyLayout is one empty row of 2 columns", () => {
  const l = emptyLayout();
  expect(l.shape).toEqual({ rows: 1, cols: 2 });
  expect(l.tiles).toEqual([null, null]);
});

test("normalize packs occupied tiles to the front and derives rows", () => {
  const l = normalize([null, sess(1), null, sess(2), sess(3)], 2);
  expect(l.shape).toEqual({ rows: 2, cols: 2 });
  expect(l.tiles).toEqual([sess(1), sess(2), sess(3), null]);
});

test("normalize keeps at least one row when empty", () => {
  const l = normalize([], 3);
  expect(l.shape).toEqual({ rows: 1, cols: 3 });
  expect(l.tiles).toEqual([null, null, null]);
});

test("setCols clamps to bounds and never drops sessions", () => {
  let l = normalize([sess(1), sess(2), sess(3)], 3);
  l = setCols(l, 0);
  expect(l.shape.cols).toBe(MIN_COLS);
  l = setCols(l, 99);
  expect(l.shape.cols).toBe(MAX_COLS);
  expect(l.tiles.filter((t) => t !== null)).toHaveLength(3);
});

test("setCols shrink grows rows to hold all sessions", () => {
  let l = normalize([sess(1), sess(2), sess(3), sess(4)], 2);
  expect(l.shape).toEqual({ rows: 2, cols: 2 });
  l = setCols(l, 1);
  expect(l.shape).toEqual({ rows: 4, cols: 1 });
  expect(l.tiles).toEqual([sess(1), sess(2), sess(3), sess(4)]);
});

test("addTile appends a new row when the grid is full", () => {
  let l = normalize([sess(1), sess(2)], 2);
  expect(l.shape.rows).toBe(1);
  l = addTile(l, sess(3));
  expect(l.shape).toEqual({ rows: 2, cols: 2 });
  expect(l.tiles).toEqual([sess(1), sess(2), sess(3), null]);
});

test("removeTile drops a session and shrinks rows", () => {
  let l = normalize([sess(1), sess(2), sess(3)], 2);
  expect(l.shape.rows).toBe(2);
  l = removeTile(l, 1);
  expect(l.shape).toEqual({ rows: 1, cols: 2 });
  expect(l.tiles).toEqual([sess(1), sess(3)]);
});

test("removeTile narrows the grid when a column would be left empty", () => {
  let l = normalize([sess(1), sess(2), sess(3)], 3);
  l = removeTile(l, 2);
  expect(l.shape).toEqual({ rows: 1, cols: 2 });
  expect(l.tiles).toEqual([sess(1), sess(2)]);

  l = removeTile(l, 0);
  expect(l.shape).toEqual({ rows: 1, cols: 1 });
  expect(l.tiles).toEqual([sess(2)]);
});

test("removeTile keeps the column count while every column is still used", () => {
  let l = normalize([sess(1), sess(2), sess(3), sess(4), sess(5)], 3);
  expect(l.shape).toEqual({ rows: 2, cols: 3 });
  l = removeTile(l, 0);
  // 4 tiles over 3 columns: no column is empty, so the width stays.
  expect(l.shape).toEqual({ rows: 2, cols: 3 });
  expect(l.tiles).toEqual([sess(2), sess(3), sess(4), sess(5), null, null]);
});

test("removing the last session collapses to a single column", () => {
  let l = normalize([sess(1)], 3);
  l = removeTile(l, 0);
  expect(l.shape).toEqual({ rows: 1, cols: 1 });
  expect(l.tiles).toEqual([null]);
});

test("swapTiles reorders occupied tiles", () => {
  let l = normalize([sess(1), sess(2)], 2);
  l = swapTiles(l, 0, 1);
  expect(l.tiles).toEqual([sess(2), sess(1)]);
});

test("swapTiles with a trailing empty slot moves the session to the end", () => {
  let l = normalize([sess(1), sess(2), sess(3)], 2);
  l = swapTiles(l, 0, 3);
  expect(l.tiles).toEqual([sess(2), sess(3), sess(1), null]);
});

test("normalize fills in equal sizes and derives one width array per row", () => {
  const l = normalize([sess(1), sess(2), sess(3)], 2);
  expect(l.rowSizes).toEqual([0.5, 0.5]);
  expect(l.colSizes).toEqual([
    [0.5, 0.5],
    [0.5, 0.5],
  ]);
});

test("swapTiles keeps the dragged sizes because the shape is unchanged", () => {
  let l = normalize([sess(1), sess(2), sess(3), sess(4)], 2);
  l = setRowSizes(l, [0.3, 0.7]);
  l = setColSizes(l, 1, [0.8, 0.2]);
  const swapped = swapTiles(l, 0, 3);
  expect(swapped.rowSizes).toEqual([0.3, 0.7]);
  expect(swapped.colSizes).toEqual([
    [0.5, 0.5],
    [0.8, 0.2],
  ]);
  expect(swapped.tiles).toEqual([sess(4), sess(2), sess(3), sess(1)]);
});

test("setCols resets every row's widths but keeps row heights when rows survive", () => {
  let l = normalize([sess(1), sess(2), sess(3), sess(4), sess(5), sess(6)], 3);
  l = setRowSizes(l, [0.25, 0.75]);
  l = setColSizes(l, 0, [0.2, 0.3, 0.5]);
  const wider = setCols(l, 2); // 6 tiles / 2 cols = 3 rows: both axes change
  expect(wider.shape).toEqual({ rows: 3, cols: 2 });
  expect(wider.rowSizes).toEqual([1 / 3, 1 / 3, 1 / 3]);
  expect(wider.colSizes).toEqual([
    [0.5, 0.5],
    [0.5, 0.5],
    [0.5, 0.5],
  ]);
});

test("addTile into an existing row keeps both axes", () => {
  let l = normalize([sess(1), sess(2), sess(3)], 2);
  l = setRowSizes(l, [0.3, 0.7]);
  l = setColSizes(l, 0, [0.35, 0.65]);
  const grown = addTile(l, sess(4)); // fills the trailing null; shape unchanged
  expect(grown.shape).toEqual({ rows: 2, cols: 2 });
  expect(grown.rowSizes).toEqual([0.3, 0.7]);
  expect(grown.colSizes).toEqual([
    [0.35, 0.65],
    [0.5, 0.5],
  ]);
});

test("removeTile that drops a row re-equalizes heights and keeps surviving widths", () => {
  let l = normalize([sess(1), sess(2), sess(3)], 2);
  l = setRowSizes(l, [0.3, 0.7]);
  l = setColSizes(l, 0, [0.35, 0.65]);
  const smaller = removeTile(l, 2); // back to a single full row
  expect(smaller.shape).toEqual({ rows: 1, cols: 2 });
  expect(smaller.rowSizes).toEqual([1]);
  expect(smaller.colSizes).toEqual([[0.35, 0.65]]);
});

test("setColSizes only touches the row it names", () => {
  const l = setColSizes(normalize([sess(1), sess(2), sess(3)], 2), 1, [0.9, 0.1]);
  expect(l.colSizes).toEqual([
    [0.5, 0.5],
    [0.9, 0.1],
  ]);
});

test("tileKey joins server and session id", () => {
  expect(tileKey({ serverId: "local", sessionId: 3 })).toBe("local:3");
  expect(tileKey({ serverId: "https://box:8686", sessionId: 1 })).toBe("https://box:8686:1");
});
