export function parseOpenerOrigin(search: string): string | null {
  const raw = new URLSearchParams(search).get("opener");
  if (!raw) return null;
  try {
    const u = new URL(raw);
    const isHttps = u.protocol === "https:";
    const isDevLocalhost = u.protocol === "http:" && (u.hostname === "localhost" || u.hostname === "127.0.0.1");
    if (!isHttps && !isDevLocalhost) return null;
    return u.origin;
  } catch {
    return null;
  }
}
