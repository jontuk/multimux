import { cycleSolo, dirButtons, effectiveSolo, filterLayout, leafName, setSoloDir, soloDir } from "../grid/dirFilter";
import type { Session } from "../grid/types";
import { normalize } from "../grid/model";

const server = (id: string) => ({ id, origin: "https://x", name: id });

const sess = (id: number, dir: string, status = "running"): Session => ({
  id,
  tmuxName: `mm-${id}`,
  toolId: 1,
  dir,
  status,
});

afterEach(() => localStorage.clear());

test("soloDir is null when nothing is stored", () => {
  expect(soloDir()).toBeNull();
});

test("soloDir round-trips through localStorage", () => {
  setSoloDir("/a");
  expect(localStorage.getItem("multimux.soloDir")).toBe('"/a"');
  expect(soloDir()).toBe("/a");
  setSoloDir(null);
  expect(localStorage.getItem("multimux.soloDir")).toBe("null");
  expect(soloDir()).toBeNull();
});

test("soloDir ignores corrupt storage and non-strings", () => {
  localStorage.setItem("multimux.soloDir", "{oops");
  expect(soloDir()).toBeNull();
  localStorage.setItem("multimux.soloDir", "7");
  expect(soloDir()).toBeNull();
});

test("soloDir ignores a value left by the old hidden-dirs behaviour", () => {
  localStorage.setItem("multimux.hiddenDirs", '["/a"]');
  expect(soloDir()).toBeNull();
});

test("leafName takes the last path segment", () => {
  expect(leafName("/Users/jon/Repos/multimux")).toBe("multimux");
  expect(leafName("/Users/jon/Repos/multimux/")).toBe("multimux");
  expect(leafName("/")).toBe("/");
});

test("dirButtons groups running sessions by full path and counts them", () => {
  const buttons = dirButtons([server("local"), server("mini")], {
    local: [sess(1, "/Users/jon/Repos/multimux"), sess(2, "/Users/jon/Repos/multimux"), sess(3, "/Users/jon/old")],
    mini: [sess(4, "/Users/jon/Repos/multimux")],
  });
  expect(buttons).toEqual([
    { path: "/Users/jon/Repos/multimux", name: "multimux", count: 3 },
    { path: "/Users/jon/old", name: "old", count: 1 },
  ]);
});

test("dirButtons skips sessions that are not running", () => {
  expect(dirButtons([server("local")], { local: [sess(1, "/a", "dead")] })).toEqual([]);
});

test("dirButtons sorts by leaf name, then full path", () => {
  const buttons = dirButtons([server("local")], {
    local: [sess(1, "/z/api"), sess(2, "/a/api"), sess(3, "/a/web")],
  });
  expect(buttons.map((b) => b.path)).toEqual(["/a/api", "/z/api", "/a/web"]);
});

const button = (path: string) => ({ path, name: leafName(path), count: 1 });

test("effectiveSolo is null when nothing is soloed", () => {
  expect(effectiveSolo(null, [button("/a")])).toBeNull();
});

test("effectiveSolo is in effect when the soloed path still has a button", () => {
  expect(effectiveSolo("/a", [button("/a"), button("/b")])).toBe("/a");
});

test("effectiveSolo falls back to showing everything when the button is gone", () => {
  // The stored value is deliberately left alone: the selection returns when
  // its directory does (sessions still loading, a remote daemon offline).
  setSoloDir("/a");
  expect(effectiveSolo("/a", [button("/b")])).toBeNull();
  expect(effectiveSolo("/a", [])).toBeNull();
  expect(soloDir()).toBe("/a");
});

test("cycleSolo walks forward from show-all through the buttons and back", () => {
  const dirs = [button("/a"), button("/b")];
  expect(cycleSolo(null, dirs, 1)).toBe("/a");
  expect(cycleSolo("/a", dirs, 1)).toBe("/b");
  expect(cycleSolo("/b", dirs, 1)).toBeNull();
});

test("cycleSolo walks backward through the same ring", () => {
  const dirs = [button("/a"), button("/b")];
  expect(cycleSolo(null, dirs, -1)).toBe("/b");
  expect(cycleSolo("/b", dirs, -1)).toBe("/a");
  expect(cycleSolo("/a", dirs, -1)).toBeNull();
});

test("cycleSolo shows everything when there is nothing to cycle through", () => {
  expect(cycleSolo(null, [], 1)).toBeNull();
  expect(cycleSolo("/a", [], -1)).toBeNull();
});

test("cycleSolo treats a path with no button as show-all", () => {
  // effectiveSolo already made it so on screen; the ring must agree, or the
  // first press would land on the second button.
  expect(cycleSolo("/gone", [button("/a"), button("/b")], 1)).toBe("/a");
});

const tile = (id: number) => ({ serverId: "local", sessionId: id });

test("filterLayout is the identity when everything is visible", () => {
  const layout = normalize([tile(1), tile(2), tile(3)], 2);
  const { view, map } = filterLayout(layout, () => true);
  expect(view).toEqual(layout);
  expect(map).toEqual([0, 1, 2]);
});

test("filterLayout repacks the survivors and shrinks the row count", () => {
  const layout = normalize([tile(1), tile(2), tile(3), tile(4)], 2);
  const { view, map } = filterLayout(layout, (t) => t.sessionId % 2 === 1);
  expect(view.shape).toEqual({ rows: 1, cols: 2 });
  expect(view.tiles).toEqual([tile(1), tile(3)]);
  expect(map).toEqual([0, 2]);
});

test("filterLayout keeps at least one empty row when everything is hidden", () => {
  const layout = normalize([tile(1), tile(2)], 2);
  const { view, map } = filterLayout(layout, () => false);
  expect(view.shape).toEqual({ rows: 1, cols: 2 });
  expect(view.tiles).toEqual([null, null]);
  expect(map).toEqual([]);
});

test("filterLayout leaves no map entry for empty view slots", () => {
  const layout = normalize([tile(1), tile(2), tile(3)], 2);
  const { view, map } = filterLayout(layout, (t) => t.sessionId === 3);
  expect(view.tiles).toEqual([tile(3), null]);
  expect(map[1]).toBeUndefined();
});
