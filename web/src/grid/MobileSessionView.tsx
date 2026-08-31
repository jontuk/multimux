import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent } from "react";
import type { Server } from "../servers";
import TerminalTile, { type TerminalHandle } from "../term/TerminalTile";
import MobileCompose from "./MobileCompose";
import MobileFontSizeControl from "./MobileFontSizeControl";
import MobileKeyBar from "./MobileKeyBar";
import MobileSessionCreator from "./MobileSessionCreator";
import PaneTextReader from "./PaneTextReader";
import { dirTintStyle } from "./dirColor";
import { readMobileFontSize, writeMobileFontSize, type MobileFontSize as MobileFontSizeValue } from "./mobileFontSize";
import type { MobileSession, MobileSelection } from "./mobileModel";
import { reconcileMobileSelection } from "./mobileModel";
import { gitStateTitles, sessionTitle, TrackingMarks } from "./SessionMetadata";
import type { Session, Tool } from "./types";

type PaneTextTarget = {
  server: Server;
  sessionId: number;
  title: string;
  trigger: HTMLButtonElement;
};

export default function MobileSessionView({
  servers,
  sessions,
  toolsByServer,
  initialLoading,
  onRefresh,
  hostLabel,
  accentColor,
}: {
  servers: Server[];
  sessions: MobileSession[];
  toolsByServer: Record<string, Tool[]>;
  initialLoading: boolean;
  onRefresh: () => void;
  hostLabel?: string;
  accentColor?: string;
}) {
  const [selection, setSelection] = useState<MobileSelection>({ key: null, index: 0 });
  const [controlsSlot, setControlsSlot] = useState<HTMLElement | null>(null);
  const [fontSize, setFontSize] = useState(readMobileFontSize);
  const [creatorOpen, setCreatorOpen] = useState(false);
  const [pendingSelectionKey, setPendingSelectionKey] = useState<string | null>(null);
  const [paneTextTarget, setPaneTextTarget] = useState<PaneTextTarget | null>(null);
  const pointerStart = useRef<{ id: number; x: number; y: number } | null>(null);
  const terminalRef = useRef<TerminalHandle | null>(null);
  const newSessionRef = useRef<HTMLButtonElement>(null);
  const restoreNewSessionFocus = useRef(false);

  useEffect(() => {
    const pendingIndex = pendingSelectionKey ? sessions.findIndex((item) => item.key === pendingSelectionKey) : -1;
    if (pendingIndex >= 0) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSelection({ key: pendingSelectionKey, index: pendingIndex });
      setPendingSelectionKey(null);
      return;
    }
    // Selection must follow asynchronous session-list changes while retaining
    // the current key when it is still present.
    setSelection((current) => reconcileMobileSelection(current, sessions));
  }, [sessions, pendingSelectionKey]);

  useEffect(() => {
    if (!creatorOpen && restoreNewSessionFocus.current) {
      restoreNewSessionFocus.current = false;
      newSessionRef.current?.focus();
    }
  }, [creatorOpen]);

  function closeCreator() {
    restoreNewSessionFocus.current = true;
    setCreatorOpen(false);
  }

  function created(server: Server, started: Session[]) {
    const first = started[0];
    if (first) setPendingSelectionKey(`${server.id}:${first.id}`);
    closeCreator();
    onRefresh();
  }

  function onPointerDown(e: PointerEvent) {
    if (!e.isPrimary || pointerStart.current) return;
    if (e.target instanceof Element && e.target.closest("a, button, input, select, textarea")) return;
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

  const resolvedSelection = reconcileMobileSelection(selection, sessions);
  const selected = sessions[resolvedSelection.index];
  const selectedKey = selected?.key;

  useEffect(() => {
    terminalRef.current?.setFontSize(fontSize);
  }, [fontSize, selectedKey]);

  function changeFontSize(size: MobileFontSizeValue) {
    setFontSize(size);
    writeMobileFontSize(size);
  }

  const selectedTitle = selected
    ? `#${selected.session.id} · ${sessionTitle(toolsByServer[selected.server.id], selected.session)}`
    : "";
  const headerStyle =
    selected || accentColor
      ? ({
          ...(selected ? dirTintStyle(selected.session.dir) : {}),
          ...(accentColor ? { "--host-accent": accentColor } : {}),
        } as CSSProperties)
      : undefined;

  return (
    <div className="mobile-session-view">
      <div className="mobile-session-browser" hidden={creatorOpen}>
        <div
          className={`mobile-session-header${accentColor ? " host-accented" : ""}`}
          style={headerStyle}
          onPointerDown={selected ? onPointerDown : undefined}
          onPointerUp={selected ? onPointerUp : undefined}
          onPointerCancel={selected ? (event) => clearPointer(event, true) : undefined}
          onLostPointerCapture={
            selected
              ? (event) => {
                  if (pointerStart.current?.id === event.pointerId) pointerStart.current = null;
                }
              : undefined
          }
        >
          <div
            className="mobile-session-selector"
            role={selected ? "slider" : undefined}
            tabIndex={selected ? 0 : undefined}
            aria-label={selected ? "Active session" : undefined}
            aria-valuemin={selected ? 1 : undefined}
            aria-valuemax={selected ? sessions.length : undefined}
            aria-valuenow={selected ? resolvedSelection.index + 1 : undefined}
            aria-valuetext={
              selected ? `Session ${resolvedSelection.index + 1} of ${sessions.length}: ${selectedTitle}` : undefined
            }
            onKeyDown={selected ? onKeyDown : undefined}
          >
            {hostLabel && <span className="mobile-host-label">@{hostLabel}</span>}
            {selected && (
              <>
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
              </>
            )}
          </div>
          <div className="mobile-session-actions">
            <button
              ref={newSessionRef}
              className="mobile-new-session"
              type="button"
              aria-label="New session"
              onClick={() => setCreatorOpen(true)}
            >
              +
            </button>
            {selected && (
              <button
                type="button"
                aria-label={`Read text from session ${selected.session.id}`}
                onClick={(event) => {
                  setPaneTextTarget({
                    server: selected.server,
                    sessionId: selected.session.id,
                    title: selectedTitle,
                    trigger: event.currentTarget,
                  });
                }}
              >
                Text
              </button>
            )}
            <span className="mobile-terminal-controls" ref={setControlsSlot} />
            <a className="mobile-settings-link" href="#/settings" aria-label="Settings">
              <span aria-hidden="true">⚙</span>
            </a>
          </div>
        </div>
        {initialLoading ? (
          <div className="mobile-session-empty">Loading sessions…</div>
        ) : !selected ? (
          <div className="mobile-session-empty">
            <span>No sessions are running.</span>
          </div>
        ) : (
          <div className="mobile-terminal">
            <TerminalTile
              ref={terminalRef}
              key={`terminal:${selected.key}`}
              server={selected.server}
              sessionId={selected.session.id}
              onClose={onRefresh}
              sizePolicy="passive"
              controlsSlot={controlsSlot}
              touchScrollback
            />
            <MobileCompose key={`compose:${selected.key}`} terminalRef={terminalRef} controlsSlot={controlsSlot} />
            <MobileFontSizeControl controlsSlot={controlsSlot} value={fontSize} onChange={changeFontSize} />
            <MobileKeyBar terminalRef={terminalRef} />
          </div>
        )}
      </div>
      {creatorOpen && (
        <MobileSessionCreator
          servers={servers}
          initialServerId={selected?.server.id}
          targetServerId={selected?.server.id}
          targetDir={selected?.session.dir ?? null}
          onCancel={closeCreator}
          onLaunched={created}
        />
      )}
      {paneTextTarget && (
        <PaneTextReader
          server={paneTextTarget.server}
          sessionId={paneTextTarget.sessionId}
          title={paneTextTarget.title}
          open
          onClose={() => setPaneTextTarget(null)}
          trigger={paneTextTarget.trigger}
        />
      )}
    </div>
  );
}
