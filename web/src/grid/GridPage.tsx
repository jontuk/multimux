import { useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { getJSON, putJSON } from "../api";
import { listServers, localServer, type Server } from "../servers";
import { addTile, emptyLayout, normalize, setCols, setTile, swapTiles, type Layout, type Tile } from "./model";
import ColumnStepper from "./ColumnStepper";
import HeaderLauncher from "./HeaderLauncher";
import TerminalTile from "../term/TerminalTile";
import { useEvents, type EventsStatus } from "../useEvents";
import type { Session } from "./types";

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

function tileKey(t: NonNullable<Tile>): string {
  return `${t.serverId}:${t.sessionId}`;
}

export default function GridPage({ headerSlot = null }: { headerSlot?: HTMLElement | null }) {
  const [layout, setLayout] = useState<Layout>(emptyLayout());
  const [sessionsByServer, setSessionsByServer] = useState<Record<string, Session[]>>({});
  const [statusByServer, setStatusByServer] = useState<Record<string, EventsStatus>>({});
  // Stable across re-renders so the events sockets don't churn; listServers()
  // reads localStorage and returns a fresh array each call.
  const servers = useMemo(() => listServers(), []);

  const persist = useCallback((l: Layout) => {
    setLayout(l);
    putJSON(localServer(), "/api/layout", l).catch(() => {});
  }, []);

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
      if (isLayout(v)) setLayout(normalize(v.tiles, v.shape.cols));
    });
  }, []);

  const onServerEvent = useCallback(
    (type: string) => {
      if (type.startsWith("session_")) refreshSessions();
      if (type === "layout_changed") refreshLayout();
    },
    [refreshSessions, refreshLayout],
  );

  useEffect(() => {
    refreshLayout();
    refreshSessions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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

  const headerControls = (
    <div className="header-controls">
      <HeaderLauncher servers={servers} onLaunched={placeSession} />
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
            className="tile"
            draggable={tile !== null}
            onDragStart={(e) => e.dataTransfer.setData("text/tile-index", String(i))}
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              const from = Number(e.dataTransfer.getData("text/tile-index"));
              if (!Number.isNaN(from) && from !== i) persist(swapTiles(layout, from, i));
            }}
          >
            {tile ? (
              <TerminalTile
                server={servers.find((s) => s.id === tile.serverId) ?? localServer()}
                sessionId={tile.sessionId}
                onClose={() => persist(setTile(layout, i, null))}
              />
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
      sessions: (sessionsByServer[s.id] ?? []).filter((sess) => !placed.has(`${s.id}:${sess.id}`)),
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
