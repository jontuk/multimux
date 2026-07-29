/* eslint-disable react-refresh/only-export-components */
import type { Session, Tool } from "./types";

export const gitStateTitles = {
  untracked: "untracked files present",
  modified: "tracked files modified",
  clean: "working tree clean",
} as const;

// Commits the branch holds that its upstream does not, and vice versa. A
// branch that was never pushed shows "↑?" — the whole history is unpushed, but
// with no upstream there is no count to report.
export function TrackingMarks({ session }: { session: Session }) {
  const { ahead = 0, behind = 0, noUpstream } = session;
  if (noUpstream) {
    return (
      <span className="git-track git-track-unpushed" title="branch has never been pushed">
        ↑?
      </span>
    );
  }
  if (!ahead && !behind) return null;
  return (
    <>
      {ahead > 0 && (
        <span className="git-track git-track-ahead" title={`${ahead} commit(s) not pushed`}>
          ↑{ahead}
        </span>
      )}
      {behind > 0 && (
        <span className="git-track git-track-behind" title={`${behind} commit(s) not pulled`}>
          ↓{behind}
        </span>
      )}
    </>
  );
}

// Display name for a session: the user's label when set, else the tool name,
// falling back to the tmux session name while tools load.
export function sessionTitle(tools: Tool[] | undefined, session: Session | undefined): string {
  if (!session) return "…";
  if (session.label) return session.label;
  return tools?.find((t) => t.id === session.toolId)?.name ?? session.tmuxName;
}
