export type Session = {
  id: number;
  tmuxName: string;
  toolId: number;
  dir: string;
  status: string;
  /** User-chosen display name; absent or empty means "use the tool name". */
  label?: string;
  repoUrl?: string;
  branch?: string;
  gitState?: "untracked" | "modified" | "clean";
  ahead?: number;
  behind?: number;
  noUpstream?: boolean;
  gitAdvice?: GitAdvice;
};
/**
 * Sent when git status is costly in the session's dir and the repo is not
 * using the git settings that would make it cheap. `costMs` is what one status
 * costs there, `everyMs` how long the daemon now waits between checks, and
 * `settings` the git config keys worth setting to true.
 */
export type GitAdvice = { costMs: number; everyMs: number; settings: string[] };
export type Tool = { id: number; name: string; command: string };
export type Dir = { id: number; name: string; path: string };
