# Directory filter buttons

## Goal

A row of buttons in the grid header, one per working directory currently in
use by a running session. Clicking a button hides every session in that
directory — its grid tiles and its quick-add ("minimised") buttons alike.
Clicking again brings them back. Each button carries the same tint the
matching tile headers use, so the mapping between button and tiles is visible
rather than remembered.

The quick-add buttons move to the right of the new filter buttons.

## Scope

Desktop grid view only. `MobileSessionView` keeps showing every session:
header controls are not rendered on narrow screens, so a filter there could
hide a session with no control to bring it back.

The filter is a view, not state the daemon knows about. Nothing is persisted
server-side and the stored layout is never rewritten by hiding or showing.

## Data

New module `web/src/grid/dirFilter.ts`, holding the pure parts and the
storage:

- `hiddenDirs(): Set<string>` / `setHiddenDirs(set: Set<string>)` — backed by
  `localStorage` under `multimux.hiddenDirs`, an array of full directory
  paths. Browser-local, in the spirit of `web/src/servers.ts`. A directory
  absent from the list is visible, so a directory seen for the first time
  shows by default. Malformed or unparsable storage reads as empty.
- `dirButtons(servers, sessionsByServer)` — one entry `{ path, name, count }`
  per distinct directory of a *running* session on any server, `name` being
  the path's last segment and `count` the number of running sessions in it
  (placed in a tile or not). Sorted by `name`, then `path`, so two
  directories sharing a leaf name have a stable order.
- `filterLayout(layout, isVisible)` → `{ view: Layout, map: number[] }`.
  `view` is `normalize(visibleTiles, layout.shape.cols, …)`, reusing the
  canonical packing rather than inventing a second one, so hidden tiles leave
  no gaps and the row count shrinks to fit. `map[viewIndex]` is the index the
  same tile occupies in the real layout.
- Visibility is decided by the session's directory whatever its status, so a
  tile holding an ended session hides and shows with the rest of its
  directory. Only the *buttons* are limited to running sessions: an ended
  session whose directory has no other running session keeps no button, and
  so stays visible until dismissed.
- A tile whose session is unknown — the server was removed, or the session is
  not in `sessionsByServer` yet — counts as visible. It cannot be classified
  by directory, and hiding it would strand it with no button to restore it.

## Rendering

`GridPage.tsx`, desktop branch only.

Header controls become, left to right: `HeaderLauncher`, `ColumnStepper`,
`DirFilterBar`, the unplaced-session buttons.

With nothing hidden, behaviour is unchanged: `layout` renders directly and
tile indices are real indices. With something hidden, `view` renders and every
mutation translates through `map` before touching the stored layout —
`removeTile(map[i])`, `swapTiles(map[from], map[to])`. Terminating still
targets the session id from the tile, which the translation preserves.

Hidden tiles unmount, so their PTY WebSockets close; re-showing remounts and
reattaches, with scrollback restored from tmux as on any reconnect.

The unplaced-session buttons are filtered by the same predicate.

Launching a session into a hidden directory unhides that directory. Otherwise
the launch would appear to do nothing.

## Splitters while filtered

Both axes stay draggable while a filter is active, and *all* size changes are
view-local for the duration — rows and columns both.

Rows cannot round-trip because the filtered grid has fewer of them. Columns
cannot either, less obviously: `colSizes` is one width array per row *indexed
by row*, and filtering shifts row indices, so persisting a filtered column
drag would apply those widths to an unrelated row of the stored layout.

So while any directory is hidden, `GridDividers`' `onPreview` and `onCommit`
both write to an in-memory `viewSizes` state instead of persisting. That state
resets whenever the hidden set changes, and clearing the filter restores the
stored `rowSizes`/`colSizes` untouched.

## Appearance

`.dir-filter button` takes the tile header's own background formula:

```css
background: color-mix(in srgb, var(--dir-tint) var(--dir-tint-strength), var(--panel));
```

with `--dir-tint` set per button from `dirTintStyle(path)` (`grid/dirColor.ts`),
which is what the tile headers already use. Content is the leaf name and the
count, in the mono face at the header's size; the full path is the `title`.
State is carried by `aria-pressed`, with the hidden state drawn as reduced
opacity plus a dashed border so it reads as off without relying on colour.

## Testing

Vitest over the pure surface, written before the implementation:

- `dirButtons`: grouping by full path, counting, leaf naming, exclusion of
  non-running sessions, sort order for a shared leaf name.
- `filterLayout`: repacking, row-count shrink, the index map, the
  unknown-session case, and the identity case when nothing is hidden.
- Storage: round-trip, absent key, corrupt JSON.

Then `./verify.sh`, which covers lint, the web build, and the Go side.
