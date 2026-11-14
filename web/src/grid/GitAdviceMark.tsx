import { adviceFor, useAdviceDismissed, type GitAdviceTarget } from "./gitAdvice";
import type { Session } from "./types";

type Props = { serverId: string; session: Session; onOpen: (target: GitAdviceTarget) => void };

// The mark a tile shows when git status is slow in its repo and there is a
// setting that would fix it. It only asks the page to open the dialog: the
// page renders it, outside the tile header and its drag and double-click
// handlers.
export default function GitAdviceMark({ serverId, session, onOpen }: Props) {
  const dismissed = useAdviceDismissed(serverId, session.dir);
  if (dismissed || !session.gitAdvice || adviceFor(session.dir, session.gitAdvice.settings).length === 0) return null;
  return (
    <button
      type="button"
      className="git-advice-mark"
      aria-label="git status is slow in this repo: show advice"
      aria-haspopup="dialog"
      title="git status is slow here: click for advice"
      onClick={(event) => onOpen({ serverId, session, trigger: event.currentTarget })}
      onDoubleClick={(event) => event.stopPropagation()}
    >
      <svg viewBox="0 0 16 16" width="12" height="12" fill="currentColor" aria-hidden="true">
        <path d="M9.5 0 2.5 9h4l-1 7 7-9.5h-4z" />
      </svg>
    </button>
  );
}
