import { dirButtons, hiddenDirs, leafName, setHiddenDirs } from "../grid/dirFilter";
import type { Session } from "../grid/types";

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

test("dirButtons sorts by leaf name, then full path", () => {
  const buttons = dirButtons([server("local")], {
    local: [sess(1, "/z/api"), sess(2, "/a/api"), sess(3, "/a/web")],
  });
  expect(buttons.map((b) => b.path)).toEqual(["/a/api", "/z/api", "/a/web"]);
});
