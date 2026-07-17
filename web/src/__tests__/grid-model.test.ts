import { addTile, emptyLayout, normalize, setCols, setTile, swapTiles, MAX_COLS, MIN_COLS } from "../grid/model";

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

test("setTile null removes a session and shrinks rows", () => {
  let l = normalize([sess(1), sess(2), sess(3)], 2);
  expect(l.shape.rows).toBe(2);
  l = setTile(l, 1, null);
  expect(l.shape).toEqual({ rows: 1, cols: 2 });
  expect(l.tiles).toEqual([sess(1), sess(3)]);
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
