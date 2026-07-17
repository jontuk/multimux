import { useCallback, useEffect, useMemo, useState } from "react";
import { getJSON, postJSON, putJSON } from "../api";
import { listServers, localServer, type Server } from "../servers";
import { emptyLayout, reshape, setTile, swapTiles, type GridShape, type Layout, type Tile } from "./model";
import ShapePicker from "./ShapePicker";
import TerminalTile from "../term/TerminalTile";
import { useEvents, type EventsStatus } from "../useEvents";

type Session = { id: number; tmuxName: string; toolId: number; dir: string; status: string };
type Tool = { id: number; name: string; command: string };
type Dir = { id: number; name: string; path: string };

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

export default function GridPage() {
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
      if (isLayout(v)) setLayout(v);
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

  function attachSession(server: Server, sessionId: number, index: number) {
    persist(setTile(layout, index, { serverId: server.id, sessionId }));
  }

  async function launchSession(server: Server, index: number) {
    let tools: Tool[], dirs: Dir[];
    try {
      [tools, dirs] = await Promise.all([getJSON<Tool[]>(server, "/api/tools"), getJSON<Dir[]>(server, "/api/dirs")]);
    } catch (e) {
      window.alert(`could not reach ${server.name}: ${e}`);
      return;
    }
    if (tools.length === 0 || dirs.length === 0) {
      window.alert(
        `${server.name} has no ${tools.length === 0 ? "tools" : "directories"} configured — add one in Settings (⚙) first`,
      );
      return;
    }
    // Minimal launcher: prompt-based pick; a modal can replace this later.
    const toolName = window.prompt(`tool? (${tools.map((t) => t.name).join(", ")})`);
    if (toolName === null) return; // cancelled
    const tool = tools.find((t) => t.name === toolName);
    if (!tool) {
      window.alert(`no tool named "${toolName}"`);
      return;
    }
    const dirName = window.prompt(`dir? (${dirs.map((d) => d.name).join(", ")})`);
    if (dirName === null) return; // cancelled
    const dir = dirs.find((d) => d.name === dirName);
    if (!dir) {
      window.alert(`no dir named "${dirName}"`);
      return;
    }
    try {
      const sess = await postJSON<Session>(server, "/api/sessions", { toolId: tool.id, dirId: dir.id });
      persist(setTile(layout, index, { serverId: server.id, sessionId: sess.id }));
    } catch (e) {
      window.alert(`launch failed: ${e}`);
      return;
    }
    refreshSessions();
  }

  const { rows, cols } = layout.shape;
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
            {statusByServer[s.id] === "auth-expired" && (
              <button onClick={() => window.location.reload()}>Reload</button>
            )}
          </div>
        ))}
      <div className="grid-toolbar">
        <ShapePicker value={layout.shape} onChange={(s: GridShape) => persist(reshape(layout, s))} />
      </div>
      <div
        className="grid"
        style={{
          display: "grid",
          gridTemplateRows: `repeat(${rows}, 1fr)`,
          gridTemplateColumns: `repeat(${cols}, 1fr)`,
          gap: 4,
          height: "calc(100vh - 80px)",
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
              <div className="empty-tile">
                {servers.map((s) => (
                  <div key={s.id} className="empty-tile-server">
                    <select
                      defaultValue=""
                      onChange={(e) => {
                        const id = Number(e.target.value);
                        if (id) attachSession(s, id, i);
                      }}
                    >
                      <option value="" disabled>
                        session on {s.name}…
                      </option>
                      {(sessionsByServer[s.id] ?? []).map((sess) => (
                        <option key={sess.id} value={sess.id}>
                          {sess.tmuxName}
                        </option>
                      ))}
                    </select>
                    <button onClick={() => launchSession(s, i)}>+ new on {s.name}</button>
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
