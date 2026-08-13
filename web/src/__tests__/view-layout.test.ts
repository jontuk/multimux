import {
  applyOverlay,
  orderOf,
  seedOverlay,
  setViewOverlay,
  swapOrder,
  viewOverlay,
  type Overlay,
} from "../grid/viewLayout";
import { normalize } from "../grid/model";

const overlay = (o: Partial<Overlay> = {}): Overlay => ({
  cols: 2,
  order: ["local:1", "local:2"],
  rowSizes: [1],
  colSizes: [[0.7, 0.3]],
  ...o,
});

afterEach(() => localStorage.clear());

test("viewOverlay is null when nothing is stored", () => {
  expect(viewOverlay("/a")).toBeNull();
});

test("overlays round-trip per directory", () => {
  setViewOverlay("/a", overlay());
  setViewOverlay("/b", overlay({ cols: 3, order: ["local:5"] }));
  expect(viewOverlay("/a")?.cols).toBe(2);
  expect(viewOverlay("/a")?.colSizes).toEqual([[0.7, 0.3]]);
  expect(viewOverlay("/b")?.order).toEqual(["local:5"]);
  // One key holds every directory's overlay.
  expect(Object.keys(JSON.parse(localStorage.getItem("multimux.viewLayout")!))).toEqual(["/a", "/b"]);
});

test("writing one directory leaves the others intact", () => {
  setViewOverlay("/a", overlay());
  setViewOverlay("/b", overlay({ cols: 4 }));
  setViewOverlay("/a", overlay({ cols: 1 }));
  expect(viewOverlay("/a")?.cols).toBe(1);
  expect(viewOverlay("/b")?.cols).toBe(4);
});

test("unreadable storage reads as no overlay", () => {
  localStorage.setItem("multimux.viewLayout", "{oops");
  expect(viewOverlay("/a")).toBeNull();
  localStorage.setItem("multimux.viewLayout", "7");
  expect(viewOverlay("/a")).toBeNull();
  localStorage.setItem("multimux.viewLayout", "null");
  expect(viewOverlay("/a")).toBeNull();
});

test("wrong-shaped entries read as no overlay", () => {
  const bad = {
    "/nocols": { order: [], rowSizes: [1], colSizes: [[1]] },
    "/strcols": { cols: "2", order: [], rowSizes: [1], colSizes: [[1]] },
    "/badorder": { cols: 2, order: [1, 2], rowSizes: [1], colSizes: [[1]] },
    "/badrows": { cols: 2, order: [], rowSizes: "x", colSizes: [[1]] },
    "/badcolsizes": { cols: 2, order: [], rowSizes: [1], colSizes: [1] },
  };
  localStorage.setItem("multimux.viewLayout", JSON.stringify(bad));
  for (const key of Object.keys(bad)) expect(viewOverlay(key)).toBeNull();
});

test("a wrong-shaped entry does not poison its neighbours", () => {
  localStorage.setItem("multimux.viewLayout", JSON.stringify({ "/bad": 7, "/good": overlay({ cols: 3 }) }));
  expect(viewOverlay("/bad")).toBeNull();
  expect(viewOverlay("/good")?.cols).toBe(3);
});

test("orderOf lists occupied tiles in view order, skipping empty slots", () => {
  const view = normalize(
    [
      { serverId: "local", sessionId: 5 },
      { serverId: "remote", sessionId: 1 },
    ],
    2,
  );
  expect(orderOf(view)).toEqual(["local:5", "remote:1"]);
  expect(orderOf(normalize([], 2))).toEqual([]);
});

test("seedOverlay captures what is on screen right now", () => {
  const view = normalize(
    [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 3 },
    ],
    2,
    [0.6, 0.4],
  );
  expect(seedOverlay(view)).toEqual({
    cols: 2,
    order: ["local:1", "local:2", "local:3"],
    rowSizes: [0.6, 0.4],
    colSizes: [
      [0.5, 0.5],
      [0.5, 0.5],
    ],
  });
});

// filterLayout's output for three visible tiles that sit at stored indices
// 0, 2 and 3 — a hidden tile at index 1 is what makes the map non-identity.
const packed3 = () => ({
  view: normalize(
    [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
      { serverId: "local", sessionId: 3 },
    ],
    2,
  ),
  map: [0, 2, 3],
});

const keysOf = (view: { tiles: ({ serverId: string; sessionId: number } | null)[] }) =>
  view.tiles.map((t) => (t ? `${t.serverId}:${t.sessionId}` : null));

test("no overlay renders the filtered view untouched", () => {
  const { view, map } = packed3();
  const out = applyOverlay(view, map, null);
  expect(out.view).toBe(view);
  expect(out.map).toBe(map);
});

test("an overlay reorders tiles and carries the map with them", () => {
  const { view, map } = packed3();
  const out = applyOverlay(view, map, {
    cols: 2,
    order: ["local:3", "local:1", "local:2"],
    rowSizes: [],
    colSizes: [],
  });
  expect(keysOf(out.view)).toEqual(["local:3", "local:1", "local:2", null]);
  // Session 3 was at stored index 3, session 1 at 0, session 2 at 2.
  expect(out.map).toEqual([3, 0, 2]);
});

test("keys the overlay does not name append in stored order", () => {
  const { view, map } = packed3();
  const out = applyOverlay(view, map, { cols: 2, order: ["local:3"], rowSizes: [], colSizes: [] });
  expect(keysOf(out.view)).toEqual(["local:3", "local:1", "local:2", null]);
  expect(out.map).toEqual([3, 0, 2]);
});

test("order entries naming no visible tile are ignored", () => {
  const { view, map } = packed3();
  const out = applyOverlay(view, map, {
    cols: 2,
    order: ["local:99", "local:2", "remote:1", "local:1", "local:3"],
    rowSizes: [],
    colSizes: [],
  });
  expect(keysOf(out.view)).toEqual(["local:2", "local:1", "local:3", null]);
  expect(out.map).toEqual([2, 0, 3]);
});

test("the overlay's column count drives the shape and is clamped", () => {
  const { view, map } = packed3();
  expect(applyOverlay(view, map, { cols: 1, order: [], rowSizes: [], colSizes: [] }).view.shape).toEqual({
    rows: 3,
    cols: 1,
  });
  expect(applyOverlay(view, map, { cols: 9, order: [], rowSizes: [], colSizes: [] }).view.shape).toEqual({
    rows: 1,
    cols: 4,
  });
});

test("sizes survive a matching shape and reset when it changes", () => {
  const { view, map } = packed3();
  const kept = applyOverlay(view, map, {
    cols: 2,
    order: [],
    rowSizes: [0.8, 0.2],
    colSizes: [
      [0.3, 0.7],
      [0.5, 0.5],
    ],
  });
  expect(kept.view.rowSizes).toEqual([0.8, 0.2]);
  expect(kept.view.colSizes![0]).toEqual([0.3, 0.7]);

  // Same sizes, one column: the row count no longer matches, so both reset.
  const reset = applyOverlay(view, map, {
    cols: 1,
    order: [],
    rowSizes: [0.8, 0.2],
    colSizes: [
      [0.3, 0.7],
      [0.5, 0.5],
    ],
  });
  expect(reset.view.rowSizes).toEqual([1 / 3, 1 / 3, 1 / 3]);
  expect(reset.view.colSizes).toEqual([[1], [1], [1]]);
});

test("swapOrder trades two positions and leaves the rest alone", () => {
  expect(swapOrder(["a", "b", "c"], 0, 2)).toEqual(["c", "b", "a"]);
  expect(swapOrder(["a", "b", "c"], 1, 1)).toEqual(["a", "b", "c"]);
  // Out-of-range indices are a no-op rather than an undefined hole.
  expect(swapOrder(["a", "b"], 0, 5)).toEqual(["a", "b"]);
  expect(swapOrder(["a", "b"], -1, 1)).toEqual(["a", "b"]);
});
