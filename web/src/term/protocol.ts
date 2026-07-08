export function encodeResize(cols: number, rows: number): string {
  return JSON.stringify({ type: "resize", cols, rows });
}

export function parseServerText(data: string): { type: string } | null {
  try {
    const v = JSON.parse(data);
    return v && typeof v.type === "string" ? v : null;
  } catch {
    return null;
  }
}
