import { useEffect, useRef, useState } from "react";
import { del, getJSON, postJSON } from "../api";
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
  const [subdir, setSubdir] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState(-1);
  const [error, setError] = useState("");
  // `loading` means "this server's tools/dirs are still being fetched";
  // `busy` means "a launch is in flight".
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const server = servers.find((s) => s.id === serverId);

  // Lets an in-flight forget() know, once its DELETE settles, whether the
  // user has since switched directories — mirrors the `stale` guard the
  // fetch effects use, but as a ref since forget() is an event handler, not
  // an effect. Updated in an effect rather than during render, per the rules
  // of hooks.
  const dirIdRef = useRef(dirId);
  useEffect(() => {
    dirIdRef.current = dirId;
  }, [dirId]);

  // Switching servers must drop the previous daemon's options in the same
  // render as the switch: tool/dir ids are per-daemon autoincrements, so a
  // leftover id would launch a different tool on the new daemon.
  function selectServer(id: string) {
    setServerId(id);
    setTools([]);
    setDirs([]);
    setToolId(0);
    setDirId(0);
    // A subdir is relative to the previous daemon's directory; it rarely means
    // the same thing on another machine, so it does not survive the switch.
    setSubdir("");
    setHistory([]);
    setOpen(false);
    setHighlight(-1);
    setError("");
    setLoading(true);
  }

  // A subdir names a path under the selected directory. Changing the directory
  // makes it meaningless, so it is dropped rather than silently re-pointed.
  function selectDir(id: number) {
    setDirId(id);
    setSubdir("");
    setHistory([]);
    setOpen(false);
    setHighlight(-1);
    setError("");
  }

  useEffect(() => {
    if (!server) return;
    // `loading` is already true here: it starts true and selectServer() re-arms
    // it in the same render as the switch.
    let stale = false;
    Promise.all([getJSON<Tool[]>(server, "/api/tools"), getJSON<Dir[]>(server, "/api/dirs")])
      .then(([t, d]) => {
        if (stale) return;
        setTools(t);
        setDirs(d);
        setToolId(t[0]?.id ?? 0);
        setDirId(d[0]?.id ?? 0);
        setError("");
        setLoading(false);
      })
      .catch(() => {
        if (stale) return;
        setTools([]);
        setDirs([]);
        setError(`can't reach ${server.name}`);
        setLoading(false);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId]);

  // Same `stale` guard as the tools/dirs fetch: a slow answer for a directory
  // the user has already moved on from must not land over the current one.
  useEffect(() => {
    if (!server || dirId <= 0) return;
    let stale = false;
    getJSON<string[]>(server, `/api/dirs/${dirId}/subdirs`)
      .then((h) => {
        if (!stale) setHistory(h);
      })
      .catch(() => {
        // History is a convenience; failing to load it must not break launching.
        if (!stale) setHistory([]);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, dirId]);

  if (!server) return null;

  // Empty lists only mean "nothing configured" once the fetch has resolved;
  // mid-load they are just the cleared state.
  const unconfigured = !loading && !error && (tools.length === 0 || dirs.length === 0);
  const canLaunch = !loading && !busy && toolId > 0 && dirId > 0;

  // Substring, case-insensitive: a remembered "internal/server" should be
  // reachable by typing "serv", not only by typing its prefix.
  const needle = subdir.trim().toLowerCase();
  const filtered = history.filter((h) => h.toLowerCase().includes(needle));
  const showHistory = open && filtered.length > 0;

  async function launch() {
    if (!server || !canLaunch) return;
    setBusy(true);
    setError("");
    try {
      const sess = await postJSON<Session>(server, "/api/sessions", { toolId, dirId, subdir });
      onLaunched(server, sess);
      // The daemon has recorded this too; updating locally keeps the dropdown
      // right without a second round trip.
      const used = subdir.trim();
      if (used) setHistory((h) => [used, ...h.filter((x) => x !== used)]);
    } catch (e) {
      setError(`launch failed: ${e instanceof Error ? e.message : e}`);
    } finally {
      setBusy(false);
    }
  }

  // Optimistic: the row disappears at once, and a failed DELETE puts it back
  // rather than leaving the UI claiming something was forgotten.
  async function forget(value: string) {
    if (!server) return;
    const forDirId = dirId;
    const previous = history;
    setHistory((h) => h.filter((x) => x !== value));
    try {
      await del(server, `/api/dirs/${forDirId}/subdirs?subdir=${encodeURIComponent(value)}`);
    } catch (e) {
      // If the user has since switched directories, this directory's history
      // is no longer on screen: restoring it would clobber the new
      // directory's freshly loaded list, and the error would refer to
      // nothing the user can see.
      if (dirIdRef.current !== forDirId) return;
      setHistory(previous);
      setError(`couldn't forget ${value}: ${e instanceof Error ? e.message : e}`);
    }
  }

  return (
    <div className="header-launcher">
      {servers.length > 1 && (
        <select aria-label="server" value={serverId} onChange={(e) => selectServer(e.target.value)}>
          {servers.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      )}
      {loading ? (
        <span className="launcher-hint">loading…</span>
      ) : unconfigured ? (
        <span className="launcher-hint">
          add {tools.length === 0 ? "tools" : "dirs"} in <a href="#/settings">Settings</a>
        </span>
      ) : (
        <>
          <select
            aria-label="tool"
            value={toolId}
            onChange={(e) => {
              setToolId(Number(e.target.value));
              setError("");
            }}
          >
            {tools.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
          <select aria-label="dir" value={dirId} onChange={(e) => selectDir(Number(e.target.value))}>
            {dirs.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
          <div className="subdir-wrap">
            <input
              className="subdir"
              aria-label="subdirectory"
              placeholder="subdir"
              value={subdir}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              onFocus={() => setOpen(true)}
              onBlur={() => setOpen(false)}
              onChange={(e) => {
                setSubdir(e.target.value);
                setHighlight(-1);
                setError("");
              }}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  if (filtered.length === 0) return;
                  e.preventDefault();
                  setOpen(true);
                  const step = e.key === "ArrowDown" ? 1 : -1;
                  setHighlight((h) =>
                    h < 0 ? (step > 0 ? 0 : filtered.length - 1) : (h + step + filtered.length) % filtered.length,
                  );
                } else if (e.key === "Enter") {
                  if (showHistory && highlight >= 0 && highlight < filtered.length) {
                    e.preventDefault();
                    setSubdir(filtered[highlight]);
                    setHighlight(-1);
                    setOpen(false);
                  } else {
                    launch();
                  }
                } else if (e.key === "Escape") {
                  if (open) {
                    setOpen(false);
                    setHighlight(-1);
                  } else {
                    e.currentTarget.blur();
                  }
                }
              }}
            />
            {showHistory && (
              // preventDefault on mousedown keeps the input's blur from firing
              // first: without it the panel unmounts before any click lands.
              <div className="subdir-history" onMouseDown={(e) => e.preventDefault()}>
                {filtered.map((h, i) => (
                  <div
                    key={h}
                    className={`subdir-history-row${i === highlight ? " on" : ""}`}
                    onMouseEnter={() => setHighlight(-1)}
                  >
                    <button
                      type="button"
                      className="subdir-pick"
                      onClick={() => {
                        setSubdir(h);
                        setOpen(false);
                      }}
                    >
                      {h}
                    </button>
                    <button
                      type="button"
                      className="subdir-forget"
                      aria-label={`forget ${h}`}
                      onClick={() => forget(h)}
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}
      <button className="launch" disabled={!canLaunch} title="launch a new session" onClick={launch}>
        + New
      </button>
      {error && <span className="launcher-error">{error}</span>}
    </div>
  );
}
