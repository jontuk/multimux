import type { Terminal } from "@xterm/xterm";

// Minimal stand-in for the parts of xterm's buffer API that wrap.ts and
// links.ts read: fixed-width rows of single-width cells, blank-padded. As in
// xterm, a space the application wrote is content: trimming only drops the
// unwritten cells past it, and a written space reads differently from an empty
// cell.
class FakeLine {
  isWrapped = false;
  private readonly text: string;
  readonly length: number;
  constructor(text: string, length: number) {
    this.text = text;
    this.length = length;
  }
  translateToString(trimRight = false, start = 0, end = this.length): string {
    const stop = trimRight ? Math.min(end, this.text.length) : end;
    return this.text.padEnd(this.length, " ").slice(start, Math.max(start, stop));
  }
  getCell(x: number) {
    if (x < 0 || x >= this.length) return undefined;
    const ch = this.text[x] ?? "";
    return { getChars: () => ch, getWidth: () => 1 };
  }
}

// lineWidth models a post-resize buffer: rows whose length still reflects the
// old, wider terminal.
export function fakeTerminal(cols: number, rows: string[], lineWidth = cols): Terminal {
  const lines = rows.map((r) => new FakeLine(r, lineWidth));
  return {
    cols,
    buffer: { active: { getLine: (y: number) => lines[y] } },
  } as unknown as Terminal;
}
