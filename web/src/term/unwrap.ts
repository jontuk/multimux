// Applications that draw their own UI (Claude Code, for one) word-wrap text
// themselves and move to the next row explicitly. To the terminal and to tmux
// those are ordinary line ends, so no wrap flag exists and only the text's
// shape can suggest that two rows were one line. Everything here is a guess
// that prefers keeping a newline: a leftover break is visible and harmless,
// while a wrongly joined line of code is neither.

export type Join = "none" | "space" | "glue";

// Applications rarely wrap at the very last column, so a row counts as full
// slightly before it.
const RIGHT_MARGIN = 6;
// Below this width almost any pair of rows looks full.
const MIN_COLS = 20;

const MARKER = /^(?:[-*+•◦▪‣⏺●○⎿✓✔✗✘☐☑☒>❯$#]+|\d{1,3}[.)])\s+/u;
const BLOCK_START = /^(?:[|}\])[]|```|\d{4}-\d{2}-\d{2}|\d{2}:\d{2}:\d{2})/;
const BOX_DRAWING = /[─-▟]/;
const CODE_END = /[{(;\\]$/;

// False when the shape of either row says a newline between them is real,
// however wide they are: tables, rules, list items, logs and code.
export function mayJoin(row: string, next: string): boolean {
  const body = next.trimStart();
  if (BOX_DRAWING.test(row) || BOX_DRAWING.test(next)) return false;
  if (MARKER.test(body) || BLOCK_START.test(body)) return false;
  return !CODE_END.test(row.trimEnd());
}

// Decides what separates a row from the next one. The test is whether the
// next row's first word could have fitted on this row: if it could, nothing
// forced the break, so the break was meant. A very long word is capped so a
// short row before a long path or URL still counts as a deliberate end.
export function softBreak(row: string, next: string, cols: number): Join {
  const text = row.trimEnd();
  const body = next.trim();
  if (cols < MIN_COLS || !text || !body || text.length > cols) return "none";
  if (!mayJoin(text, next)) return "none";

  const indent = text.length - text.trimStart().length;
  const marker = MARKER.exec(text.slice(indent))?.[0].length ?? 0;
  const hanging = indent + marker;
  if (next.length - next.trimStart().length !== hanging) return "none";

  const full = cols - RIGHT_MARGIN;
  const word = body.split(/\s/, 1)[0].length;
  if (text.length + 1 + Math.min(word, Math.floor(cols / 4)) <= full) return "none";

  // A single token filling the row had to be cut wherever the row ended, and
  // the two halves belong together.
  return text.length >= full && !/\s/.test(text.slice(hanging)) ? "glue" : "space";
}

// Rows that stay separate keep their exact text. cols null means the width is
// unknown, which leaves nothing to judge a row against.
export function unwrapText(text: string, cols: number | null): string {
  if (cols === null) return text;
  const rows = text.split("\n");
  let out = rows[0];
  for (let i = 1; i < rows.length; i++) {
    const join = softBreak(rows[i - 1], rows[i], cols);
    if (join === "none") out += `\n${rows[i]}`;
    else out = out.trimEnd() + (join === "space" ? " " : "") + rows[i].trimStart();
  }
  return out;
}
