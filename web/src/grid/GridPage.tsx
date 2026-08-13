import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { del, getJSON, putJSON } from "../api";
import { connectServer, listServers, localServer, removeServer, type Server } from "../servers";
import {
  addTile,
  emptyLayout,
  normalize,
  removeTile,
  setCols,
  swapTiles,
  tileKey,
  type Layout,
  type Tile,
} from "./model";
import { tileRect } from "./sizes";
import ColumnStepper from "./ColumnStepper";
import GridDividers from "./GridDividers";
import HeaderLauncher from "./HeaderLauncher";
import TerminalTile from "../term/TerminalTile";
import { useEvents, type EventsStatus } from "../useEvents";
import { MOBILE_VIEW_QUERY, useMediaQuery } from "../useMediaQuery";
import MobileSessionView from "./MobileSessionView";
import { orderMobileSessions } from "./mobileModel";
import type { Session, Tool } from "./types";
import { gitStateTitles, sessionTitle, TrackingMarks } from "./SessionMetadata";
import { dirTintStyle } from "./dirColor";
import DirFilterBar from "./DirFilterBar";
import { cycleSolo, dirButtons, effectiveSolo, filterLayout, setSoloDir, soloDir } from "./dirFilter";
import { applyOverlay, orderOf, seedOverlay, setViewOverlay, swapOrder, viewOverlay, type Overlay } from "./viewLayout";

function isLayout(v: unknown): v is Layout {
  return !!v && typeof v === "object" && "shape" in v && "tiles" in v;
}

// Hooks can't be called in a loop, so each distinct server gets its own bridge component.
function EventsBridge({
  server,
  onEvent,
  onStatus,
}: {
  server: Server;
  onEvent: (type: string) => void;
  onStatus: (s: EventsStatus) => void;
}) {
  useEvents(server, onEvent, onStatus);
  return null;
}

const statusMessages: Record<Exclude<EventsStatus, "open">, string> = {
  "auth-expired": "not logged in — your session is stale (daemon restarted or data dir changed). Reload to log in.",
  forbidden: "daemon refused access — setup may still be pending on that machine.",
  "ws-blocked": "API reachable but WebSockets are blocked — the daemon does not allow this page's origin.",
  unreachable: "daemon unreachable — retrying.",
};

// GitHub octicon "mark-github" (MIT-licensed by GitHub).
function GitHubIcon() {
  return (
    <svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true">
      <path d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z" />
    </svg>
  );
}

// The tile title, double-click-to-rename. The tile header's own double-click
// maximizes the tile, so the handlers here stop propagation; the input also
// tells the page to drop `draggable` on the tile, or the browser's drag
// intercepts text selection inside it.
function TileTitle({
  sessionId,
  text,
  label,
  onEditingChange,
  onRename,
}: {
  sessionId: number;
  text: string;
  label: string;
  onEditingChange: (editing: boolean) => void;
  onRename: (label: string) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);

  const stop = (draftValue: string | null) => {
    setDraft(null);
    onEditingChange(false);
    if (draftValue !== null && draftValue.trim() !== label) onRename(draftValue.trim());
  };

  if (draft === null) {
    return (
      <span
        className="tile-title"
        title="double-click to rename"
        onDoubleClick={(e) => {
          e.stopPropagation();
          setDraft(label);
          onEditingChange(true);
        }}
      >
        #{sessionId} · {text}
      </span>
    );
  }
  return (
    <input
      className="tile-title tile-title-input"
      aria-label={`rename session ${sessionId}`}
      autoFocus
      // 64 UTF-16 code units, conservative against the server's 64-*rune* cap
      // (astral-plane characters, e.g. emoji, are 2 code units here but 1
      // rune server-side) — the UI can only be stricter, never send a label
      // the server would reject for length.
      maxLength={64}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onDoubleClick={(e) => e.stopPropagation()}
      onFocus={(e) => e.currentTarget.select()}
      onBlur={() => stop(draft)}
      onKeyDown={(e) => {
        if (e.key === "Enter") stop(draft);
        else if (e.key === "Escape") stop(null);
      }}
    />
  );
}

export default function GridPage({
  headerSlot = null,
  confirmTerminate = false,
}: {
  headerSlot?: HTMLElement | null;
  // Off by default: terminating is one click unless the user opts in.
  confirmTerminate?: boolean;
}) {
  const [layout, setLayout] = useState<Layout>(emptyLayout());
  const [sessionsByServer, setSessionsByServer] = useState<Record<string, Session[]>>({});
  const [toolsByServer, setToolsByServer] = useState<Record<string, Tool[]>>({});
  const [statusByServer, setStatusByServer] = useState<Record<string, EventsStatus>>({});
  const [layoutSettled, setLayoutSettled] = useState(false);
  const [settledSessionServers, setSettledSessionServers] = useState<Set<string>>(() => new Set());
  // Ephemeral: which tile fills the viewport (tile key), or null for grid view.
  const [maximizedKey, setMaximizedKey] = useState<string | null>(null);
  // Ephemeral: a just-launched tile whose terminal should grab keyboard focus
  // so the user can type immediately. Only the freshly-mounted tile reads it.
  const [focusKey, setFocusKey] = useState<string | null>(null);
  // Tile key of the session the user is working in — last tile to take focus,
  // and it stays after focus leaves the grid entirely (clicking the launcher
  // must not un-answer "which session am I in?"). Feeds the launcher, so
  // "+ New" opens where the user already is.
  const [activeKey, setActiveKey] = useState<string | null>(null);
  // Tile key whose title is being renamed; the tile drops `draggable` while it
  // is, so the drag doesn't eat text selection in the input.
  const [editingKey, setEditingKey] = useState<string | null>(null);
  // State (not a per-render listServers() call) so the array identity is
  // stable across re-renders and the events sockets don't churn; refreshed
  // explicitly after reconnect/remove changes the stored list.
  const [servers, setServers] = useState(() => listServers());

  // Browser-local view filter: the one directory shown on its own, or null for
  // all of them. Not persisted server-side and never written into the layout.
  const [solo, setSolo] = useState<string | null>(() => soloDir());

  // The soloed directory's own arrangement — columns, splitter sizes and tile
  // order. Mirrors localStorage so a re-render does not re-read it; null means
  // no solo is in effect, or the soloed directory has not been arranged yet
  // and renders exactly as the stored layout does.
  const [overlay, setOverlay] = useState<Overlay | null>(null);

  // A click solos that directory; a click on the soloed one shows every
  // directory again.
  const toggleDir = useCallback((path: string) => {
    setSolo((prev) => (prev === path ? null : path));
  }, []);

  // Show a directory that is not the solo, by clearing the solo rather than
  // moving it: attaching or launching from an empty tile asks for that tile to
  // be visible, not for everything else on screen to silently change. Written
  // as a state updater — the only place allowed to decide — so a stale read of
  // `solo` in a handler's closure cannot re-solo something.
  const showDir = useCallback((path: string) => {
    setSolo((prev) => (prev === null || prev === path ? prev : null));
  }, []);

  // The updaters above stay pure — StrictMode runs them twice — so the side
  // effect of a changed solo lives here: persist the selection. The overlay is
  // loaded from `activeSolo` below, not here, because a stored solo whose
  // directory has no button is not in effect and must not load an arrangement.
  useEffect(() => {
    setSoloDir(solo);
  }, [solo]);

  // Mirrors `layout` so edits always build on the newest state, not the state
  // captured when a handler's closure was created.
  const layoutRef = useRef(layout);
  // The `.grid` container: divider drags read its box to convert pixel deltas
  // into fractions.
  const gridRef = useRef<HTMLDivElement>(null);

  // A maximized tile that leaves the layout (removed, terminated, server-side
  // layout change) must not leave the page stuck fullscreen — or re-maximize
  // if the same session is later re-added.
  const adoptLayout = useCallback((l: Layout) => {
    layoutRef.current = l;
    setLayout(l);
    setMaximizedKey((k) => (k && !l.tiles.some((t) => t && tileKey(t) === k) ? null : k));
  }, []);

  // At most one layout PUT in flight; edits made during a write coalesce into
  // `pendingWrite` so the newest layout is always the last one persisted.
  const pendingWrite = useRef<Layout | null>(null);
  const writeInFlight = useRef(false);
  const flushLayout = useCallback(function flush() {
    if (writeInFlight.current || pendingWrite.current === null) return;
    const l = pendingWrite.current;
    pendingWrite.current = null;
    writeInFlight.current = true;
    putJSON(localServer(), "/api/layout", l)
      .catch(() => {})
      .then(() => {
        writeInFlight.current = false;
        flush();
      });
  }, []);

  const persist = useCallback(
    (update: (prev: Layout) => Layout) => {
      const next = update(layoutRef.current);
      adoptLayout(next);
      pendingWrite.current = next;
      flushLayout();
    },
    [adoptLayout, flushLayout],
  );

  const refreshSessions = useCallback(
    (settleInitial = false) => {
      for (const server of servers) {
        getJSON<Session[]>(server, "/api/sessions")
          .then((s) => setSessionsByServer((prev) => ({ ...prev, [server.id]: s })))
          .catch(() => setSessionsByServer((prev) => ({ ...prev, [server.id]: [] })))
          .finally(() => {
            if (settleInitial) {
              setSettledSessionServers((prev) => {
                const next = new Set(prev);
                next.add(server.id);
                return next;
              });
            }
          });
      }
    },
    [servers],
  );

  const renameSession = useCallback(
    (server: Server, sessionId: number, label: string) => {
      // The response and the session_renamed broadcast both land as a refresh;
      // a failure just leaves the old title in place.
      putJSON(server, `/api/sessions/${sessionId}/label`, { label }).then(
        () => refreshSessions(),
        () => refreshSessions(),
      );
    },
    [refreshSessions],
  );

  const refreshLayout = useCallback(
    (settleInitial = false) => {
      getJSON<unknown>(localServer(), "/api/layout")
        .then((v) => {
          // Normalize so layouts persisted before rows were derived still load cleanly.
          if (isLayout(v)) adoptLayout(normalize(v.tiles, v.shape.cols, v.rowSizes, v.colSizes));
        })
        .catch(() => {})
        .finally(() => {
          if (settleInitial) setLayoutSettled(true);
        });
    },
    [adoptLayout],
  );

  const onServerEvent = useCallback(
    (type: string) => {
      // "hello" arrives on every (re)connect; the hub drops events for slow
      // subscribers, so a reconnected socket must resync everything.
      if (type.startsWith("session_") || type === "git_changed" || type === "hello") refreshSessions();
      if (type === "layout_changed" || type === "hello") refreshLayout();
    },
    [refreshSessions, refreshLayout],
  );

  useEffect(() => {
    refreshLayout(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Refetch per-server data whenever the server list changes (mount, and after
  // a reconnect replaces a token or a dead server is removed).
  useEffect(() => {
    refreshSessions(true);
    for (const server of servers) {
      getJSON<Tool[]>(server, "/api/tools")
        .then((t) => setToolsByServer((prev) => ({ ...prev, [server.id]: t })))
        .catch(() => {});
    }
  }, [servers, refreshSessions]);

  // Listener only exists while maximized; Escape also reaches the focused
  // terminal (same trade-off as cheep).
  useEffect(() => {
    if (!maximizedKey) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMaximizedKey(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [maximizedKey]);

  // Sessions already placed in a tile; each session may only be open once.
  const placed = useMemo(
    () => new Set(layout.tiles.filter((t): t is NonNullable<Tile> => t !== null).map(tileKey)),
    [layout],
  );
  function attachSession(server: Server, sessionId: number) {
    if (placed.has(`${server.id}:${sessionId}`)) return;
    // Attaching a session outside the solo — from the empty-tile dropdown or
    // a quick-add button — would otherwise land the tile and immediately
    // filter it back out, with no sign anything happened.
    const session = (sessionsByServer[server.id] ?? []).find((s) => s.id === sessionId);
    if (session) showDir(session.dir);
    persist((l) => addTile(l, { serverId: server.id, sessionId }));
  }

  function placeSession(server: Server, session: Session) {
    // Launching outside the solo would look like nothing happened.
    showDir(session.dir);
    persist((l) => addTile(l, { serverId: server.id, sessionId: session.id }));
    setFocusKey(`${server.id}:${session.id}`);
    // The terminal's autoFocus raises this too, but only once it has mounted;
    // setting it here means the launcher is already aimed at the new session.
    setActiveKey(`${server.id}:${session.id}`);
    refreshSessions();
  }

  const sessionFor = useCallback(
    (tile: NonNullable<Tile>) => (sessionsByServer[tile.serverId] ?? []).find((s) => s.id === tile.sessionId),
    [sessionsByServer],
  );

  const dirs = useMemo(() => dirButtons(servers, sessionsByServer), [servers, sessionsByServer]);
  // A stored solo whose directory has no button is not in effect this render,
  // so the grid is unfiltered rather than filtered by something the user
  // cannot see. The stored value stays put and comes back when its button
  // does.
  const activeSolo = effectiveSolo(solo, dirs);

  // Load the arrangement for whichever directory is soloed. A directory with
  // no stored overlay reads null and renders as the stored layout does.
  useEffect(() => {
    setOverlay(activeSolo === null ? null : viewOverlay(activeSolo));
  }, [activeSolo]);

  // Where "+ New" should aim. A solo wins: it is the standing statement of
  // which directory the user is working in, and it is server-agnostic — the
  // filter bar counts a directory across every daemon — so any server whose
  // dirs contain it may answer. Without one, the focused tile's session
  // answers, which is what "follow me around the grid" means in all mode; that
  // one is pinned to its own server, since an identical path on another daemon
  // is a different machine's directory.
  const activeTile = useMemo(
    () => layout.tiles.find((t): t is NonNullable<Tile> => t !== null && tileKey(t) === activeKey),
    [layout, activeKey],
  );
  const activeSession = activeTile && sessionFor(activeTile);
  const target =
    activeSolo !== null
      ? { dir: activeSolo, serverId: null }
      : activeSession
        ? { dir: activeSession.dir, serverId: activeTile.serverId }
        : { dir: null, serverId: null };

  // Ctrl+Alt+←/→ rotates the solo through the filter bar, Ctrl+Alt+0 clears it.
  // Ctrl+Alt is the one modifier pair left free: plain Alt+arrow is browser
  // back/forward, Cmd+arrow is line-start/end in the shell, and both reach the
  // terminal. Listened for in the capture phase at the window and stopped
  // there, so a focused xterm.js never also sees the keypress (same trick as
  // TerminalTile's Shift+Enter interception, one layer up). Digit0 is read off
  // `code`: with Alt held, macOS reports `key` as "º".
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey || !e.altKey || e.metaKey || e.shiftKey) return;
      let next: string | null;
      if (e.key === "ArrowRight") next = cycleSolo(activeSolo, dirs, 1);
      else if (e.key === "ArrowLeft") next = cycleSolo(activeSolo, dirs, -1);
      else if (e.code === "Digit0") next = null;
      else return;
      e.preventDefault();
      e.stopPropagation();
      e.stopImmediatePropagation();
      setSolo(next);
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [activeSolo, dirs]);

  // With a solo in effect a tile shows iff its session's directory is the
  // solo, whatever the session's status — an ended session in the soloed
  // directory still needs its dismiss button. A tile whose session is unknown
  // (server removed, sessions not loaded yet) has no directory to match and so
  // hides; the soloed directory has a button on screen by construction, so the
  // way back is always one click.
  const { view: packed, map: packedMap } = useMemo(
    () => filterLayout(layout, (tile) => activeSolo === null || sessionFor(tile)?.dir === activeSolo),
    [layout, activeSolo, sessionFor],
  );
  // A soloed directory renders through its overlay: tiles reordered, columns
  // and sizes taken from the overlay, and `map` rebuilt to follow the tiles.
  // normalize (inside applyOverlay) drops any track array whose count no
  // longer matches, so a session arriving or leaving heals the sizes.
  const { view, map } = useMemo(() => applyOverlay(packed, packedMap, overlay), [packed, packedMap, overlay]);
  // Index in the stored layout for a slot on screen. Empty view slots have no
  // counterpart while soloed, so drops onto them are ignored below.
  const realIndex = (i: number): number | undefined => (activeSolo !== null ? map[i] : i);

  // Presentation edits made under a solo belong to that directory, not to the
  // stored layout — that is what keeps the unfiltered grid from being
  // rearranged by an edit the user made inside a filtered view. The first such
  // edit seeds an overlay from what is on screen. `persist` is false for a
  // drag in progress: only the committed value is worth writing to storage.
  const editOverlay = (update: (o: Overlay) => Overlay, persistEdit = true) => {
    if (activeSolo === null) return;
    const next = update(overlay ?? seedOverlay(view));
    if (persistEdit) setViewOverlay(activeSolo, next);
    setOverlay(next);
  };

  async function terminateSession(server: Server, sessionId: number, tileIndex: number) {
    if (confirmTerminate && !window.confirm(`Terminate session #${sessionId}?`)) return;
    try {
      await del(server, `/api/sessions/${sessionId}`);
    } catch {
      // Session may already be gone; drop the tile either way.
    }
    persist((l) => removeTile(l, tileIndex));
    refreshSessions();
  }

  // Sessions running on some server but not shown in any tile. Dead sessions
  // stay in /api/sessions until dismissed — offering those would attach a tile
  // to a tmux session that no longer exists.
  const unplaced = servers.flatMap((server) =>
    (sessionsByServer[server.id] ?? [])
      .filter(
        (sess) =>
          sess.status === "running" &&
          !placed.has(`${server.id}:${sess.id}`) &&
          (activeSolo === null || sess.dir === activeSolo),
      )
      .map((sess) => ({ server, sess })),
  );

  const headerControls = (
    <div className="header-controls">
      <HeaderLauncher
        servers={servers}
        targetDir={target.dir}
        targetServerId={target.serverId}
        onLaunched={placeSession}
      />
      {/* Shows the grid on screen, so under a dir filter it counts the visible
          tiles. Under a solo the count belongs to that directory's overlay;
          otherwise it is a property of the stored layout. */}
      <ColumnStepper
        cols={view.shape.cols}
        rows={view.shape.rows}
        onChange={(c) =>
          activeSolo !== null ? editOverlay((o) => ({ ...o, cols: c })) : persist((l) => setCols(l, c))
        }
      />
      <DirFilterBar dirs={dirs} solo={activeSolo} onSolo={toggleDir} />
      {unplaced.length > 0 && (
        <div className="unplaced-sessions">
          {unplaced.map(({ server, sess }) => (
            <button
              key={`${server.id}:${sess.id}`}
              className="unplaced-session"
              // Prefixed with the visible text rather than replacing it, so
              // the id and title are still announced and voice control can
              // act on the label the user can see (WCAG 2.5.3).
              aria-label={`#${sess.id} ${sessionTitle(toolsByServer[server.id], sess)} — add to grid — ${sess.dir}${
                servers.length > 1 ? ` on ${server.name}` : ""
              }`}
              title={`add to grid — ${sess.dir}${servers.length > 1 ? ` on ${server.name}` : ""}`}
              onClick={() => attachSession(server, sess.id)}
            >
              + #{sess.id} {sessionTitle(toolsByServer[server.id], sess)}
              <span className="unplaced-session-dir">{sess.dir}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );

  const narrow = useMediaQuery(MOBILE_VIEW_QUERY);
  const mobileSessions = useMemo(
    () => orderMobileSessions(layout, servers, sessionsByServer),
    [layout, servers, sessionsByServer],
  );
  const initialLoading = !layoutSettled || servers.some((server) => !settledSessionServers.has(server.id));
  return (
    <div className="grid-page">
      {servers.map((s) => (
        <EventsBridge
          key={s.id}
          server={s}
          onEvent={onServerEvent}
          onStatus={(st) => setStatusByServer((prev) => ({ ...prev, [s.id]: st }))}
        />
      ))}
      {servers
        .filter((s) => statusByServer[s.id] && statusByServer[s.id] !== "open")
        .map((s) => (
          <div key={s.id} className="error server-status-banner">
            <b>{s.name}</b>: {statusMessages[statusByServer[s.id] as Exclude<EventsStatus, "open">]}{" "}
            {statusByServer[s.id] === "auth-expired" &&
              (s.id === "local" ? (
                <button onClick={() => window.location.reload()}>Reload</button>
              ) : (
                // A remote server's stored token is dead; reloading would reuse
                // it forever. Offer a fresh token or removing the server.
                <>
                  <button className="primary" onClick={() => connectServer(s, () => setServers(listServers()))}>
                    Reconnect
                  </button>
                  <button
                    className="danger"
                    onClick={() => {
                      removeServer(s.id);
                      setServers(listServers());
                    }}
                  >
                    Remove server
                  </button>
                </>
              ))}
          </div>
        ))}
      {narrow ? (
        <MobileSessionView
          sessions={mobileSessions}
          toolsByServer={toolsByServer}
          initialLoading={initialLoading}
          onRefresh={refreshSessions}
        />
      ) : (
        <>
          {headerSlot ? createPortal(headerControls, headerSlot) : headerControls}
          <div
            ref={gridRef}
            className="grid"
            style={{
              // Tiles are absolutely positioned rather than laid out by CSS
              // grid: column widths differ per row, which a single grid cannot
              // express, and a row-element-per-row would reparent tiles — which
              // unmounts xterm and reconnects the PTY (see the key comment
              // below).
              position: "relative",
              height: "calc(100vh - 60px)",
            }}
          >
            {view.tiles.map((tile: Tile, i: number) => {
              // view.rowSizes / view.colSizes are non-null on anything
              // normalize() returned, and every layout in GridPage comes from
              // normalize — hence the `!`.
              const rect = tileRect(view.shape, view.rowSizes!, view.colSizes!, i);
              // Non-null for every occupied slot, which is the only kind that
              // renders a remove or terminate control.
              const real = realIndex(i) ?? i;
              return (
                <div
                  // Identity keys: swapping tiles moves the DOM nodes instead of
                  // re-rendering each position with a different session, which would
                  // rebuild xterm and reconnect the WebSocket for both tiles.
                  key={tile ? tileKey(tile) : `empty-${i}`}
                  data-tile-index={i}
                  className={`tile${tile && tileKey(tile) === maximizedKey ? " tile-maximized" : ""}`}
                  style={
                    tile && tileKey(tile) === maximizedKey
                      ? undefined
                      : {
                          position: "absolute",
                          left: `calc(${rect.left}% + var(--tile-gap) / 2)`,
                          top: `calc(${rect.top}% + var(--tile-gap) / 2)`,
                          width: `calc(${rect.width}% - var(--tile-gap))`,
                          height: `calc(${rect.height}% - var(--tile-gap))`,
                        }
                  }
                  // React's onFocus is focusin, so this catches focus landing
                  // anywhere inside the tile — xterm's textarea included.
                  onFocus={() => tile && setActiveKey(tileKey(tile))}
                  draggable={tile !== null && tileKey(tile) !== editingKey}
                  onDragStart={(e) => e.dataTransfer.setData("text/tile-index", String(i))}
                  onDragOver={(e) => {
                    if (e.dataTransfer.types.includes("text/tile-index")) e.preventDefault();
                  }}
                  onDrop={(e) => {
                    e.preventDefault();
                    // Only our own drags: foreign drops yield "" and Number("")
                    // is 0, which would silently swap with tile zero.
                    const raw = e.dataTransfer.getData("text/tile-index");
                    if (!/^\d+$/.test(raw)) return;
                    const rawFrom = Number(raw);
                    if (rawFrom >= view.tiles.length) return;
                    // Under a solo the drag reorders that directory's overlay
                    // and the stored layout is left alone; the two views hold
                    // their own arrangements. Empty slots are not draggable
                    // targets either way.
                    if (activeSolo !== null) {
                      const keys = orderOf(view);
                      if (rawFrom >= keys.length || i >= keys.length || rawFrom === i) return;
                      editOverlay((o) => ({ ...o, order: swapOrder(keys, rawFrom, i) }));
                      return;
                    }
                    const from = realIndex(rawFrom);
                    const to = realIndex(i);
                    // A drop onto an empty slot while filtering has no index in
                    // the stored layout; there is nothing to swap with.
                    if (from === undefined || to === undefined || from === to) return;
                    persist((l) => swapTiles(l, from, to));
                  }}
                >
                  {tile ? (
                    (() => {
                      const server = servers.find((s) => s.id === tile.serverId);
                      // Never fall back to another server: attaching or terminating
                      // would target that server's session with the same id.
                      if (!server) {
                        return (
                          <div className="tile-cell">
                            <div className="tile-header">
                              <span className="tile-title">#{tile.sessionId} · server removed</span>
                              <span className="tile-actions">
                                <button
                                  aria-label={`remove session ${tile.sessionId} from grid`}
                                  title="remove from grid"
                                  onClick={() => persist((l) => removeTile(l, real))}
                                >
                                  −
                                </button>
                              </span>
                            </div>
                            <div className="tile-body empty-tile-hint">
                              This session's server was removed. Re-add the server in Settings or remove this tile.
                            </div>
                          </div>
                        );
                      }
                      const session = (sessionsByServer[tile.serverId] ?? []).find((s) => s.id === tile.sessionId);
                      return (
                        <div className="tile-cell">
                          <div
                            className="tile-header"
                            style={session ? dirTintStyle(session.dir) : undefined}
                            onDoubleClick={() => setMaximizedKey((k) => (k === tileKey(tile) ? null : tileKey(tile)))}
                          >
                            <TileTitle
                              sessionId={tile.sessionId}
                              text={sessionTitle(toolsByServer[tile.serverId], session)}
                              label={session?.label ?? ""}
                              onEditingChange={(editing) => setEditingKey(editing ? tileKey(tile) : null)}
                              onRename={(label) => renameSession(server, tile.sessionId, label)}
                            />
                            {session && (
                              <span className="tile-dir" title={session.dir}>
                                {session.dir}
                              </span>
                            )}
                            {session?.gitState && (
                              <span className="tile-branch">
                                <span
                                  className={`git-dot git-dot-${session.gitState}`}
                                  title={gitStateTitles[session.gitState]}
                                />
                                <span className="tile-branch-name">{session.branch}</span>
                                <TrackingMarks session={session} />
                              </span>
                            )}
                            {session?.repoUrl && (
                              <a
                                className="tile-repo-link"
                                href={session.repoUrl}
                                target="_blank"
                                rel="noreferrer"
                                aria-label="open repository on GitHub"
                                title={session.repoUrl}
                              >
                                <GitHubIcon />
                              </a>
                            )}
                            <span className="tile-actions">
                              <button
                                aria-label={`remove session ${tile.sessionId} from grid`}
                                title="remove from grid"
                                onClick={() => persist((l) => removeTile(l, real))}
                              >
                                −
                              </button>
                              <button
                                className="danger"
                                aria-label={`terminate session ${tile.sessionId}`}
                                title="terminate session"
                                onClick={() => terminateSession(server, tile.sessionId, real)}
                              >
                                ✕
                              </button>
                            </span>
                          </div>
                          {session && session.status !== "running" ? (
                            // Dead sessions must not mount a terminal: the daemon
                            // rejects the attach and the tile would retry forever.
                            <div className="tile-body empty-tile-hint">
                              session ended <button onClick={() => persist((l) => removeTile(l, real))}>dismiss</button>
                            </div>
                          ) : (
                            <div className="tile-body">
                              <TerminalTile
                                server={server}
                                sessionId={tile.sessionId}
                                autoFocus={tileKey(tile) === focusKey}
                                onClose={() => persist((l) => removeTile(l, real))}
                              />
                            </div>
                          )}
                        </div>
                      );
                    })()
                  ) : (
                    <EmptyTile
                      servers={servers}
                      sessionsByServer={sessionsByServer}
                      placed={placed}
                      onAttach={attachSession}
                    />
                  )}
                </div>
              );
            })}
            <GridDividers
              layout={view}
              containerRef={gridRef}
              onPreview={(next) =>
                activeSolo !== null
                  ? editOverlay((o) => ({ ...o, rowSizes: next.rowSizes!, colSizes: next.colSizes! }), false)
                  : adoptLayout(next)
              }
              onCommit={(next) =>
                activeSolo !== null
                  ? editOverlay((o) => ({ ...o, rowSizes: next.rowSizes!, colSizes: next.colSizes! }))
                  : persist(() => next)
              }
            />
          </div>
        </>
      )}
    </div>
  );
}

function EmptyTile({
  servers,
  sessionsByServer,
  placed,
  onAttach,
}: {
  servers: Server[];
  sessionsByServer: Record<string, Session[]>;
  placed: Set<string>;
  onAttach: (server: Server, sessionId: number) => void;
}) {
  const attachable = servers
    .map((s) => ({
      server: s,
      sessions: (sessionsByServer[s.id] ?? []).filter(
        (sess) => sess.status === "running" && !placed.has(`${s.id}:${sess.id}`),
      ),
    }))
    .filter(({ sessions }) => sessions.length > 0);

  if (attachable.length === 0) {
    return <div className="empty-tile empty-tile-hint">＋ New in the header to launch a session</div>;
  }

  return (
    <div className="empty-tile">
      {attachable.map(({ server, sessions }) => (
        <select
          key={server.id}
          className="empty-tile-attach"
          value=""
          onChange={(e) => {
            const id = Number(e.target.value);
            if (id) onAttach(server, id);
          }}
        >
          <option value="" disabled>
            attach session on {server.name}…
          </option>
          {sessions.map((sess) => (
            <option key={sess.id} value={sess.id}>
              {sess.label || sess.tmuxName}
            </option>
          ))}
        </select>
      ))}
    </div>
  );
}
