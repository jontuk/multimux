import { equalSizes, normalizeSizes, resizeTracks, tileRect, MIN_TILE_PX, SNAP_PX } from "../grid/sizes";

const shape = (rows: number, cols: number) => ({ rows, cols });

test("equalSizes splits evenly and sums to 1", () => {
  expect(equalSizes(1)).toEqual([1]);
  expect(equalSizes(4)).toEqual([0.25, 0.25, 0.25, 0.25]);
  expect(equalSizes(3).reduce((a, b) => a + b, 0)).toBeCloseTo(1, 10);
});

test("normalizeSizes fills in equal sizes when none are stored", () => {
  const s = normalizeSizes(shape(2, 2));
  expect(s.rowSizes).toEqual([0.5, 0.5]);
  expect(s.colSizes).toEqual([
    [0.5, 0.5],
    [0.5, 0.5],
  ]);
});

test("normalizeSizes keeps sizes whose track counts still match", () => {
  const s = normalizeSizes(
    shape(2, 2),
    [0.3, 0.7],
    [
      [0.35, 0.65],
      [0.8, 0.2],
    ],
  );
  expect(s.rowSizes).toEqual([0.3, 0.7]);
  expect(s.colSizes).toEqual([
    [0.35, 0.65],
    [0.8, 0.2],
  ]);
});

test("normalizeSizes resets row heights when the row count changed, keeping widths", () => {
  const s = normalizeSizes(
    shape(3, 2),
    [0.3, 0.7],
    [
      [0.35, 0.65],
      [0.8, 0.2],
    ],
  );
  expect(s.rowSizes).toEqual([1 / 3, 1 / 3, 1 / 3]);
  // Surviving rows keep their widths; the new row starts equal.
  expect(s.colSizes).toEqual([
    [0.35, 0.65],
    [0.8, 0.2],
    [0.5, 0.5],
  ]);
});

test("normalizeSizes resets every row's widths when the column count changed", () => {
  const s = normalizeSizes(
    shape(2, 3),
    [0.3, 0.7],
    [
      [0.35, 0.65],
      [0.8, 0.2],
    ],
  );
  expect(s.rowSizes).toEqual([0.3, 0.7]);
  expect(s.colSizes).toEqual([
    [1 / 3, 1 / 3, 1 / 3],
    [1 / 3, 1 / 3, 1 / 3],
  ]);
});

test("normalizeSizes repairs malformed input", () => {
  const s = normalizeSizes(
    shape(2, 2),
    [0.9, 0.9],
    [
      [Number.NaN, 0.5],
      [-1, 2],
    ],
  );
  expect(s.rowSizes).toEqual([0.5, 0.5]);
  expect(s.colSizes).toEqual([
    [0.5, 0.5],
    [0.5, 0.5],
  ]);
});

test("resizeTracks moves one boundary and leaves other tracks alone", () => {
  const out = resizeTracks([0.25, 0.25, 0.25, 0.25], 1, 0.1, 0.05);
  expect(out[0]).toBeCloseTo(0.25, 10);
  expect(out[1]).toBeCloseTo(0.35, 10);
  expect(out[2]).toBeCloseTo(0.15, 10);
  expect(out[3]).toBeCloseTo(0.25, 10);
  expect(out.reduce((a, b) => a + b, 0)).toBeCloseTo(1, 10);
});

test("resizeTracks clamps so neither neighbour falls below the minimum", () => {
  const out = resizeTracks([0.5, 0.5], 0, 0.9, 0.1);
  expect(out[0]).toBeCloseTo(0.9, 10);
  expect(out[1]).toBeCloseTo(0.1, 10);
  const back = resizeTracks([0.5, 0.5], 0, -0.9, 0.1);
  expect(back[0]).toBeCloseTo(0.1, 10);
  expect(back[1]).toBeCloseTo(0.9, 10);
});

test("resizeTracks snaps to the equal split when close, and not when far", () => {
  // snap: the pair [0.5,0.5] is equal, so a 0.004 nudge with a 0.01 snap
  // threshold lands exactly back on the equal value.
  const snapped = resizeTracks([0.5, 0.5], 0, 0.004, 0.05, 0.01);
  expect(snapped).toEqual([0.5, 0.5]);
  const free = resizeTracks([0.5, 0.5], 0, 0.05, 0.05, 0.01);
  expect(free[0]).toBeCloseTo(0.55, 10);
});

test("tileRect places a tile from row heights and that row's widths", () => {
  const rows = [0.3, 0.7];
  const cols = [
    [0.35, 0.65],
    [0.8, 0.2],
  ];
  expect(tileRect(shape(2, 2), rows, cols, 0)).toEqual({ left: 0, top: 0, width: 35, height: 30 });
  expect(tileRect(shape(2, 2), rows, cols, 1)).toEqual({ left: 35, top: 0, width: 65, height: 30 });
  expect(tileRect(shape(2, 2), rows, cols, 3)).toEqual({ left: 80, top: 30, width: 20, height: 70 });
});

test("exports the pixel constants the drag layer clamps with", () => {
  expect(MIN_TILE_PX).toBe(120);
  expect(SNAP_PX).toBe(8);
});
