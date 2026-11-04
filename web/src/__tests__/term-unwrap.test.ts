import { describe, expect, it } from "vitest";
import { softBreak, unwrapText } from "../term/unwrap";

const COLS = 60;

describe("unwrapText", () => {
  it("joins rows an application wrapped, dropping the padding and indent", () => {
    const text = [
      "  They also never get marked stale. Any tick clears the ",
      "  resubscribed flag in watchdog.py, and SKEW looks to tick ",
      "  once after each resubscribe.",
    ].join("\n");
    expect(unwrapText(text, COLS)).toBe(
      "  They also never get marked stale. Any tick clears the resubscribed flag in watchdog.py, and SKEW looks to tick once after each resubscribe.",
    );
  });

  it("follows a hanging indent after a bullet", () => {
    const text = "⏺ They also never get marked stale. Any tick clears the \n  resubscribed flag.";
    expect(unwrapText(text, COLS)).toBe("⏺ They also never get marked stale. Any tick clears the resubscribed flag.");
  });

  it("follows a hanging indent after a numbered item", () => {
    const text = "  1. They also never get marked stale. Any tick clears the \n     resubscribed flag.";
    expect(unwrapText(text, COLS)).toBe(
      "  1. They also never get marked stale. Any tick clears the resubscribed flag.",
    );
  });

  it("keeps short lines apart", () => {
    expect(unwrapText("x = 1\ny = 2", COLS)).toBe("x = 1\ny = 2");
  });

  it("keeps a break the next word would have fitted before", () => {
    const text = "They also never get marked stale. Any tick.\nNext sentence.";
    expect(unwrapText(text, COLS)).toBe(text);
  });

  it("keeps a blank row between paragraphs", () => {
    const text = "They also never get marked stale. Any tick clears the \n\nresubscribed flag.";
    expect(unwrapText(text, COLS)).toBe(text);
  });

  it("does not join across a change of indent", () => {
    const text = "They also never get marked stale. Any tick clears the\n    resubscribed flag.";
    expect(unwrapText(text, COLS)).toBe(text);
  });

  it("leaves a list of long paths alone", () => {
    const cols = 100;
    const row = `/usr/local/very/long/path/${"a".repeat(30)}/file`;
    const text = [`${row}1`, `${row}2`, `${row}3`].join("\n");
    expect(unwrapText(text, cols)).toBe(text);
  });

  it("leaves a logical line longer than the pane alone", () => {
    const text = `${"word ".repeat(40).trimEnd()}\nnext`;
    expect(unwrapText(text, COLS)).toBe(text);
  });

  it("glues a single token the application had to break", () => {
    const start = `  https://example.com/${"a".repeat(36)}`;
    expect(unwrapText(`${start}\n  bbbb`, COLS)).toBe(`${start}bbbb`);
  });

  it("returns the text untouched when the pane width is unknown", () => {
    const text = "They also never get marked stale. Any tick clears the \n resubscribed flag.";
    expect(unwrapText(text, null)).toBe(text);
  });
});

describe("softBreak keeps a newline before", () => {
  const full = "They also never get marked stale. Any tick clears the";
  it.each([
    ["a list item", "- item"],
    ["a numbered item", "2. item"],
    ["a quote", "> quoted"],
    ["a prompt", "$ ls"],
    ["a heading", "## Title"],
    ["a tool result", "⎿ done"],
    ["a table rule", "│ cell │"],
    ["a pipe table", "| cell |"],
    ["a timestamp", "2026-11-04 12:00:01 INFO ready"],
    ["a clock time", "12:00:01 ready"],
    ["a log tag", "[info] ready"],
    ["a closing bracket", "} else"],
    ["a code fence", "```go"],
  ])("%s", (_name, next) => {
    expect(softBreak(full, next, COLS)).toBe("none");
  });

  it.each([
    ["an opening brace", `${full} {`],
    ["a semicolon", `${full};`],
    ["a line continuation", `${full} \\`],
    ["a box-drawing rule", "─".repeat(58)],
  ])("a row ending in %s", (_name, row) => {
    expect(softBreak(row, "resubscribed", COLS)).toBe("none");
  });
});
