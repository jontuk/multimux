// The dir filter is a view, not state the daemon knows about: hiding a
// directory changes what this browser renders and nothing else. The stored
// layout keeps every tile, so another tab (or this one after a clear) still
// sees the full grid. Mirrors how servers.ts keeps its list browser-local.

import type { Server } from "../servers";
import type { Session } from "./types";

const KEY = "multimux.hiddenDirs";

/** Full paths whose sessions this browser is hiding. Absent means visible. */
export function hiddenDirs(): Set<string> {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    if (!Array.isArray(raw)) return new Set();
    return new Set(raw.filter((v): v is string => typeof v === "string"));
  } catch {
    return new Set();
  }
}

export function setHiddenDirs(hidden: Set<string>) {
  // Sorted so the stored value is stable regardless of toggle order.
  localStorage.setItem(KEY, JSON.stringify([...hidden].sort()));
}

// Last path segment, trailing slashes ignored. Root (and anything else that
// leaves nothing behind) falls back to the raw path so a button always has a
// label.
export function leafName(path: string): string {
  const parts = path.replace(/\/+$/, "").split("/");
  return parts[parts.length - 1] || path;
}

export type DirButton = { path: string; name: string; count: number };

// One entry per distinct directory of a running session, on any server. Dead
// sessions get no button: they cannot be launched into and hiding them is not
// what the user is reaching for.
export function dirButtons(servers: Server[], sessionsByServer: Record<string, Session[]>): DirButton[] {
  const counts = new Map<string, number>();
  for (const server of servers) {
    for (const sess of sessionsByServer[server.id] ?? []) {
      if (sess.status === "running") counts.set(sess.dir, (counts.get(sess.dir) ?? 0) + 1);
    }
  }
  return [...counts]
    .map(([path, count]) => ({ path, name: leafName(path), count }))
    // Leaf name is what the user reads; the full path breaks ties so two
    // repos with the same leaf keep a stable order.
    .sort((a, b) => a.name.localeCompare(b.name) || a.path.localeCompare(b.path));
}
