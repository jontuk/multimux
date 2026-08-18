import type { ILink, ILinkProvider, Terminal } from "@xterm/xterm";
import { isContinued, rowWidth } from "./wrap";

// Verbatim from @xterm/addon-web-links: http(s) up to the first whitespace or
// quote, minus characters that commonly enclose or terminate a url.
const urlRegex = /(https?|HTTPS?):[/]{2}[^\s"'!*(){}|\\^<>`]*[^\s"':,.!?{}|\\^~[\]`()<>]/;

// Bound on how far a single link may run, so a screen full of filled rows can
// never turn into an unbounded scan.
const maxLinkChars = 2048;

type Pos = { x: number; y: number };

function isUrl(text: string): boolean {
  try {
    const url = new URL(text);
    const base =
      url.username && url.password
        ? `${url.protocol}//${url.username}:${url.password}@${url.host}`
        : url.username
          ? `${url.protocol}//${url.username}@${url.host}`
          : `${url.protocol}//${url.host}`;
    return text.toLocaleLowerCase().startsWith(base.toLocaleLowerCase());
  } catch {
    return false;
  }
}

// The rows that row belongs to, as one string plus a buffer position per string
// index. Positions are recorded cell by cell so wide and combining characters
// map back correctly.
function readBlock(term: Terminal, row: number): { text: string; map: Pos[] } {
  const buf = term.buffer.active;
  let top = row;
  let bottom = row;
  while (top > 0 && isContinued(term, top - 1) && (row - top) * term.cols < maxLinkChars) top--;
  while (isContinued(term, bottom) && (bottom - row) * term.cols < maxLinkChars) bottom++;

  let text = "";
  const map: Pos[] = [];
  for (let y = top; y <= bottom; y++) {
    const line = buf.getLine(y);
    if (!line) continue;
    const width = rowWidth(term, line);
    for (let x = 0; x < width; x++) {
      const cell = line.getCell(x);
      if (!cell || cell.getWidth() === 0) continue; // trailing half of a wide char
      const chars = cell.getChars() || " ";
      for (let i = 0; i < chars.length; i++) map.push({ x, y });
      text += chars;
    }
  }
  return { text, map };
}

// y is 1-based, as xterm hands it to a link provider.
export function computeLinks(term: Terminal, y: number, activate: (event: MouseEvent, uri: string) => void): ILink[] {
  const { text, map } = readBlock(term, y - 1);
  const rex = new RegExp(urlRegex.source, (urlRegex.flags || "") + "g");
  const links: ILink[] = [];
  let match: RegExpExecArray | null;
  while ((match = rex.exec(text))) {
    const uri = match[0];
    if (!isUrl(uri)) continue;
    const start = map[match.index];
    const end = map[match.index + uri.length - 1];
    if (!start || !end) continue;
    links.push({
      text: uri,
      range: {
        start: { x: start.x + 1, y: start.y + 1 },
        end: { x: end.x + 1, y: end.y + 1 },
      },
      activate,
    });
  }
  return links;
}

function openLink(_event: MouseEvent, uri: string): void {
  const win = window.open();
  if (!win) {
    console.warn("Opening link blocked as opener could not be cleared");
    return;
  }
  try {
    win.opener = null;
  } catch {
    // no-op
  }
  win.location.href = uri;
}

export function wrapAwareLinkProvider(term: Terminal): ILinkProvider {
  return {
    provideLinks(y, callback) {
      callback(computeLinks(term, y, openLink));
    },
  };
}
