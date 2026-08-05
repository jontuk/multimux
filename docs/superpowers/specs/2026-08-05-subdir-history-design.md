# Subdir field: expand on focus, remember per directory

## Problem

The launcher's subdir input (`web/src/grid/HeaderLauncher.tsx`) is a fixed 5rem
box in the header row. Anything longer than about eight characters scrolls out
of view while it is being typed, so a path like `internal/server` cannot be
checked before launch. It is also amnesiac: the same subdir is retyped for every
launch into the same directory.

Widening the resting field is not an option — the header already carries three
selects and the launch button, and the mobile layout has no room to give.

## Shape

Two changes, independent of each other:

1. The field stays narrow at rest and **overlays** the header while focused, so
   the typed value is fully visible and no other control moves.
2. Each directory keeps a short history of subdirs that were actually launched
   into. The history appears under the focused field, filters as you type, fills
   on click, and each entry can be deleted individually.

The history is per directory, not global: `web/src` means something under the
multimux repo and nothing under `~`.

## Storage: the daemon's SQLite database

The history lives server-side, in `multimux.db`, not in `localStorage`. A
directory is a daemon-side concept — `dir_id` is a per-daemon autoincrement —
so the history belongs with the row it describes, and it then follows the user
across browsers and devices hitting the same daemon.

Append a migration to `store.migrations` (append only — never edit a shipped
entry):

```sql
CREATE TABLE dir_subdirs (
  dir_id  INTEGER NOT NULL REFERENCES dirs(id) ON DELETE CASCADE,
  subdir  TEXT NOT NULL,
  used_at TEXT NOT NULL,
  PRIMARY KEY (dir_id, subdir)
);
```

`store.Open` already sets `_pragma=foreign_keys(1)`, so deleting a directory
drops its history with it — no cleanup code, no orphan rows.

`used_at` is RFC3339 UTC text, matching every other timestamp in the store.

The composite primary key makes re-launching a remembered subdir an upsert that
bumps `used_at` rather than a duplicate row.

### Store API (`internal/store/tools.go`)

| Function | Behaviour |
| --- | --- |
| `ListSubdirs(dirID int64) ([]string, error)` | `ORDER BY used_at DESC`, at most `subdirHistoryLimit` rows. Returns an empty slice, never nil. |
| `RecordSubdir(dirID int64, subdir string) error` | Upsert on `(dir_id, subdir)`, setting `used_at` to now. Then deletes rows outside the newest `subdirHistoryLimit` for that `dir_id`. |
| `DeleteSubdir(dirID int64, subdir string) error` | Exact-match delete. Deleting an absent entry is not an error. |

`subdirHistoryLimit = 10`. Ten is enough to cover the directories a person
actually cycles through, and short enough that the dropdown never needs its own
scrollbar.

`RecordSubdir` trims after inserting, in the same call, so the table cannot grow
without bound even if the pruning query and the insert race across two tabs.

## When an entry is recorded

In `handleCreateSession` (`internal/server/sessions.go`), **after**
`Tmux.CreateSession` returns successfully, and only when `in.Subdir` is
non-empty after trimming.

Recording late matters: `resolveSubdir` rejects subdirs that escape the base or
do not exist, and a tmux failure rolls the session row back. A subdir that never
produced a running session is a typo, and typos must not be offered back as
suggestions.

The recorded string is the client's trimmed input, not the resolved absolute
`workdir` — the history refills the same input field it came from.

A `RecordSubdir` error is logged and swallowed. The session exists and the
response is already a success; failing the launch over a history write would be
a strictly worse outcome for the user.

## HTTP surface

Two routes in `internal/server/server.go`, next to the existing `/api/dirs`
group:

```
GET    /api/dirs/{id}/subdirs                    -> 200 ["web/src","cmd"]
DELETE /api/dirs/{id}/subdirs?subdir=web%2Fsrc   -> 204
```

The subdir travels as a query parameter, not a path segment: subdirs contain
slashes, and `net/http`'s `{id}` wildcard matching does not span them.

Both sit behind the standard auth middleware — no bypass, nothing new in the
gate list. `GET` on an unknown `{id}` returns an empty array rather than 404;
the client's directory list and its history fetch can legitimately race a
directory deletion, and an empty history is the correct answer either way.
A non-numeric `{id}` is a malformed request: 400.

`DELETE` is idempotent — 204 whether or not the entry existed.

## Frontend

All changes are confined to `web/src/grid/HeaderLauncher.tsx` and the
`.header-launcher` rules in `web/src/index.css`.

### State

Three additions: `history: string[]`, `open: boolean` (dropdown visible), and
`highlight: number` (keyboard-selected row, `-1` for none).

`history` is fetched whenever `dirId` changes to a non-zero value, using the
same `stale` guard as the existing tools/dirs effect so a slow response for a
previously selected directory cannot land over a newer one.

### Clearing

Changing the directory clears `subdir` and `history` in the same handler that
sets `dirId`, then the effect refetches. `selectServer` already clears `subdir`;
it clears `history` too, for the same reason it clears everything else — a
directory id from the previous daemon means nothing on the new one.

### Layout

The input and its dropdown are wrapped in a `position: relative` container that
occupies the resting width in the header's flex row.

- Resting: input is **4rem** wide (down from 5rem), in normal flow.
- Focused: input becomes `position: absolute; width: 20rem; z-index: 20`, anchored
  to the wrapper's left edge, with the dropdown directly beneath it.

Because the wrapper keeps its 4rem footprint in both states, no other header
control moves when the field opens. Net resting width is 1rem smaller than
today.

On a narrow viewport the 20rem overlay is capped so it cannot exceed the header
width, and it right-aligns to the wrapper instead of the left when that would
push it off-screen.

### Dropdown

Visible when the field is focused and the filtered history is non-empty.

Filtering is a case-insensitive substring match on the typed value, so a
remembered `internal/server` is reachable by typing `serv`. An exact match is
still listed — the user may want to click it rather than finish typing.

Each row is a button showing the subdir, with a small `x` button at its right
edge. Clicking the row fills the input and keeps focus. Clicking the `x` removes
the row optimistically and fires the `DELETE`; a failed delete restores the row
and shows the existing `launcher-error` span.

The dropdown panel calls `preventDefault` on `mousedown`. Without it the input's
blur fires before the click, the panel unmounts, and neither the row nor the `x`
ever receives its click.

### Keyboard

| Key | Behaviour |
| --- | --- |
| ArrowDown / ArrowUp | Move `highlight` through the filtered rows; wraps. |
| Enter | With a highlighted row: fill the input from it, close the dropdown. Otherwise: launch, as today. |
| Escape | Close the dropdown and clear `highlight`; a second Escape blurs the field. |

Typing resets `highlight` to `-1`, so Enter after typing always launches. This
keeps the existing type-and-Enter muscle memory intact — the dropdown never
silently substitutes a suggestion for what was typed.

## Testing

**`internal/store/tools_test.go`**
- Record then list returns the value; recording the same subdir twice yields one
  row with a bumped `used_at` and most-recent-first ordering.
- Recording eleven distinct subdirs leaves the newest ten.
- Delete removes exactly one entry; deleting an absent entry succeeds.
- Deleting the parent directory removes its history (cascade).

**`internal/server/api_test.go` / `sessions_test.go`**
- `GET` returns `[]` for a directory with no history and for an unknown id;
  non-numeric id is 400.
- `DELETE` returns 204 and the entry is gone; repeat DELETE still 204.
- A successful launch with a subdir records it.
- A launch rejected by `resolveSubdir` (`..`, `missing`) records nothing.
- A launch with an empty subdir records nothing.
- Both routes 403 without auth.

**`web/src/__tests__/header-launcher.test.tsx`**
- Changing the directory clears a typed subdir and fetches the new directory's
  history.
- Focusing shows the history; typing filters it; a non-matching filter hides the
  dropdown.
- Clicking a row fills the input; launching then posts that subdir.
- Clicking `x` calls DELETE and removes the row.
- Enter with no highlighted row launches rather than selecting.

## Out of scope

- No filesystem completion of real subdirectories. The history is a record of
  what was launched, not a directory browser.
- No editing or reordering of history entries; the only mutations are "used it
  again" and "delete it".
- No settings surface for the history. Ten entries per directory, always on.
