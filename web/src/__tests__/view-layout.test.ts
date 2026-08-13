import { orderOf, seedOverlay, setViewOverlay, viewOverlay, type Overlay } from "../grid/viewLayout";
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
