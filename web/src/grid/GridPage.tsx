import { useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { del, getJSON, putJSON } from "../api";
import { listServers, localServer, type Server } from "../servers";
import { addTile, emptyLayout, normalize, setCols, setTile, swapTiles, type Layout, type Tile } from "./model";
import ColumnStepper from "./ColumnStepper";
import HeaderLauncher from "./HeaderLauncher";
import TerminalTile from "../term/TerminalTile";
import { useEvents, type EventsStatus } from "../useEvents";
import type { Session, Tool } from "./types";

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

const gitStateTitles = {
  untracked: "untracked files present",
  modified: "tracked files modified",
  clean: "working tree clean",
} as const;

function tileKey(t: NonNullable<Tile>): string {
  return `${t.serverId}:${t.sessionId}`;
}

// Tool name for display; falls back to the tmux session name while tools load.
function toolName(tools: Tool[] | undefined, session: Session | undefined): string {
  if (!session) return "…";
  return tools?.find((t) => t.id === session.toolId)?.name ?? session.tmuxName;
}

export default function GridPage({ headerSlot = null }: { headerSlot?: HTMLElement | null }) {
  const [layout, setLayout] = useState<Layout>(emptyLayout());
  const [sessionsByServer, setSessionsByServer] = useState<Record<string, Session[]>>({});
  const [toolsByServer, setToolsByServer] = useState<Record<string, Tool[]>>({});
  const [statusByServer, setStatusByServer] = useState<Record<string, EventsStatus>>({});
  // Ephemeral: which tile fills the viewport (tile key), or null for grid view.
  const [maximizedKey, setMaximizedKey] = useState<string | null>(null);
  // Stable across re-renders so the events sockets don't churn; listServers()
  // reads localStorage and returns a fresh array each call.
  const servers = useMemo(() => listServers(), []);

  // A maximized tile that leaves the layout (removed, terminated, server-side
  // layout change) must not leave the page stuck fullscreen — or re-maximize
  // if the same session is later re-added.
  const adoptLayout = useCallback((l: Layout) => {
    setLayout(l);
    setMaximizedKey((k) => (k && !l.tiles.some((t) => t && tileKey(t) === k) ? null : k));
  }, []);

  const persist = useCallback(
    (l: Layout) => {
      adoptLayout(l);
      putJSON(localServer(), "/api/layout", l).catch(() => {});
    },
    [adoptLayout],
  );

  const refreshSessions = useCallback(() => {
    for (const server of servers) {
      getJSON<Session[]>(server, "/api/sessions")
        .then((s) => setSessionsByServer((prev) => ({ ...prev, [server.id]: s })))
        .catch(() => setSessionsByServer((prev) => ({ ...prev, [server.id]: [] })));
    }
  }, [servers]);

  const refreshLayout = useCallback(() => {
    getJSON<unknown>(localServer(), "/api/layout").then((v) => {
      // Normalize so layouts persisted before rows were derived still load cleanly.
      if (isLayout(v)) adoptLayout(normalize(v.tiles, v.shape.cols));
    });
  }, [adoptLayout]);

  const onServerEvent = useCallback(
    (type: string) => {
      if (type.startsWith("session_") || type === "git_changed") refreshSessions();
      if (type === "layout_changed") refreshLayout();
    },
    [refreshSessions, refreshLayout],
  );

  useEffect(() => {
    refreshLayout();
    refreshSessions();
    for (const server of servers) {
      getJSON<Tool[]>(server, "/api/tools")
        .then((t) => setToolsByServer((prev) => ({ ...prev, [server.id]: t })))
        .catch(() => {});
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
    persist(addTile(layout, { serverId: server.id, sessionId }));
  }

  function placeSession(server: Server, session: Session) {
    persist(addTile(layout, { serverId: server.id, sessionId: session.id }));
    refreshSessions();
  }

  async function terminateSession(server: Server, sessionId: number, tileIndex: number) {
    if (!window.confirm(`Terminate session #${sessionId}?`)) return;
    try {
      await del(server, `/api/sessions/${sessionId}`);
    } catch {
      // Session may already be gone; drop the tile either way.
    }
    persist(setTile(layout, tileIndex, null));
    refreshSessions();
  }

  // Sessions running on some server but not shown in any tile. Dead sessions
  // stay in /api/sessions until dismissed — offering those would attach a tile
  // to a tmux session that no longer exists.
  const unplaced = servers.flatMap((server) =>
    (sessionsByServer[server.id] ?? [])
      .filter((sess) => sess.status === "running" && !placed.has(`${server.id}:${sess.id}`))
      .map((sess) => ({ server, sess })),
  );

  const headerControls = (
    <div className="header-controls">
      <HeaderLauncher servers={servers} onLaunched={placeSession} />
      {unplaced.length > 0 && (
        <div className="unplaced-sessions">
          {unplaced.map(({ server, sess }) => (
            <button
              key={`${server.id}:${sess.id}`}
              className="unplaced-session"
              title={`add to grid — ${sess.dir}${servers.length > 1 ? ` on ${server.name}` : ""}`}
              onClick={() => attachSession(server, sess.id)}
            >
              + #{sess.id} {toolName(toolsByServer[server.id], sess)}
            </button>
          ))}
        </div>
      )}
      <ColumnStepper cols={layout.shape.cols} rows={layout.shape.rows} onChange={(c) => persist(setCols(layout, c))} />
    </div>
  );

  const { rows, cols } = layout.shape;
  return (
    <div className="grid-page">
      {headerSlot ? createPortal(headerControls, headerSlot) : headerControls}
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
            {statusByServer[s.id] === "auth-expired" && (
              <button onClick={() => window.location.reload()}>Reload</button>
            )}
          </div>
        ))}
      <div
        className="grid"
        style={{
          display: "grid",
          gridTemplateRows: `repeat(${rows}, 1fr)`,
          gridTemplateColumns: `repeat(${cols}, 1fr)`,
          gap: 4,
          height: "calc(100vh - 60px)",
        }}
      >
        {layout.tiles.map((tile: Tile, i: number) => (
          <div
            key={i}
            className={`tile${tile && tileKey(tile) === maximizedKey ? " tile-maximized" : ""}`}
            draggable={tile !== null}
            onDragStart={(e) => e.dataTransfer.setData("text/tile-index", String(i))}
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              const from = Number(e.dataTransfer.getData("text/tile-index"));
              if (!Number.isNaN(from) && from !== i) persist(swapTiles(layout, from, i));
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
                            onClick={() => persist(setTile(layout, i, null))}
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
                      onDoubleClick={() => setMaximizedKey((k) => (k === tileKey(tile) ? null : tileKey(tile)))}
                    >
                      <span className="tile-title">
                        #{tile.sessionId} · {toolName(toolsByServer[tile.serverId], session)}
                      </span>
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
                          {session.branch}
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
                          onClick={() => persist(setTile(layout, i, null))}
                        >
                          −
                        </button>
                        <button
                          className="danger"
                          aria-label={`terminate session ${tile.sessionId}`}
                          title="terminate session"
                          onClick={() => terminateSession(server, tile.sessionId, i)}
                        >
                          ✕
                        </button>
                      </span>
                    </div>
                    {session && session.status !== "running" ? (
                      // Dead sessions must not mount a terminal: the daemon
                      // rejects the attach and the tile would retry forever.
                      <div className="tile-body empty-tile-hint">
                        session ended <button onClick={() => persist(setTile(layout, i, null))}>dismiss</button>
                      </div>
                    ) : (
                      <div className="tile-body">
                        <TerminalTile
                          server={server}
                          sessionId={tile.sessionId}
                          onClose={() => persist(setTile(layout, i, null))}
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
        ))}
      </div>
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
              {sess.tmuxName}
            </option>
          ))}
        </select>
      ))}
    </div>
  );
}
