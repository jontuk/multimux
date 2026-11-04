import type { IBufferCell, IBufferLine, IBufferRange, Terminal } from "@xterm/xterm";
import { mayJoin, softBreak, type Join } from "./unwrap";

// tmux never sets xterm's isWrapped flag: it repaints every row with absolute
// cursor positioning, so a line that spilled over the right edge arrives as two
// unrelated rows. Everything that has to see the original line again — link
// detection, copying — asks here instead of trusting isWrapped.
//
// The tell is a filled last cell: tmux only writes into the final column when
// the text kept going. A wrap can also land on a space, which leaves that cell
// blank, so a row ending one short of the edge counts too when the next row
// starts hard against the left. A real line that happens to end at either
// column is indistinguishable and joins too; that is the price of the missing
// flag.
// A resize leaves buffer lines whose length still reflects the old width, and
// the cells past the current width hold stale text. Everything here reads
// columns through this clamp so that text never reappears.
export function rowWidth(term: Terminal, line: IBufferLine): number {
  return Math.min(line.length, term.cols);
}

function isFilled(cell: IBufferCell | undefined): boolean {
  if (!cell) return false;
  const chars = cell.getChars();
  // A wide char leaves an empty cell of width 0 beside it, and one that did not
  // fit leaves the last cell that way: both still count as written.
  if (chars === "") return cell.getWidth() === 0;
  return chars !== " ";
}

function rowText(term: Terminal, line: IBufferLine): string {
  return line.translateToString(true, 0, rowWidth(term, line));
}

export function isContinued(term: Terminal, row: number): boolean {
  const buf = term.buffer.active;
  const line = buf.getLine(row);
  const next = buf.getLine(row + 1);
  if (!line || !next) return false;
  if (next.isWrapped) return true; // a genuinely wrapped row needs no guessing
  const nextText = rowText(term, next);
  if (nextText.length === 0) return false;
  const width = rowWidth(term, line);
  if (isFilled(line.getCell(width - 1))) return true;
  return isFilled(line.getCell(width - 2)) && isFilled(next.getCell(0)) && mayJoin(rowText(term, line), nextText);
}

// What joins a row to the next: a terminal wrap keeps the row's cells exactly,
// while an application's wrap replaces its padding and indent.
function rowBreak(term: Terminal, y: number): "wrapped" | Join {
  if (isContinued(term, y)) return "wrapped";
  const buf = term.buffer.active;
  const line = buf.getLine(y);
  const next = buf.getLine(y + 1);
  if (!line || !next) return "none";
  return softBreak(rowText(term, line), rowText(term, next), term.cols);
}

// Rebuilds the selected text the way xterm would, except that rows belonging to
// one line are joined instead of separated by a newline. Breaks are judged from
// whole rows, so a partial selection copies the same way as a full one.
// range.end.x is exclusive, matching Terminal.getSelectionPosition().
export function selectionToText(term: Terminal, range: IBufferRange): string {
  const buf = term.buffer.active;
  let out = "";
  let stripIndent = false;
  for (let y = range.start.y; y <= range.end.y; y++) {
    const line = buf.getLine(y);
    if (!line) continue;
    const from = y === range.start.y ? range.start.x : 0;
    const width = rowWidth(term, line);
    const to = y === range.end.y ? Math.min(range.end.x, width) : width;
    const sep = y < range.end.y ? rowBreak(term, y) : "none";
    // Only a terminal wrap keeps its padding — trimming that row would eat
    // spaces that belong inside the line.
    const text = line.translateToString(sep !== "wrapped", from, to);
    const start = stripIndent ? text.trimStart() : text;
    // An application's wrap often leaves a space it wrote itself, which xterm
    // counts as content and does not trim.
    stripIndent = sep === "space" || sep === "glue";
    out += stripIndent ? start.trimEnd() : start;
    if (y < range.end.y && sep === "none") out += "\n";
    else if (sep === "space") out += " ";
  }
  return out;
}

// What the terminal's current selection should land on the clipboard as.
// Falls back to xterm's own serializer when there is no selection range.
export function selectedText(term: Terminal): string {
  const range = term.getSelectionPosition?.();
  return range ? selectionToText(term, range) : term.getSelection();
}
