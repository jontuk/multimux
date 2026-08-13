import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { del, getJSON, postJSON } from "../api";
import type { Server } from "../servers";
import { splitUnderDir } from "./dirFilter";
import type { Dir, Session, Tool } from "./types";

// A dropdown longer than this stops being a shortcut: past a dozen entries the
// user is faster typing another character than reading the list.
const suggestionLimit = 12;

export default function HeaderLauncher({
  servers,
  targetDir = null,
  onLaunched,
}: {
  servers: Server[];
  /**
   * Working directory the grid is currently soloed on, or null for none. The
   * launcher points itself at it so "+ New" opens another session where the
   * user is already looking.
   */
  targetDir?: string | null;
  onLaunched: (server: Server, session: Session) => void;
}) {
  const [serverId, setServerId] = useState(servers[0]?.id ?? "");
  const [tools, setTools] = useState<Tool[]>([]);
  const [dirs, setDirs] = useState<Dir[]>([]);
  const [toolId, setToolId] = useState(0);
  const [dirId, setDirId] = useState(0);
  const [subdir, setSubdir] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  // Real child directories of whatever parent segment is typed, newest fetch
  // wins. `childrenOf` records which parent they describe so a list from the
  // previous segment is never filtered as if it belonged to this one.
  const [children, setChildren] = useState<string[]>([]);
  const [childrenOf, setChildrenOf] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState(-1);
  const [error, setError] = useState("");
  // `loading` means "this server's tools/dirs are still being fetched";
  // `busy` means "a launch is in flight".
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const server = servers.find((s) => s.id === serverId);

  // Lets in-flight async work (forget's DELETE, launch's history bump) know,
  // once it settles, whether the user has since moved on to a different
  // directory or server — dir ids are per-daemon autoincrements, so a bare
  // dirId match is not enough; a server switch can hand back the same
  // number on a different daemon. Assigned synchronously wherever the
  // selection changes (selectServer, selectDir, and the tools/dirs fetch's
  // auto-select), never from a passive effect, so a microtask that resumes
  // before the next render still sees the true current selection.
  const selectionRef = useRef({ serverId, dirId });

  // The solo below can also move the selection, and it does so during render,
  // where writing a ref is not allowed. A layout effect closes that gap: it
  // runs during the commit, before paint and before any microtask a later
  // event could schedule, so nothing observes a selectionRef out of step with
  // what is on screen. It writes the same value selectServer/selectDir already
  // wrote when the change came from those.
  useLayoutEffect(() => {
    selectionRef.current = { serverId, dirId };
  }, [serverId, dirId]);

  // Switching servers must drop the previous daemon's options in the same
  // render as the switch: tool/dir ids are per-daemon autoincrements, so a
  // leftover id would launch a different tool on the new daemon.
  function selectServer(id: string) {
    selectionRef.current = { serverId: id, dirId: 0 };
    setServerId(id);
    setTools([]);
    setDirs([]);
    setToolId(0);
    setDirId(0);
    // A subdir is relative to the previous daemon's directory; it rarely means
    // the same thing on another machine, so it does not survive the switch.
    setSubdir("");
    setHistory([]);
    setChildren([]);
    setChildrenOf(null);
    setOpen(false);
    setHighlight(-1);
    setError("");
    setLoading(true);
  }

  // A subdir names a path under the selected directory. Changing the directory
  // makes it meaningless, so it is dropped rather than silently re-pointed.
  function selectDir(id: number) {
    selectionRef.current = { serverId, dirId: id };
    setDirId(id);
    setSubdir("");
    setHistory([]);
    setChildren([]);
    setChildrenOf(null);
    setOpen(false);
    setHighlight(-1);
    setError("");
  }

  // "web/src/comp" is a settled path ("web/src/") plus a fragment still being
  // typed ("comp"). The daemon lists one directory's children; the fragment is
  // filtered here, so a fetch happens once per segment, not once per keystroke.
  const cut = subdir.lastIndexOf("/");
  const typedParent = cut < 0 ? "" : subdir.slice(0, cut + 1);
  const typedLeaf = cut < 0 ? subdir : subdir.slice(cut + 1);

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
        const autoDirId = d[0]?.id ?? 0;
        setDirId(autoDirId);
        selectionRef.current = { serverId, dirId: autoDirId };
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

  // Follow the grid's soloed directory: soloing is the user saying "this is the
  // project I'm in", so the launcher aims at it and "+ New" is one click from
  // another session in the same place. A session's working directory is a
  // launch dir plus a subdir, and both halves are restored — soloing
  // .../multimux/web fills the subdir too, not just the repo.
  //
  // Only the directory moves; the tool keeps whatever the user chose. A solo
  // this daemon has no directory for (it belongs to another server, or its dir
  // was removed) leaves the selection untouched rather than resetting it.
  // Deliberately overwrites a hand-typed subdir: the solo is the more recent
  // statement of intent, and the typed one is one keystroke away again.
  // Adjusted during render rather than in an effect, so the launcher never
  // paints a frame pointing at the old directory (and never fetches that
  // directory's history on the way past). `applied` records the pair the
  // current selection was derived from — the solo *and* the dir list, because
  // both arrive asynchronously: a solo that matched nothing while the list was
  // still loading is applied the moment the list lands.
  const [applied, setApplied] = useState<{ target: string; dirs: Dir[] } | null>(null);
  if (targetDir !== null && dirs.length > 0 && (applied?.target !== targetDir || applied.dirs !== dirs)) {
    setApplied({ target: targetDir, dirs });
    const match = splitUnderDir(dirs, targetDir);
    if (match && (match.dirId !== dirId || match.subdir !== subdir)) {
      setDirId(match.dirId);
      setSubdir(match.subdir);
      setChildren([]);
      setChildrenOf(null);
      setOpen(false);
      setHighlight(-1);
      setError("");
    }
  }

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

  // Only while the dropdown is open: an unfocused launcher has nothing to
  // suggest into, and the listing is worth re-reading on every focus anyway —
  // directories appear and vanish behind the daemon's back.
  useEffect(() => {
    if (!server || dirId <= 0 || !open) return;
    let stale = false;
    const parent = typedParent;
    getJSON<string[]>(server, `/api/dirs/${dirId}/children?path=${encodeURIComponent(parent)}`)
      .then((c) => {
        if (stale) return;
        setChildren(c);
        setChildrenOf(parent);
      })
      .catch(() => {
        // Suggestions are a convenience: a failure leaves the history-only list.
        if (stale) return;
        setChildren([]);
        setChildrenOf(parent);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, dirId, typedParent, open]);

  if (!server) return null;

  // Empty lists only mean "nothing configured" once the fetch has resolved;
  // mid-load they are just the cleared state.
  const unconfigured = !loading && !error && (tools.length === 0 || dirs.length === 0);
  const canLaunch = !loading && !busy && toolId > 0 && dirId > 0;

  // Substring, case-insensitive: a remembered "internal/server" should be
  // reachable by typing "serv", not only by typing its prefix.
  const needle = subdir.trim().toLowerCase();
  const filtered = history.filter((h) => h.toLowerCase().includes(needle));

  // Directories that really exist under the typed parent. Prefix-matched, not
  // substring: this is path completion, and the parent segment is already
  // pinned. Stale lists (still describing the previous segment) suggest nothing.
  const leaf = typedLeaf.toLowerCase();
  const suggestions =
    childrenOf === typedParent
      ? children
          .filter((n) => n.toLowerCase().startsWith(leaf))
          // Hidden directories are launch targets too — .config, .github — but
          // they only clutter the list until the user types the dot.
          .filter((n) => leaf.startsWith(".") || !n.startsWith("."))
          .map((n) => typedParent + n)
          .filter((p) => p !== subdir.trim() && !filtered.includes(p))
          .slice(0, suggestionLimit)
      : [];

  // History first: a subdir launched before is a better guess than the
  // alphabetical neighbours it shares a parent with.
  const options = [
    ...filtered.map((value) => ({ value, remembered: true })),
    ...suggestions.map((value) => ({ value, remembered: false })),
  ];
  const showMenu = open && options.length > 0;

  async function launch() {
    if (!server || !canLaunch) return;
    const issuedFor = selectionRef.current;
    setBusy(true);
    setError("");
    try {
      const sess = await postJSON<Session>(server, "/api/sessions", { toolId, dirId, subdir });
      onLaunched(server, sess);
      // The daemon has recorded this too; updating locally keeps the dropdown
      // right without a second round trip. But if the user has since picked
      // a different directory or server, that dropdown belongs to someone
      // else's history now — bumping it here would prepend a subdir the
      // daemon never recorded for the directory on screen.
      const used = subdir.trim();
      const current = selectionRef.current;
      if (used && current.serverId === issuedFor.serverId && current.dirId === issuedFor.dirId) {
        setHistory((h) => [used, ...h.filter((x) => x !== used)]);
      }
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
    const issuedFor = selectionRef.current;
    const previous = history;
    setHistory((h) => h.filter((x) => x !== value));
    setHighlight(-1);
    try {
      await del(server, `/api/dirs/${issuedFor.dirId}/subdirs?subdir=${encodeURIComponent(value)}`);
    } catch (e) {
      // If the user has since switched directories or servers, this
      // directory's history is no longer on screen: restoring it would
      // clobber the new selection's freshly loaded list (dir ids are
      // per-daemon autoincrements, so a bare dirId match could otherwise
      // match a different daemon's directory), and the error would refer to
      // nothing the user can see.
      const current = selectionRef.current;
      if (current.serverId !== issuedFor.serverId || current.dirId !== issuedFor.dirId) return;
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
          <div className={`subdir-wrap${open || subdir ? " slashed" : ""}`}>
            {(open || subdir) && (
              <span className="subdir-slash" aria-hidden="true">
                /
              </span>
            )}
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
                setOpen(true);
                setHighlight(-1);
                setError("");
              }}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  if (options.length === 0) return;
                  e.preventDefault();
                  setOpen(true);
                  const step = e.key === "ArrowDown" ? 1 : -1;
                  setHighlight((h) =>
                    h < 0 ? (step > 0 ? 0 : options.length - 1) : (h + step + options.length) % options.length,
                  );
                } else if (e.key === "Enter") {
                  if (showMenu && highlight >= 0 && highlight < options.length) {
                    e.preventDefault();
                    setSubdir(options[highlight].value);
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
            {showMenu && (
              // preventDefault on mousedown keeps the input's blur from firing
              // first: without it the panel unmounts before any click lands.
              <div className="subdir-history" onMouseDown={(e) => e.preventDefault()}>
                {options.map((o, i) => (
                  <div
                    key={o.value}
                    className={`subdir-history-row${i === highlight ? " on" : ""}`}
                    onMouseEnter={() => setHighlight(-1)}
                  >
                    <button
                      type="button"
                      className="subdir-pick"
                      onClick={() => {
                        setSubdir(o.value);
                        setOpen(false);
                      }}
                    >
                      {o.value}
                    </button>
                    {/* Only remembered entries can be forgotten: a directory
                        that exists on disk is not the launcher's to drop. */}
                    {o.remembered ? (
                      <button
                        type="button"
                        className="subdir-forget"
                        aria-label={`forget ${o.value}`}
                        onClick={() => forget(o.value)}
                      >
                        ×
                      </button>
                    ) : (
                      <span className="subdir-tag" aria-hidden="true">
                        dir
                      </span>
                    )}
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
