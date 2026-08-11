const KEY = "multimux-client-id";

// A stable id for this browser profile, sent on PTY WebSockets. The daemon keys
// shared-tmux-window ownership on it (see internal/tmuxmgr.Arbiter) so a tile
// that reconnects — network blip, remount, phone waking — comes back as the
// same owner instead of letting another machine's passive resize grab the
// window. It identifies nothing about the user and carries no authority.
let cached: string | null = null;

function generate(): string {
  if (typeof crypto?.randomUUID === "function") return crypto.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

export function clientId(): string {
  if (cached) return cached;
  try {
    const stored = localStorage.getItem(KEY);
    if (stored) {
      cached = stored;
      return stored;
    }
  } catch {
    // Storage disabled (private mode, blocked cookies) — fall through to a
    // per-page id: ownership then behaves as it did before, never worse.
  }
  cached = generate();
  try {
    localStorage.setItem(KEY, cached);
  } catch {
    // ignore — the in-memory id still holds for this page's lifetime
  }
  return cached;
}
