# Session rename — design

Date: 2026-07-26

## Goal

Let the user give a session a human-readable label, so a grid of tiles reads
`#3 · api refactor` instead of `#3 · claude` three times over.

## Scope decision: label only

The label is a display name held in the database. The tmux session name stays
`mm-{id}`, derived from the row ID.

`tmux_name` is load-bearing in three places — attach (`/ws/pty/{id}` resolves the
row, then the tmux name), `Reconcile` (membership test against `tmux ls`), and
the orphan-replace branch in `handleCreateSession` (which relies on the name
being derivable from the row ID before the tmux session exists). Renaming the
tmux session would put all three behind a name that can change under them, and
would open collisions with foreign tmux sessions. None of that buys anything the
user asked for.

## Storage

Append a migration (never edit a shipped entry):

```sql
ALTER TABLE sessions ADD COLUMN label TEXT NOT NULL DEFAULT ''
```

- `store.Session` gains `Label string \`json:"label,omitempty"\``.
- `sessionCols` and `scanSession` extend to carry it.
- New `SetSessionLabel(id int64, label string) error`, returning `ErrNotFound`
  when the update affects zero rows.

Empty string is the "no label" value — not NULL — so scanning needs no
`sql.NullString` and `omitempty` drops it from the wire.

## API

`PUT /api/sessions/{id}/label`, body `{"label": "..."}`.

Validation, in this order:

1. Trim leading/trailing whitespace.
2. Reject any control character (`unicode.IsControl`) → 400.
3. Reject over 64 runes (counted after trimming) → 400.

An empty label after trimming is valid and clears the label, reverting the tile
to the tool name.

Responses: 200 with the updated session JSON; 400 on a bad body or failed
validation; 404 on an unknown id. Dead sessions may be renamed — the tile stays
on screen until dismissed, so the label still matters.

On success the handler broadcasts `session_renamed` with the updated session.
`GridPage.onServerEvent` already refetches on any `session_*` event, so every
open tab and every tile showing that session updates with no new client wiring.

The route sits behind the same auth middleware as the rest of `/api`, so
bearer-token requests from a remote daemon's tile work unchanged.

## Frontend

- `Session` (`web/src/grid/types.ts`) gains `label?: string`.
- `toolName()` becomes `sessionTitle()`: the label when non-empty, otherwise the
  existing tool-name-with-tmux-name fallback. The tile title keeps its `#id · `
  prefix — the id is the handle used in logs, errors, and the attach dropdown.
- The tile title becomes a `TileTitle` component. Double-click swaps the text
  for an `<input>` with the current label preselected and `maxLength={64}`.
  Enter or blur saves; Escape cancels. While editing, the tile header sets
  `draggable={false}` and the double-click handler stops propagation, so tile
  drag and tap-to-move never fire from a rename.
- A failed PUT reverts the displayed text; the tile stays usable. No error toast
  — the app has no toast surface.
- No optimistic update: the PUT response and the broadcast drive the redisplay.
- `EmptyTile`'s attach dropdown shows the label when set, else `tmuxName`.
- The "server removed" placeholder tile keeps `#id · server removed`; there is
  no session record to label.

### Known limitation

Double-click has no touch equivalent, so rename is unreachable on a tablet. The
grid's other touch-hostile affordance (drag-to-move) has a tap-based fallback;
rename deliberately does not. Accepted for this iteration.

## Testing

Test-first, in this order.

`internal/store/sessions_test.go`
- New session's label defaults to `""`.
- Set then read back; set then clear.
- `SetSessionLabel` on an unknown id returns `ErrNotFound`.
- A database created at the previous schema version opens and migrates.

`internal/server/sessions_test.go`
- Valid PUT → 200, and the label appears in `GET /api/sessions`.
- 64 runes accepted, 65 rejected with 400 (multi-byte runes, to pin rune
  counting rather than byte counting).
- Control character rejected with 400.
- Unknown id → 404.
- A successful rename broadcasts `session_renamed`.
- Renaming a dead session succeeds.

`web/src/__tests__/grid-page.test.tsx`
- Double-click the title → input appears with the current value.
- Enter → `PUT /api/sessions/3/label` with the typed body.
- Escape → no fetch, original title restored.
- A session with a label renders it in the tile title and in the attach
  dropdown.

`./verify.sh` must pass before the work is called done.
