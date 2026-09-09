import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { del, getJSON, postJSON } from "../api";
import type { Server } from "../servers";
import { splitUnderDir } from "./dirFilter";
import type { Dir, Session, Tool } from "./types";

export interface SessionLaunchBatch {
  server: Server;
  sessions: Session[];
}

/**
 * A directory launched into before, resolved back to the configured root it
 * lives under. `path` is what the user reads; the pair is what a launch needs.
 */
export interface RecentDir {
  dirId: number;
  subdir: string;
  path: string;
}

/** Where the picker should open when the caller has a session in mind. */
export interface LaunchTarget {
  dirId: number;
  subdir: string;
}

export interface SessionLauncherModel {
  server: Server | undefined;
  serverId: string;
  tools: Tool[];
  dirs: Dir[];
  toolId: number;
  loading: boolean;
  busy: boolean;
  error: string;
  unconfigured: "tools" | "dirs" | null;
  canLaunch: boolean;
  recents: RecentDir[];
  /** The location the picker opens at, or null to open on the landing view. */
  start: LaunchTarget | null;
  selectServer: (id: string) => void;
  selectTool: (id: number) => void;
  forget: (recent: RecentDir) => Promise<void>;
  launch: (dirId: number, subdir: string) => Promise<SessionLaunchBatch | null>;
}

function joinPath(base: string, subdir: string): string {
  const root = base.replace(/\/+$/, "");
  return subdir ? `${root}/${subdir}` : root;
}

export function useSessionLauncher({
  servers,
  initialServerId,
  targetDir = null,
  targetServerId = null,
}: {
  servers: Server[];
  initialServerId?: string | null;
  targetDir?: string | null;
  targetServerId?: string | null;
}): SessionLauncherModel {
  const initial = servers.some((server) => server.id === initialServerId) ? initialServerId! : (servers[0]?.id ?? "");
  const [serverId, setServerId] = useState(initial);
  const [tools, setTools] = useState<Tool[]>([]);
  const [dirs, setDirs] = useState<Dir[]>([]);
  const [toolId, setToolId] = useState(0);
  // Per-root launch history, keyed by dir id. Merged into `recents` in the
  // roots' own order: the daemon records no timestamp a cross-root sort could
  // use, so each root keeps its own recency and the roots keep theirs.
  const [history, setHistory] = useState<Record<number, string[]>>({});
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const server = servers.find((candidate) => candidate.id === serverId);
  const serverRef = useRef(serverId);

  useLayoutEffect(() => {
    serverRef.current = serverId;
  }, [serverId]);

  function selectServer(id: string) {
    serverRef.current = id;
    setServerId(id);
    setTools([]);
    setDirs([]);
    setToolId(0);
    setHistory({});
    setError("");
    setLoading(true);
  }

  useEffect(() => {
    if (!server) return;
    let stale = false;
    Promise.all([getJSON<Tool[]>(server, "/api/tools"), getJSON<Dir[]>(server, "/api/dirs")])
      .then(([nextTools, nextDirs]) => {
        if (stale) return;
        setTools(nextTools);
        setDirs(nextDirs);
        setToolId(nextTools[0]?.id ?? 0);
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

  // One history request per configured root. Roots are few by design — the
  // picker exists so a couple of broad ones cover everything — so this stays a
  // handful of requests made once per server.
  const dirIds = dirs.map((dir) => dir.id).join(",");
  useEffect(() => {
    if (!server || dirs.length === 0) return;
    let stale = false;
    Promise.all(
      dirs.map((dir) =>
        getJSON<string[]>(server, `/api/dirs/${dir.id}/subdirs`)
          .then((subdirs) => [dir.id, subdirs] as const)
          .catch(() => [dir.id, [] as string[]] as const),
      ),
    ).then((pairs) => {
      if (stale) return;
      setHistory(Object.fromEntries(pairs));
    });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, dirIds]);

  const recents: RecentDir[] = dirs.flatMap((dir) =>
    (history[dir.id] ?? []).map((subdir) => ({ dirId: dir.id, subdir, path: joinPath(dir.path, subdir) })),
  );

  // A caller with a session in mind (the tile that asked for a sibling) opens
  // the picker in that session's directory. No match — another daemon's path,
  // or a root since removed — leaves the picker on its landing view.
  const mine = targetServerId === null || targetServerId === serverId;
  const start = targetDir !== null && mine && dirs.length > 0 ? splitUnderDir(dirs, targetDir) : null;

  async function forget(recent: RecentDir) {
    if (!server) return;
    const issuedFor = serverRef.current;
    const previous = history[recent.dirId] ?? [];
    setHistory((all) => ({ ...all, [recent.dirId]: previous.filter((item) => item !== recent.subdir) }));
    try {
      await del(server, `/api/dirs/${recent.dirId}/subdirs?subdir=${encodeURIComponent(recent.subdir)}`);
    } catch (reason) {
      if (serverRef.current !== issuedFor) return;
      setHistory((all) => ({ ...all, [recent.dirId]: previous }));
      setError(`couldn't forget ${recent.path}: ${reason instanceof Error ? reason.message : reason}`);
    }
  }

  const canLaunch = !loading && !busy && toolId > 0 && dirs.length > 0;

  async function launch(dirId: number, subdir: string): Promise<SessionLaunchBatch | null> {
    if (!server || !canLaunch || dirId <= 0) return null;
    const issuedFor = serverRef.current;
    setBusy(true);
    setError("");
    try {
      const sessions = await postJSON<Session[]>(server, "/api/sessions", { toolId, dirId, subdir });
      const used = subdir.trim();
      if (used && serverRef.current === issuedFor) {
        setHistory((all) => ({ ...all, [dirId]: [used, ...(all[dirId] ?? []).filter((item) => item !== used)] }));
      }
      return { server, sessions };
    } catch (reason) {
      setError(`launch failed: ${reason instanceof Error ? reason.message : reason}`);
      return null;
    } finally {
      setBusy(false);
    }
  }

  return {
    server,
    serverId,
    tools,
    dirs,
    toolId,
    loading,
    busy,
    error,
    unconfigured:
      !loading && !error && tools.length === 0 ? "tools" : !loading && !error && dirs.length === 0 ? "dirs" : null,
    canLaunch,
    recents,
    start,
    selectServer,
    selectTool(id) {
      setToolId(id);
      setError("");
    },
    forget,
    launch,
  };
}
