import { describe, expect, it } from "vitest";
import { isContinued, selectedText, selectionToText } from "../term/wrap";
import { fakeTerminal } from "./helpers/fakeTerm";

describe("isContinued", () => {
  it("treats a row whose last cell is filled as continued by the next row", () => {
    const term = fakeTerminal(10, ["abcdefghij", "klm"]);
    expect(isContinued(term, 0)).toBe(true);
  });

  it("does not continue a row that ends in a blank cell", () => {
    const term = fakeTerminal(10, ["abcdefghi", "klm"]);
    expect(isContinued(term, 0)).toBe(false);
  });

  it("does not continue into a blank row", () => {
    const term = fakeTerminal(10, ["abcdefghij", ""]);
    expect(isContinued(term, 0)).toBe(false);
  });

  it("does not continue past the last row", () => {
    const term = fakeTerminal(10, ["abcdefghij"]);
    expect(isContinued(term, 0)).toBe(false);
  });

  it("honours a real isWrapped flag even when the row is short", () => {
    const term = fakeTerminal(10, ["abc", "def"]);
    (term.buffer.active.getLine(1) as { isWrapped: boolean }).isWrapped = true;
    expect(isContinued(term, 0)).toBe(true);
  });
});

describe("selectionToText", () => {
  it("joins a tmux-wrapped row to the next with no newline", () => {
    const term = fakeTerminal(10, ["https://ex", "ample.com"]);
    const text = selectionToText(term, { start: { x: 0, y: 0 }, end: { x: 9, y: 1 } });
    expect(text).toBe("https://example.com");
  });

  it("keeps the newline between unrelated rows", () => {
    const term = fakeTerminal(10, ["one", "two"]);
    const text = selectionToText(term, { start: { x: 0, y: 0 }, end: { x: 3, y: 1 } });
    expect(text).toBe("one\ntwo");
  });

  it("trims trailing blanks on rows that end the line", () => {
    const term = fakeTerminal(10, ["one", "two"]);
    const text = selectionToText(term, { start: { x: 0, y: 0 }, end: { x: 10, y: 1 } });
    expect(text).toBe("one\ntwo");
  });

  it("honours the selection's start and end columns", () => {
    const term = fakeTerminal(10, ["abcdefghij", "klmnop"]);
    const text = selectionToText(term, { start: { x: 2, y: 0 }, end: { x: 3, y: 1 } });
    expect(text).toBe("cdefghijklm");
  });

  it("returns a single row unchanged", () => {
    const term = fakeTerminal(10, ["hello"]);
    const text = selectionToText(term, { start: { x: 0, y: 0 }, end: { x: 5, y: 0 } });
    expect(text).toBe("hello");
  });
});

describe("selectedText", () => {
  it("falls back to xterm's own selection when there is no range", () => {
    const term = fakeTerminal(10, ["hello"]);
    Object.assign(term, { getSelection: () => "raw", getSelectionPosition: () => undefined });
    expect(selectedText(term)).toBe("raw");
  });

  it("rebuilds the selection from the buffer when a range exists", () => {
    const term = fakeTerminal(10, ["https://ex", "ample.com"]);
    Object.assign(term, {
      getSelection: () => "https://ex\nample.com",
      getSelectionPosition: () => ({ start: { x: 0, y: 0 }, end: { x: 9, y: 1 } }),
    });
    expect(selectedText(term)).toBe("https://example.com");
  });
});

// After a resize, xterm can hand back lines whose length still reflects the
// old width, with stale text in the cells past the current one.
describe("stale-width rows", () => {
  it("ignores columns past the terminal width", () => {
    const term = fakeTerminal(10, ["https://exSTALE", "ample.com"], 15);
    expect(selectionToText(term, { start: { x: 0, y: 0 }, end: { x: 9, y: 1 } })).toBe("https://example.com");
  });
});
