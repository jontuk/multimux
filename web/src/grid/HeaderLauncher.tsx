import { useEffect, useState } from "react";
import { getJSON, postJSON } from "../api";
import type { Server } from "../servers";
import type { Dir, Session, Tool } from "./types";

export default function HeaderLauncher({
  servers,
  onLaunched,
}: {
  servers: Server[];
  onLaunched: (server: Server, session: Session) => void;
}) {
  const [serverId, setServerId] = useState(servers[0]?.id ?? "");
  const [tools, setTools] = useState<Tool[]>([]);
  const [dirs, setDirs] = useState<Dir[]>([]);
  const [toolId, setToolId] = useState(0);
  const [dirId, setDirId] = useState(0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const server = servers.find((s) => s.id === serverId);

  useEffect(() => {
    if (!server) return;
    let stale = false;
    Promise.all([getJSON<Tool[]>(server, "/api/tools"), getJSON<Dir[]>(server, "/api/dirs")])
      .then(([t, d]) => {
        if (stale) return;
        setTools(t);
        setDirs(d);
        setToolId(t[0]?.id ?? 0);
        setDirId(d[0]?.id ?? 0);
        setError("");
      })
      .catch(() => {
        if (stale) return;
        setTools([]);
        setDirs([]);
        setError(`can't reach ${server.name}`);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId]);

  if (!server) return null;

  const unconfigured = !error && (tools.length === 0 || dirs.length === 0);

  async function launch() {
    if (!server) return;
    setBusy(true);
    setError("");
    try {
      const sess = await postJSON<Session>(server, "/api/sessions", { toolId, dirId });
      onLaunched(server, sess);
    } catch (e) {
      setError(`launch failed: ${e instanceof Error ? e.message : e}`);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="header-launcher">
      {servers.length > 1 && (
        <select aria-label="server" value={serverId} onChange={(e) => setServerId(e.target.value)}>
          {servers.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      )}
      {unconfigured ? (
        <span className="launcher-hint">
          add {tools.length === 0 ? "tools" : "dirs"} in <a href="#/settings">Settings</a>
        </span>
      ) : (
        <>
          <select aria-label="tool" value={toolId} onChange={(e) => setToolId(Number(e.target.value))}>
            {tools.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
          <select aria-label="dir" value={dirId} onChange={(e) => setDirId(Number(e.target.value))}>
            {dirs.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </>
      )}
      <button
        className="launch"
        disabled={busy || unconfigured || !!error}
        title="launch a new session"
        onClick={launch}
      >
        + New
      </button>
      {error && <span className="launcher-error">{error}</span>}
    </div>
  );
}
