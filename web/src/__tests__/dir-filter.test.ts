import { dirButtons, filterLayout, hiddenDirs, leafName, setHiddenDirs } from "../grid/dirFilter";
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

test("hiddenDirs is empty when nothing is stored", () => {
  expect(hiddenDirs()).toEqual(new Set());
});

test("hiddenDirs round-trips through localStorage", () => {
  setHiddenDirs(new Set(["/b", "/a"]));
  expect(localStorage.getItem("multimux.hiddenDirs")).toBe('["/a","/b"]');
  expect(hiddenDirs()).toEqual(new Set(["/a", "/b"]));
});

test("hiddenDirs ignores corrupt storage and non-strings", () => {
  localStorage.setItem("multimux.hiddenDirs", "{oops");
  expect(hiddenDirs()).toEqual(new Set());
  localStorage.setItem("multimux.hiddenDirs", '["/a",7,null]');
  expect(hiddenDirs()).toEqual(new Set(["/a"]));
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

test("dirButtons keeps a button for a hidden directory with no running sessions", () => {
  // Otherwise the last session in a hidden directory ending would take the
  // button with it and leave no way to unhide.
  expect(dirButtons([server("local")], { local: [sess(1, "/a", "dead")] }, new Set(["/a"]))).toEqual([
    { path: "/a", name: "a", count: 0 },
  ]);
  // Even with no sessions known at all — a server that is still loading or
  // unreachable must not make the button disappear.
  expect(dirButtons([server("local")], {}, new Set(["/gone"]))).toEqual([{ path: "/gone", name: "gone", count: 0 }]);
});

test("dirButtons still counts running sessions in a hidden directory", () => {
  expect(dirButtons([server("local")], { local: [sess(1, "/a"), sess(2, "/a")] }, new Set(["/a"]))).toEqual([
    { path: "/a", name: "a", count: 2 },
  ]);
});

test("dirButtons sorts by leaf name, then full path", () => {
  const buttons = dirButtons([server("local")], {
    local: [sess(1, "/z/api"), sess(2, "/a/api"), sess(3, "/a/web")],
  });
  expect(buttons.map((b) => b.path)).toEqual(["/a/api", "/z/api", "/a/web"]);
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
