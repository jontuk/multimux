import { useEffect, useRef, useState, type PointerEvent } from "react";
import TerminalTile from "../term/TerminalTile";
import type { MobileSession, MobileSelection } from "./mobileModel";
import { reconcileMobileSelection } from "./mobileModel";
import { gitStateTitles, sessionTitle, TrackingMarks } from "./SessionMetadata";
import type { Tool } from "./types";

export default function MobileSessionView({
  sessions,
  toolsByServer,
  initialLoading,
  onRefresh,
}: {
  sessions: MobileSession[];
  toolsByServer: Record<string, Tool[]>;
  initialLoading: boolean;
  onRefresh: () => void;
}) {
  const [selection, setSelection] = useState<MobileSelection>({ key: null, index: 0 });
  const pointerStart = useRef<{ id: number; x: number; y: number } | null>(null);

  useEffect(() => {
    // Selection must follow asynchronous session-list changes while retaining
    // the current key when it is still present.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSelection((current) => reconcileMobileSelection(current, sessions));
  }, [sessions]);

  function onPointerDown(e: PointerEvent) {
    if (!e.isPrimary) return;
    pointerStart.current = { id: e.pointerId, x: e.clientX, y: e.clientY };
  }

  function onPointerUp(e: PointerEvent) {
    const start = pointerStart.current;
    pointerStart.current = null;
    if (!start || start.id !== e.pointerId) return;
    const dx = e.clientX - start.x;
    const dy = e.clientY - start.y;
    if (Math.abs(dx) < 48 || Math.abs(dx) <= Math.abs(dy)) return;
    setSelection((current) => {
      const index = Math.max(0, Math.min(sessions.length - 1, current.index + (dx < 0 ? 1 : -1)));
      return { key: sessions[index]?.key ?? null, index };
    });
  }

  if (initialLoading) {
    return <div className="mobile-session-empty">Loading sessions…</div>;
  }
  if (sessions.length === 0) {
    return (
      <div className="mobile-session-empty">
        <span>No sessions are running.</span>
        <span>Launching needs a wider device.</span>
      </div>
    );
  }

  const selected =
    sessions.find((entry) => entry.key === selection.key) ??
    sessions[Math.max(0, Math.min(sessions.length - 1, selection.index))];

  return (
    <div className="mobile-session-view">
      <div
        className="mobile-session-header"
        style={{ touchAction: "pan-y" }}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerCancel={() => {
          pointerStart.current = null;
        }}
      >
        <span className="mobile-session-title">
          #{selected.session.id} · {sessionTitle(toolsByServer[selected.server.id], selected.session)}
        </span>
        <span className="mobile-session-context">
          {selected.session.gitState && (
            <span className="mobile-session-branch">
              <span
                className={`git-dot git-dot-${selected.session.gitState}`}
                title={gitStateTitles[selected.session.gitState]}
              />
              <span className="tile-branch-name">{selected.session.branch}</span>
              <TrackingMarks session={selected.session} />
            </span>
          )}
          <span className="mobile-session-dir" title={selected.session.dir}>
            {selected.session.dir}
          </span>
        </span>
        <span className="mobile-session-position">
          {selection.index + 1}/{sessions.length}
        </span>
      </div>
      <div className="mobile-terminal">
        <TerminalTile key={selected.key} server={selected.server} sessionId={selected.session.id} onClose={onRefresh} />
      </div>
    </div>
  );
}
