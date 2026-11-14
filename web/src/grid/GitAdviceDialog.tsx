import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { trapTab } from "./focusTrap";
import { adviceFor, dismissAdvice, formatDuration, primeCommand, type GitAdviceTarget } from "./gitAdvice";

type Props = { target: GitAdviceTarget; onClose: () => void };

export default function GitAdviceDialog({ target, onClose }: Props) {
  const { serverId, session, trigger } = target;
  const [copyStatus, setCopyStatus] = useState("");
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    closeRef.current?.focus();
    return () => trigger?.focus();
  }, [trigger]);

  const advice = session.gitAdvice;
  if (!advice) return null;

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    event.stopPropagation();
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
    } else if (event.key === "Tab") {
      trapTab(event, dialogRef.current);
    }
  }

  async function copy(command: string) {
    try {
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(command);
      setCopyStatus("Copied.");
    } catch {
      setCopyStatus("Clipboard unavailable. Select the command and copy it manually.");
    }
  }

  return (
    <div
      className="git-advice-backdrop"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        className="git-advice-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onKeyDown={onKeyDown}
      >
        <h1 id={titleId}>git status is slow in this repo</h1>
        <p>
          Each <code>git status</code> in <code>{session.dir}</code> costs about {formatDuration(advice.costMs)}, so
          multimux now checks it only every {formatDuration(advice.everyMs)} or so. Until the next check, this
          session&apos;s branch and change marks can be out of date.
        </p>
        <p>These git settings make it fast again. Run each line on the machine the session runs on:</p>
        <ul className="git-advice-commands">
          {adviceFor(session.dir, advice.settings).map(({ setting, command, note }) => (
            <li key={setting}>
              <div className="git-advice-command">
                <code>{command}</code>
                <button type="button" aria-label={`Copy ${setting} command`} onClick={() => void copy(command)}>
                  Copy
                </button>
              </div>
              <p className="git-advice-note">{note}</p>
            </li>
          ))}
          <li>
            <div className="git-advice-command">
              <code>{primeCommand(session.dir)}</code>
              <button
                type="button"
                aria-label="Copy git status command"
                onClick={() => void copy(primeCommand(session.dir))}
              >
                Copy
              </button>
            </div>
            <p className="git-advice-note">
              Then run this once. It lets git save what the settings teach it; multimux&apos;s own checks never write to
              your repo, so they cannot.
            </p>
          </li>
        </ul>
        <div className="git-advice-feedback" aria-live="polite">
          {copyStatus}
        </div>
        <div className="git-advice-actions">
          <button
            type="button"
            onClick={() => {
              dismissAdvice(serverId, session.dir);
              onClose();
            }}
          >
            Don&apos;t show for this repo
          </button>
          <button ref={closeRef} type="button" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
