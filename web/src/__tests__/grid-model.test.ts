import { emptyLayout, reshape, setTile, swapTiles, SHAPES } from "../grid/model";

test("emptyLayout is 1x1 null", () => {
  const l = emptyLayout();
  expect(l.shape).toEqual({ rows: 1, cols: 1 });
  expect(l.tiles).toEqual([null]);
});

test("SHAPES contains the six designed shapes", () => {
  expect(SHAPES).toHaveLength(6);
  expect(SHAPES).toContainEqual({ rows: 2, cols: 3 });
});

test("reshape grows with null padding", () => {
  let l = emptyLayout();
  l = setTile(l, 0, { serverId: "local", sessionId: 1 });
  l = reshape(l, { rows: 2, cols: 2 });
  expect(l.tiles).toHaveLength(4);
  expect(l.tiles[0]).toEqual({ serverId: "local", sessionId: 1 });
  expect(l.tiles.slice(1)).toEqual([null, null, null]);
});

test("reshape shrink keeps non-null tiles first", () => {
  let l = reshape(emptyLayout(), { rows: 2, cols: 2 });
  l = setTile(l, 3, { serverId: "local", sessionId: 7 });
  l = reshape(l, { rows: 1, cols: 1 });
  // The only occupied tile survives the shrink.
  expect(l.tiles).toEqual([{ serverId: "local", sessionId: 7 }]);
});

test("reshape shrink drops overflow tiles beyond capacity", () => {
  let l = reshape(emptyLayout(), { rows: 2, cols: 2 });
  for (let i = 0; i < 4; i++) l = setTile(l, i, { serverId: "local", sessionId: i + 1 });
  l = reshape(l, { rows: 1, cols: 2 });
  expect(l.tiles).toEqual([
    { serverId: "local", sessionId: 1 },
    { serverId: "local", sessionId: 2 },
  ]);
});

test("swapTiles", () => {
  let l = reshape(emptyLayout(), { rows: 1, cols: 2 });
  l = setTile(l, 0, { serverId: "local", sessionId: 1 });
  l = swapTiles(l, 0, 1);
  expect(l.tiles[0]).toBeNull();
  expect(l.tiles[1]).toEqual({ serverId: "local", sessionId: 1 });
});
