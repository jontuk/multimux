import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { del, getJSON, postJSON } from "../api";
import type { Server } from "../servers";
import { splitUnderDir } from "./dirFilter";
import type { Dir, Session, Tool } from "./types";

const suggestionLimit = 12;

export interface SessionLaunchBatch {
  server: Server;
  sessions: Session[];
}

export interface SessionLauncherOption {
  value: string;
  remembered: boolean;
}

export interface SessionLauncherModel {
  server: Server | undefined;
  serverId: string;
  tools: Tool[];
  dirs: Dir[];
  toolId: number;
  dirId: number;
  subdir: string;
  loading: boolean;
  busy: boolean;
  error: string;
  unconfigured: "tools" | "dirs" | null;
  canLaunch: boolean;
  suggestionsOpen: boolean;
  highlighted: number;
  options: SessionLauncherOption[];
  showMenu: boolean;
  selectServer: (id: string) => void;
  selectTool: (id: number) => void;
  selectDir: (id: number) => void;
  changeSubdir: (value: string) => void;
  openSuggestions: () => void;
  closeSuggestions: () => void;
  clearHighlight: () => void;
  moveHighlight: (step: -1 | 1) => void;
  chooseOption: (index: number) => void;
  forget: (value: string) => Promise<void>;
  launch: () => Promise<SessionLaunchBatch | null>;
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
  const initial = servers.some((server) => server.id === initialServerId)
    ? initialServerId!
    : (servers[0]?.id ?? "");
  const [serverId, setServerId] = useState(initial);
  const [tools, setTools] = useState<Tool[]>([]);
  const [dirs, setDirs] = useState<Dir[]>([]);
  const [toolId, setToolId] = useState(0);
  const [dirId, setDirId] = useState(0);
  const [subdir, setSubdir] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const [children, setChildren] = useState<string[]>([]);
  const [childrenOf, setChildrenOf] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState(-1);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [applied, setApplied] = useState<{ target: string; dirs: Dir[] } | null>(null);
  const server = servers.find((candidate) => candidate.id === serverId);
  const selectionRef = useRef({ serverId, dirId });

  useLayoutEffect(() => {
    selectionRef.current = { serverId, dirId };
  }, [serverId, dirId]);

  function selectServer(id: string) {
    selectionRef.current = { serverId: id, dirId: 0 };
    setServerId(id);
    setTools([]);
    setDirs([]);
    setToolId(0);
    setDirId(0);
    setSubdir("");
    setHistory([]);
    setChildren([]);
    setChildrenOf(null);
    setOpen(false);
    setHighlight(-1);
    setError("");
    setLoading(true);
  }

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

  const cut = subdir.lastIndexOf("/");
  const typedParent = cut < 0 ? "" : subdir.slice(0, cut + 1);
  const typedLeaf = cut < 0 ? subdir : subdir.slice(cut + 1);

  useEffect(() => {
    if (!server) return;
    let stale = false;
    Promise.all([getJSON<Tool[]>(server, "/api/tools"), getJSON<Dir[]>(server, "/api/dirs")])
      .then(([nextTools, nextDirs]) => {
        if (stale) return;
        setTools(nextTools);
        setDirs(nextDirs);
        setToolId(nextTools[0]?.id ?? 0);
        const autoDirId = nextDirs[0]?.id ?? 0;
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

  const mine = targetServerId === null || targetServerId === serverId;
  if (targetDir !== null && mine && dirs.length > 0 && (applied?.target !== targetDir || applied.dirs !== dirs)) {
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

  useEffect(() => {
    if (!server || dirId <= 0) return;
    let stale = false;
    getJSON<string[]>(server, `/api/dirs/${dirId}/subdirs`)
      .then((nextHistory) => {
        if (!stale) setHistory(nextHistory);
      })
      .catch(() => {
        if (!stale) setHistory([]);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, dirId]);

  useEffect(() => {
    if (!server || dirId <= 0 || !open) return;
    let stale = false;
    const parent = typedParent;
    getJSON<string[]>(server, `/api/dirs/${dirId}/children?path=${encodeURIComponent(parent)}`)
      .then((nextChildren) => {
        if (stale) return;
        setChildren(nextChildren);
        setChildrenOf(parent);
      })
      .catch(() => {
        if (stale) return;
        setChildren([]);
        setChildrenOf(parent);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, dirId, typedParent, open]);

  const canLaunch = !loading && !busy && toolId > 0 && dirId > 0;
  const needle = subdir.trim().toLowerCase();
  const filtered = history.filter((item) => item.toLowerCase().includes(needle));
  const leaf = typedLeaf.toLowerCase();
  const suggestions =
    childrenOf === typedParent
      ? children
          .filter((name) => name.toLowerCase().startsWith(leaf))
          .filter((name) => leaf.startsWith(".") || !name.startsWith("."))
          .map((name) => typedParent + name)
          .filter((path) => path !== subdir.trim() && !filtered.includes(path))
          .slice(0, suggestionLimit)
      : [];
  const options = [
    ...filtered.map((value) => ({ value, remembered: true })),
    ...suggestions.map((value) => ({ value, remembered: false })),
  ];
  const showMenu = open && options.length > 0;

  async function forget(value: string) {
    if (!server) return;
    const issuedFor = selectionRef.current;
    const previous = history;
    setHistory((items) => items.filter((item) => item !== value));
    setHighlight(-1);
    try {
      await del(server, `/api/dirs/${issuedFor.dirId}/subdirs?subdir=${encodeURIComponent(value)}`);
    } catch (reason) {
      const current = selectionRef.current;
      if (current.serverId !== issuedFor.serverId || current.dirId !== issuedFor.dirId) return;
      setHistory(previous);
      setError(`couldn't forget ${value}: ${reason instanceof Error ? reason.message : reason}`);
    }
  }

  async function launch(): Promise<SessionLaunchBatch | null> {
    if (!server || !canLaunch) return null;
    const issuedFor = selectionRef.current;
    setBusy(true);
    setError("");
    try {
      const sessions = await postJSON<Session[]>(server, "/api/sessions", { toolId, dirId, subdir });
      const used = subdir.trim();
      const current = selectionRef.current;
      if (used && current.serverId === issuedFor.serverId && current.dirId === issuedFor.dirId) {
        setHistory((items) => [used, ...items.filter((item) => item !== used)]);
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
    dirId,
    subdir,
    loading,
    busy,
    error,
    unconfigured: !loading && !error && tools.length === 0 ? "tools" : !loading && !error && dirs.length === 0 ? "dirs" : null,
    canLaunch,
    suggestionsOpen: open,
    highlighted: highlight,
    options,
    showMenu,
    selectServer,
    selectTool(id) {
      setToolId(id);
      setError("");
    },
    selectDir,
    changeSubdir(value) {
      setSubdir(value);
      setOpen(true);
      setHighlight(-1);
      setError("");
    },
    openSuggestions() {
      setOpen(true);
    },
    closeSuggestions() {
      setOpen(false);
      setHighlight(-1);
    },
    clearHighlight() {
      setHighlight(-1);
    },
    moveHighlight(step) {
      if (options.length === 0) return;
      setOpen(true);
      setHighlight((current) =>
        current < 0 ? (step > 0 ? 0 : options.length - 1) : (current + step + options.length) % options.length,
      );
    },
    chooseOption(index) {
      if (index < 0 || index >= options.length) return;
      setSubdir(options[index].value);
      setHighlight(-1);
      setOpen(false);
    },
    forget,
    launch,
  };
}
