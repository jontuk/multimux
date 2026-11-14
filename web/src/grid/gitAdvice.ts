// Advice for a repo whose `git status` is slow. The daemon decides when it
// applies (see gitAdvice in internal/server/sessions.go); this module turns it
// into commands to paste, and remembers which repos this browser has waved it
// away for. Like the solo in dirFilter.ts, a dismissal is a view of this
// browser's, not state the daemon knows about.

import { useSyncExternalStore } from "react";
import type { Session } from "./types";

/** What the page needs to open the advice dialog for one tile. */
export type GitAdviceTarget = { serverId: string; session: Session; trigger: HTMLElement | null };

// The settings this page can explain. The daemon's list is filtered through
// it: the text ends up in a shell, so nothing a daemon sends is pasted on
// trust, and a setting a newer daemon knows but this page does not is dropped.
const SETTING_NOTES: Record<string, string> = {
  "core.fsmonitor":
    "git keeps a small background watcher on this repo and asks it what changed, instead of checking every file.",
  "core.untrackedCache": "git remembers which folders have no new files and skips them.",
};

export type AdviceCommand = { setting: string; command: string; note: string };

/** The advised settings this page knows, each with the command that turns it on. */
export function adviceFor(dir: string, settings: string[]): AdviceCommand[] {
  return settings
    .filter((setting) => Object.hasOwn(SETTING_NOTES, setting))
    .map((setting) => ({
      setting,
      command: `git -C ${shellQuote(dir)} config ${setting} true`,
      note: SETTING_NOTES[setting],
    }));
}

/** Just the commands. `-C` names the dir so they work from any shell on that machine. */
export function adviceCommands(dir: string, settings: string[]): string[] {
  return adviceFor(dir, settings).map(({ command }) => command);
}

/**
 * The status run that makes the settings take effect. git keeps what they let
 * it learn in the repo's index, and the daemon's own checks are forbidden to
 * write there (they run with --no-optional-locks so they can never collide
 * with a commit), so the first write has to come from the user. Measured on a
 * 250k-file repo: with both settings on, the daemon's status stayed at
 * seconds of CPU until one plain `git status` ran, then dropped to 30ms.
 */
export function primeCommand(dir: string): string {
  return `git -C ${shellQuote(dir)} status`;
}

// Characters every POSIX shell leaves alone. Anything else gets single
// quotes, inside which only the quote itself needs escaping.
const SHELL_SAFE = /^[A-Za-z0-9_@%+=:,./-]+$/;

function shellQuote(arg: string): string {
  return SHELL_SAFE.test(arg) ? arg : `'${arg.replace(/'/g, "'\\''")}'`;
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
  return `${Math.round(ms / 60_000)} min`;
}

const KEY = "multimux.gitAdviceDismissed";

// The same path on two machines is two repos, so the server is part of the key.
const dismissKey = (serverId: string, dir: string) => `${serverId}\n${dir}`;

const listeners = new Set<() => void>();

function subscribe(onChange: () => void) {
  listeners.add(onChange);
  // Another tab's dismissal arrives as a storage event.
  window.addEventListener("storage", onChange);
  return () => {
    listeners.delete(onChange);
    window.removeEventListener("storage", onChange);
  };
}

function stored(): string {
  try {
    return localStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

function parse(raw: string): string[] {
  try {
    const value: unknown = JSON.parse(raw || "[]");
    return Array.isArray(value) ? value.filter((key): key is string => typeof key === "string") : [];
  } catch {
    return [];
  }
}

/** Whether this browser has dismissed the advice for one repo. Live: every mark for the repo goes at once. */
export function useAdviceDismissed(serverId: string, dir: string): boolean {
  const raw = useSyncExternalStore(subscribe, stored);
  return parse(raw).includes(dismissKey(serverId, dir));
}

export function dismissAdvice(serverId: string, dir: string) {
  const key = dismissKey(serverId, dir);
  const dismissed = parse(stored());
  if (!dismissed.includes(key)) localStorage.setItem(KEY, JSON.stringify([...dismissed, key]));
  listeners.forEach((notify) => notify());
}
