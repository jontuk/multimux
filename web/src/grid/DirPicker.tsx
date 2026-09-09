import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { getJSON } from "../api";
import type { Server } from "../servers";
import type { LaunchTarget, RecentDir } from "./useSessionLauncher";
import type { Dir } from "./types";

/**
 * A row is always a directory that could be launched into, and always one that
 * could be drilled into. What differs is where it came from: a `recent` row
 * carries launch history the user can forget, a `place` row is a configured
 * root, a `child` row is a subdirectory read off the daemon's disk.
 */
type Row = {
  kind: "recent" | "place" | "child";
  /** What the user reads, and what the filter matches against. */
  label: string;
  hint?: string;
  dirId: number;
  subdir: string;
  recent?: RecentDir;
};

const recentLimit = 8;

function joinSubdir(subdir: string, name: string): string {
  return subdir ? `${subdir}/${name}` : name;
}

function dropLast(subdir: string): string {
  const cut = subdir.lastIndexOf("/");
  return cut < 0 ? "" : subdir.slice(0, cut);
}

function fullPath(dirs: Dir[], at: LaunchTarget): string {
  const root = (dirs.find((d) => d.id === at.dirId)?.path ?? "").replace(/\/+$/, "");
  return at.subdir ? `${root}/${at.subdir}` : root;
}

/**
 * Where a new session goes, chosen by walking directories rather than by
 * typing a path. Configured roots are the entry points; everything below them
 * is read from the daemon on demand, so a couple of broad roots cover a whole
 * machine.
 *
 * The one rule the whole surface rests on: the row's name launches there, the
 * row's chevron descends into it. Drilling never launches, so a wrong turn
 * costs nothing — which is what lets a click be a launch anywhere else.
 */
export default function DirPicker({
  server,
  dirs,
  recents,
  start,
  busy,
  error,
  onForget,
  onLaunch,
  onClose,
  variant = "desktop",
}: {
  server: Server;
  dirs: Dir[];
  recents: RecentDir[];
  start: LaunchTarget | null;
  busy: boolean;
  error: string;
  onForget: (recent: RecentDir) => void;
  onLaunch: (dirId: number, subdir: string) => void;
  onClose: () => void;
  variant?: "desktop" | "mobile";
}) {
  const [at, setAt] = useState<LaunchTarget | null>(start);
  const [children, setChildren] = useState<string[]>([]);
  const [childrenOf, setChildrenOf] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [highlight, setHighlight] = useState(-1);
  const filterRef = useRef<HTMLInputElement>(null);

  // Desktop only: the dialog opens because a pointer asked it to, so focus
  // belongs in the filter. On mobile the same focus would raise the keyboard
  // over the very list the user came to tap.
  useEffect(() => {
    if (variant === "desktop") filterRef.current?.focus();
  }, [variant]);

  const here = at ? `${at.dirId}:${at.subdir}` : "";
  useEffect(() => {
    if (!at) return;
    let stale = false;
    const key = here;
    getJSON<string[]>(server, `/api/dirs/${at.dirId}/children?path=${encodeURIComponent(at.subdir)}`)
      .then((names) => {
        if (stale) return;
        setChildren(names);
        setChildrenOf(key);
      })
      .catch(() => {
        if (stale) return;
        setChildren([]);
        setChildrenOf(key);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [server.id, here]);

  function navigate(next: LaunchTarget | null) {
    setAt(next);
    setChildren([]);
    setChildrenOf(null);
    setFilter("");
    setHighlight(-1);
  }

  const groups = useMemo((): { title: string; rows: Row[] }[] => {
    if (at) {
      const rows: Row[] = children.map((name) => ({
        kind: "child",
        label: name,
        dirId: at.dirId,
        subdir: joinSubdir(at.subdir, name),
      }));
      return [{ title: "", rows }];
    }
    const recentRows: Row[] = recents.slice(0, recentLimit).map((recent) => ({
      kind: "recent",
      label: recent.path,
      dirId: recent.dirId,
      subdir: recent.subdir,
      recent,
    }));
    const placeRows: Row[] = dirs.map((dir) => ({
      kind: "place",
      label: dir.name,
      hint: dir.path,
      dirId: dir.id,
      subdir: "",
    }));
    return [
      ...(recentRows.length > 0 ? [{ title: "Recent", rows: recentRows }] : []),
      { title: "Places", rows: placeRows },
    ];
  }, [at, children, dirs, recents]);

  // Dotfile directories stay out of the way until the filter reaches for one,
  // the same bargain the old subdir typeahead struck.
  const needle = filter.trim().toLowerCase();
  const visible = groups
    .map((group) => ({
      title: group.title,
      rows: group.rows.filter(
        (row) =>
          (needle.startsWith(".") || !row.label.split("/").pop()?.startsWith(".")) &&
          (needle === "" ||
            row.label.toLowerCase().includes(needle) ||
            (row.hint ?? "").toLowerCase().includes(needle)),
      ),
    }))
    .filter((group) => group.rows.length > 0);
  const flat = visible.flatMap((group) => group.rows);
  const spot = highlight >= 0 && highlight < flat.length ? highlight : -1;
  const loadingChildren = at !== null && childrenOf !== here;

  // Crumbs are the way back up: the root's own name, then one per subdir
  // segment. "Places" returns to the landing view, which no crumb can express.
  const crumbs = at
    ? [
        { label: dirs.find((d) => d.id === at.dirId)?.name ?? "dir", target: { dirId: at.dirId, subdir: "" } },
        ...at.subdir
          .split("/")
          .filter(Boolean)
          .map((segment, index, all) => ({
            label: segment,
            target: { dirId: at.dirId, subdir: all.slice(0, index + 1).join("/") },
          })),
      ]
    : [];

  function move(step: -1 | 1) {
    if (flat.length === 0) return;
    setHighlight((current) =>
      current < 0 ? (step > 0 ? 0 : flat.length - 1) : (current + step + flat.length) % flat.length,
    );
  }

  function keyDown(event: React.KeyboardEvent) {
    if (event.key === "Escape") {
      event.stopPropagation();
      onClose();
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      move(event.key === "ArrowDown" ? 1 : -1);
    } else if (event.key === "ArrowRight") {
      if (spot < 0) return;
      event.preventDefault();
      navigate({ dirId: flat[spot].dirId, subdir: flat[spot].subdir });
    } else if (event.key === "ArrowLeft") {
      if (!at) return;
      event.preventDefault();
      navigate(at.subdir ? { dirId: at.dirId, subdir: dropLast(at.subdir) } : null);
    } else if (event.key === "Enter") {
      event.preventDefault();
      // No row picked out means the breadcrumb is the choice: the directory
      // just drilled into is a launch target in its own right.
      if (spot >= 0) onLaunch(flat[spot].dirId, flat[spot].subdir);
      else if (at) onLaunch(at.dirId, at.subdir);
    }
  }

  return (
    <section
      className={`dir-picker dir-picker-${variant}`}
      aria-label="Choose a directory"
      onKeyDown={keyDown}
      role={variant === "desktop" ? "dialog" : undefined}
      aria-modal={variant === "desktop" ? true : undefined}
    >
      <header className="dir-picker-head">
        <nav className="dir-picker-crumbs" aria-label="Directory path">
          <button type="button" className="dir-crumb" onClick={() => navigate(null)}>
            Places
          </button>
          {crumbs.map((crumb, index) => (
            <Fragment key={`${crumb.target.subdir}:${index}`}>
              {/* The separator is its own hidden element rather than a ::before
                  on the crumb: generated content lands in the accessible name,
                  and a crumb should be named for its directory alone. */}
              <span className="dir-crumb-sep" aria-hidden="true">
                /
              </span>
              <button
                type="button"
                className="dir-crumb"
                aria-current={index === crumbs.length - 1 ? "page" : undefined}
                onClick={() => navigate(crumb.target)}
              >
                {crumb.label}
              </button>
            </Fragment>
          ))}
        </nav>
        <button type="button" className="dir-picker-close" aria-label="Close directory picker" onClick={onClose}>
          ×
        </button>
      </header>

      <div className="dir-picker-list">
        {visible.map((group) => (
          <div key={group.title} className="dir-picker-group">
            {group.title && <h2>{group.title}</h2>}
            {group.rows.map((row) => {
              const index = flat.indexOf(row);
              const path = fullPath(dirs, row);
              return (
                <div
                  key={`${row.kind}:${row.dirId}:${row.subdir}`}
                  className={`dir-row${index === spot ? " on" : ""}`}
                  onMouseEnter={() => setHighlight(-1)}
                >
                  <button
                    type="button"
                    className="dir-row-launch"
                    disabled={busy}
                    // The visible text leads the accessible name rather than
                    // being replaced by it, so a screen reader still hears the
                    // name and voice control can act on it (WCAG 2.5.3).
                    aria-label={`${row.label} — launch in ${path}`}
                    title={`launch in ${path}`}
                    onClick={() => onLaunch(row.dirId, row.subdir)}
                  >
                    <span className="dir-row-name">{row.label}</span>
                    {row.hint && <span className="dir-row-hint">{row.hint}</span>}
                  </button>
                  {row.recent && (
                    <button
                      type="button"
                      className="dir-row-forget"
                      aria-label={`forget ${row.label}`}
                      title={`forget ${row.label}`}
                      onClick={() => onForget(row.recent!)}
                    >
                      ×
                    </button>
                  )}
                  <button
                    type="button"
                    className="dir-row-drill"
                    aria-label={`open ${row.label}`}
                    title={`open ${row.label}`}
                    onClick={() => navigate({ dirId: row.dirId, subdir: row.subdir })}
                  >
                    ›
                  </button>
                </div>
              );
            })}
          </div>
        ))}
        {visible.length === 0 && <p className="dir-picker-empty">{loadingChildren ? "loading…" : "nothing here"}</p>}
      </div>

      <footer className="dir-picker-foot">
        <input
          ref={filterRef}
          className="dir-picker-filter"
          aria-label="filter directories"
          placeholder="filter"
          value={filter}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          onChange={(event) => {
            setFilter(event.target.value);
            setHighlight(-1);
          }}
        />
        {at && (
          <button
            type="button"
            className="primary dir-picker-launch-here"
            disabled={busy}
            title={`launch in ${fullPath(dirs, at)}`}
            onClick={() => onLaunch(at.dirId, at.subdir)}
          >
            Launch here
          </button>
        )}
      </footer>
      {error && <p className="launcher-error">{error}</p>}
    </section>
  );
}
