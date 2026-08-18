import { describe, expect, it, vi } from "vitest";
import { computeLinks } from "../term/links";
import { fakeTerminal } from "./helpers/fakeTerm";

const activate = vi.fn();

describe("computeLinks", () => {
  it("finds a url that sits on one row", () => {
    const term = fakeTerminal(40, ["see https://example.com/a here"]);
    const links = computeLinks(term, 1, activate);
    expect(links.map((l) => l.text)).toEqual(["https://example.com/a"]);
    expect(links[0].range).toEqual({ start: { x: 5, y: 1 }, end: { x: 25, y: 1 } });
  });

  it("joins a url tmux split across two rows", () => {
    const term = fakeTerminal(20, ["https://example.com/", "one/two?a=b#frag"]);
    const links = computeLinks(term, 1, activate);
    expect(links.map((l) => l.text)).toEqual(["https://example.com/one/two?a=b#frag"]);
    expect(links[0].range).toEqual({ start: { x: 1, y: 1 }, end: { x: 16, y: 2 } });
  });

  it("finds the same wrapped link when asked about the continuation row", () => {
    const term = fakeTerminal(20, ["https://example.com/", "one/two?a=b#frag"]);
    const links = computeLinks(term, 2, activate);
    expect(links.map((l) => l.text)).toEqual(["https://example.com/one/two?a=b#frag"]);
  });

  it("spans three rows", () => {
    const term = fakeTerminal(10, ["https://ex", "ample.com/", "path"]);
    const links = computeLinks(term, 1, activate);
    expect(links.map((l) => l.text)).toEqual(["https://example.com/path"]);
    expect(links[0].range.end).toEqual({ x: 4, y: 3 });
  });

  it("does not swallow the next row when the row ends short", () => {
    const term = fakeTerminal(20, ["https://example.com", "nonsense"]);
    const links = computeLinks(term, 1, activate);
    expect(links.map((l) => l.text)).toEqual(["https://example.com"]);
  });

  it("ignores stale columns past the terminal width", () => {
    const term = fakeTerminal(20, ["https://example.com/STALETEXT", "one/two?a=b#frag"], 28);
    expect(computeLinks(term, 1, activate).map((l) => l.text)).toEqual(["https://example.com/one/two?a=b#frag"]);
  });

  it("ignores text that is not a url", () => {
    const term = fakeTerminal(20, ["not a link at all"]);
    expect(computeLinks(term, 1, activate)).toEqual([]);
  });

  it("drops trailing punctuation", () => {
    const term = fakeTerminal(40, ["go to https://example.com/x, please"]);
    expect(computeLinks(term, 1, activate).map((l) => l.text)).toEqual(["https://example.com/x"]);
  });

  it("finds two links on one row", () => {
    const term = fakeTerminal(60, ["https://a.example.com https://b.example.com"]);
    expect(computeLinks(term, 1, activate).map((l) => l.text)).toEqual([
      "https://a.example.com",
      "https://b.example.com",
    ]);
  });
});
