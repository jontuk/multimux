# Pane Text Reader Design

## Problem

Multimux currently copies a browser selection by reconstructing text from the
xterm buffer. tmux paints rows with cursor positioning rather than preserving
enough wrap metadata for xterm to distinguish every soft wrap from a genuine
newline. The current last-cell heuristic therefore has unavoidable false
positives and false negatives: it can insert newlines into one logical line or
preserve padding and join rows that should remain separate.

The primary use case is copying arbitrary output from an ongoing interactive
session, including tools such as Codex and Claude Code. A tool-specific
conversation export is not appropriate: the selected material may be any pane
output, and the foreground program must continue running while the user copies.

## Decision

Add a frozen **Pane Text** reader backed by tmux's own pane capture. It is the
reliable logical-copy path. Existing direct terminal selection remains
available for quick visual copying, but this feature does not attempt to make
the browser's selected screen coordinates authoritative.

The reader captures the active pane's available history and current screen at
one instant, asks tmux to join soft-wrapped rows, and presents the result as
ordinary selectable web text. It is a pane snapshot, not a semantic
conversation transcript and not a record of terminal frames that an
interactive program has already erased by redrawing in place.

## User Experience

Every running session exposes a **Text** action:

- On desktop it appears in the tile header before the remove and terminate
  actions.
- On mobile it appears in the selected session's header controls.
- It is absent when no running session is available.

Activating **Text** opens a large modal reader over the application. The live
`TerminalTile` stays mounted behind it, so its PTY WebSocket remains connected
and the foreground program continues running. The modal takes keyboard focus,
preventing ordinary keystrokes from reaching the terminal while it is open.

The reader header shows the session identity and these actions:

- **Refresh** requests a new complete snapshot.
- **Copy all** writes the complete snapshot to the browser clipboard.
- **Close** returns to the live terminal.

The body is normal selectable text. It opens scrolled to the newest output,
supports normal pointer selection, Cmd/Ctrl+C, scrolling, and browser Find.
Long logical lines may wrap visually for readability, but CSS wrapping does not
insert characters into the selected or copied string.

The snapshot never updates automatically. Output cannot move beneath an active
selection. Refresh deliberately replaces it with a newer snapshot and scrolls
to the new bottom. Escape closes the reader, and closing restores focus to the
trigger that opened it.

On mobile the modal occupies the usable viewport and respects existing safe
areas. It uses the same reader component and behavior as desktop rather than a
separate mobile copy implementation.

## Capture Boundary

The snapshot contains the active pane of the tmux session, from the beginning
of its retained history through the end of its current visible content, in
oldest-to-newest order. For a window with splits, it does not concatenate the
screens of every pane. The active pane is the only unambiguous pane target
available from a session-level **Text** action.

tmux remains the authority for logical line boundaries. The manager invokes
the equivalent of:

```sh
tmux capture-pane -pJ -S - -E - -t '=session-name:'
```

using Multimux's existing private-socket and UTF-8 arguments. `-J` joins rows
that tmux marks as wrapped and trims unused terminal cells while preserving
real line boundaries and meaningful spaces. The capture does not request
formatting escapes, so the result is plain text.

This boundary has explicit limitations:

- It cannot recover content that a full-screen or interactive application
  overwrote without allowing it to enter tmux history.
- It reflects tmux's retained pane state, not an application's message model.
- It captures only the active pane, even when the window contains splits.
- It does not pause the foreground process, enter copy mode, or resize the
  window.

## Backend

Add an authenticated read-only endpoint:

```http
GET /api/sessions/{id}/text
```

The handler performs these steps:

1. Parse the session ID and load the exact database session.
2. Reject a session whose stored status is not `running`.
3. Call a focused `tmuxmgr.Manager` capture method with the session's canonical
   tmux name.
4. Return the captured bytes as `text/plain; charset=utf-8`.

The response always includes `Cache-Control: no-store`. Captured text is never
written to logs, the database, or a temporary file. The endpoint does not add
an application-level truncation: the tmux panes already use Multimux's fixed
50,000-physical-line history limit, and arbitrary older output must remain
available when it is still retained by tmux.

The manager method captures stdout and stderr separately. A missing tmux
session or server is classified with a sentinel error so the HTTP layer does
not inspect error strings. Other command failures retain enough context for
diagnostic logging without including captured stdout.

The endpoint returns:

- `400` for a malformed ID;
- `404` when the database session does not exist;
- `409` when the stored session has ended or its tmux session disappeared
  during the request;
- `500` for another tmux capture or server failure.

## Frontend Architecture

Add a shared `PaneTextReader` component whose public inputs are the server,
session ID, display title, open state, close callback, and trigger element for
focus restoration. Desktop `GridPage` and `MobileSessionView` own only the
small amount of state needed to choose the reader target and open it.

The component is rendered as a modal above the application, not in place of a
terminal tile. This keeps React from unmounting or reconnecting `TerminalTile`.
It uses the existing authenticated `apiFetch` path so local cookie sessions and
remote bearer-token servers behave identically. A small text-response helper
may share the existing `ApiError` conversion rather than duplicating request
error handling.

The body renders the response through React text content in a preformatted
container. It never uses `innerHTML` and does not interpret terminal output as
markup. The container uses visual wrapping while preserving the response's
underlying whitespace and newlines.

The dialog has an accessible name, modal semantics, an initial focus target,
Escape handling, and focus containment. Closing aborts any outstanding request,
discards the in-memory snapshot, and restores focus to the opening **Text**
button.

## Loading and Error States

Opening the reader displays the modal immediately with a loading state while
the initial capture is fetched.

An initial failure replaces the loading state with the existing short API
error wording plus **Retry** and **Close**. During a refresh, the previous
snapshot remains selectable while a small progress indicator is visible. A
refresh failure leaves that snapshot in place and displays the error without
closing the reader.

Every request receives a monotonically increasing generation. Only the latest
generation may replace the current snapshot, so a slow older refresh cannot
overwrite a newer one. Closing aborts the active fetch and makes all of its
results inert.

If the session ends after a successful capture, the existing snapshot remains
usable. A later refresh reports that the session is no longer available.

**Copy all** calls the Clipboard API from its click gesture and announces a
short success status. If the API is absent or rejects the write, the snapshot
stays visible and the reader instructs the user to select and copy manually.
An empty snapshot is a valid result; **Copy all** is disabled until loading has
completed and while there is no text to copy.

## Security and Privacy

Pane output can contain secrets. The design therefore requires all of the
following:

- normal Multimux authentication and cross-daemon token handling;
- `Cache-Control: no-store` on the text endpoint;
- no logging or persistence of captured content;
- no temporary capture files;
- text-only rendering with no HTML interpretation;
- snapshot state discarded when the modal closes.

The feature does not introduce a new authority boundary: an authenticated
client that can attach to a session can already read the same terminal output.

## Testing

### tmux manager

Use a private tmux socket to create a narrow pane containing both a genuine
newline and a long soft-wrapped logical line. Assert that capture keeps the
former and joins the latter. Cover exact session targeting, active-pane
selection in a split window, UTF-8 text, empty output, missing sessions, and
ordinary command failures.

### HTTP server

Cover successful plain-text output, the UTF-8 content type, `no-store`, auth,
malformed and missing IDs, stored ended sessions, a tmux disappearance race,
and an internal capture failure. Test doubles must assert that the database
tmux name, not a client-supplied name, reaches the manager.

### Web client

Cover desktop and mobile **Text** triggers, initial loading, successful text
rendering, initial retry, refresh with the old snapshot retained, refresh
failure, stale-response rejection, Copy all success and failure, empty output,
Escape, modal focus behavior, focus restoration, and scrolling to the newest
output after initial load and refresh.

An integration-oriented component test records terminal construction and
WebSocket attachment counts and verifies that opening, refreshing, and closing
the reader never unmounts or reconnects the live terminal.

## Out of Scope

- Replacing or further tuning the existing direct-drag wrap heuristic.
- Mapping a browser terminal selection back to tmux pane coordinates.
- Tool-specific Codex, Claude Code, or shell conversation exports.
- Recovering terminal frames that are no longer in tmux's grid or history.
- Combining multiple split panes into one textual document.
- Live or automatic reader refresh.
- Persisting, downloading, or sharing pane snapshots.
