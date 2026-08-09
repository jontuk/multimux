// Sessions sharing a working directory are "linked" — the grid shows that by
// washing their tile headers with a hue derived from the directory itself.
// Hashing the path keeps the mapping stateless: the same directory tints the
// same colour in every tab, on every host, with nothing persisted anywhere.

// FNV-1a, 32-bit. Math.imul keeps the multiply in 32-bit integer space.
function hash(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

// The full path, not the leaf: ~/Repos/multimux and ~/old/multimux are
// different directories, so they get different colours.
export function dirHue(path: string): number {
  return hash(path) % 360;
}

// Saturation and lightness are fixed so no hue reads as heavier than another —
// only the hue distinguishes directories. CSS decides how far to mix this into
// the header background (see --dir-tint in index.css).
export function dirTint(path: string): string {
  return `hsl(${dirHue(path)} 60% 55%)`;
}

// Inline style for a header element. Typed loosely because CSS custom
// properties aren't part of React's CSSProperties.
export function dirTintStyle(path: string): React.CSSProperties {
  return { "--dir-tint": dirTint(path) } as React.CSSProperties;
}
