import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
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
    if (!e.isPrimary || pointerStart.current) return;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    pointerStart.current = { id: e.pointerId, x: e.clientX, y: e.clientY };
  }

  function clearPointer(e: PointerEvent, releaseCapture: boolean) {
    const start = pointerStart.current;
    if (!start || start.id !== e.pointerId) return null;
    pointerStart.current = null;
    if (releaseCapture && e.currentTarget.hasPointerCapture?.(start.id)) {
      e.currentTarget.releasePointerCapture?.(start.id);
    }
    return start;
  }

  function moveSelection(offset: number) {
    setSelection((current) => {
      const resolved = reconcileMobileSelection(current, sessions);
      const index = Math.max(0, Math.min(sessions.length - 1, resolved.index + offset));
      return { key: sessions[index]?.key ?? null, index };
    });
  }

  function onPointerUp(e: PointerEvent) {
    const start = clearPointer(e, true);
    if (!start) return;
    const dx = e.clientX - start.x;
    const dy = e.clientY - start.y;
    if (Math.abs(dx) < 48 || Math.abs(dx) <= Math.abs(dy)) return;
    moveSelection(dx < 0 ? 1 : -1);
  }

  function onKeyDown(e: KeyboardEvent) {
    const offsets: Record<string, number> = {
      ArrowDown: -1,
      ArrowLeft: -1,
      ArrowRight: 1,
      ArrowUp: 1,
      End: sessions.length,
      Home: -sessions.length,
    };
    const offset = offsets[e.key];
    if (offset === undefined) return;
    e.preventDefault();
    moveSelection(offset);
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

  const resolvedSelection = reconcileMobileSelection(selection, sessions);
  const selected = sessions[resolvedSelection.index];
  const selectedTitle = `#${selected.session.id} · ${sessionTitle(
    toolsByServer[selected.server.id],
    selected.session,
  )}`;

  return (
    <div className="mobile-session-view">
      <div
        className="mobile-session-header"
        role="slider"
        tabIndex={0}
        aria-label="Active session"
        aria-valuemin={1}
        aria-valuemax={sessions.length}
        aria-valuenow={resolvedSelection.index + 1}
        aria-valuetext={`Session ${resolvedSelection.index + 1} of ${sessions.length}: ${selectedTitle}`}
        style={{ touchAction: "pan-y" }}
        onKeyDown={onKeyDown}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerCancel={(e) => clearPointer(e, true)}
        onLostPointerCapture={(e) => {
          if (pointerStart.current?.id === e.pointerId) pointerStart.current = null;
        }}
      >
        <span className="mobile-session-title">{selectedTitle}</span>
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
          {resolvedSelection.index + 1}/{sessions.length}
        </span>
      </div>
      <div className="mobile-terminal">
        <TerminalTile key={selected.key} server={selected.server} sessionId={selected.session.id} onClose={onRefresh} />
      </div>
    </div>
  );
}
