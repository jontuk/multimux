import type { IBufferLine, IBufferRange, Terminal } from "@xterm/xterm";

// tmux never sets xterm's isWrapped flag: it repaints every row with absolute
// cursor positioning, so a line that spilled over the right edge arrives as two
// unrelated rows. Everything that has to see the original line again — link
// detection, copying — asks here instead of trusting isWrapped.
//
// The tell is a filled last cell: tmux only writes into the final column when
// the text kept going. A real line that happens to end exactly at the edge is
// indistinguishable and joins too; that is the price of the missing flag.
// A resize leaves buffer lines whose length still reflects the old width, and
// the cells past the current width hold stale text. Everything here reads
// columns through this clamp so that text never reappears.
export function rowWidth(term: Terminal, line: IBufferLine): number {
  return Math.min(line.length, term.cols);
}

export function isContinued(term: Terminal, row: number): boolean {
  const buf = term.buffer.active;
  const line = buf.getLine(row);
  const next = buf.getLine(row + 1);
  if (!line || !next) return false;
  if (next.isWrapped) return true; // a genuinely wrapped row needs no guessing
  if (next.translateToString(true, 0, rowWidth(term, next)).length === 0) return false;
  const cell = line.getCell(rowWidth(term, line) - 1);
  if (!cell) return false;
  const chars = cell.getChars();
  // A wide char that did not fit leaves the last cell empty with width 0, and
  // that is a continuation too.
  if (chars === "") return cell.getWidth() === 0;
  return chars !== " ";
}

// Rebuilds the selected text the way xterm would, except that continuation rows
// are glued on instead of separated by a newline. range.end.x is exclusive,
// matching Terminal.getSelectionPosition().
export function selectionToText(term: Terminal, range: IBufferRange): string {
  const buf = term.buffer.active;
  let out = "";
  for (let y = range.start.y; y <= range.end.y; y++) {
    const line = buf.getLine(y);
    if (!line) continue;
    const from = y === range.start.y ? range.start.x : 0;
    const width = rowWidth(term, line);
    const to = y === range.end.y ? Math.min(range.end.x, width) : width;
    const joined = y < range.end.y && isContinued(term, y);
    // Only a row that really ends the line loses its padding — trimming a
    // continuation row would eat spaces that belong inside the line.
    out += line.translateToString(!joined, from, to);
    if (y < range.end.y && !joined) out += "\n";
  }
  return out;
}

// What the terminal's current selection should land on the clipboard as.
// Falls back to xterm's own serializer when there is no selection range.
export function selectedText(term: Terminal): string {
  const range = term.getSelectionPosition?.();
  return range ? selectionToText(term, range) : term.getSelection();
}
