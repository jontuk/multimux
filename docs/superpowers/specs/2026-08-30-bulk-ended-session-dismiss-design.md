# Bulk ended-session dismissal

## Problem

A machine reboot destroys its tmux server and therefore every multimux session
on that machine. The multimux database and the browser's persisted grid layout
survive. When the daemon starts again, its existing reconciliation pass marks
the former sessions dead, but every corresponding grid tile remains until the
user dismisses it individually.

The client should make this expected recovery case a single action without
silently hiding what happened or affecting sessions on another server.

## Behaviour

After the client has successfully loaded a server's session list, it identifies
placed tiles on that server which no longer represent a running session. A tile
is ended when either:

- its session exists in the response with a status other than `running`; or
- its session ID is absent from the authoritative response.

If at least one such tile exists, GridPage shows one server-scoped notice near
the existing server status notices:

> **work-server**: 8 sessions ended · **Dismiss all**

The exact singular/plural form follows the count. The wording stays generic:
the same state can result from a reboot, a crash, an externally killed tmux
session, or a session record dismissed elsewhere.

Selecting **Dismiss all** removes every ended tile for that server in one
layout edit. It does not remove running tiles, affect another server, terminate
tmux sessions, or call a session mutation API. It needs no confirmation because
it is the bulk form of the existing non-destructive tile dismissal.

The notice disappears when none of that server's placed tiles are ended. If a
later successful refresh finds more ended tiles, it appears again.

## Client state and data flow

GridPage already owns the configured servers, per-server session lists, and the
persisted layout. It gains per-server knowledge of whether at least one session
request has completed successfully. A failed request must continue to preserve
the last known session list, as it does today, but it must not make an absent
session authoritative.

For each configured server whose list has loaded successfully:

1. Read that server's placed tiles from the current layout.
2. Match each tile against the server's current session response.
3. Collect tile keys whose session is dead or absent.
4. Render one notice with the size of that set.
5. On dismissal, call the existing layout persistence path once with
   `removeTilesWhere`, matching the collected keys.

Using tile keys rather than layout indices is important because removing one
tile repacks the grid and invalidates subsequent indices. The existing
`removeTilesWhere` helper already performs a stable batch removal and one
normalization pass.

The derived ended-tile sets are recalculated from the latest layout and session
state. They are not separately persisted. Consequently the behaviour also
works when a tab is opened only after the reboot, not merely when a live tab
observes the disconnect and reconnect.

## UI placement and accessibility

Recovery notices render in GridPage alongside the existing per-server
connection-status notices, before the desktop or mobile session view. If a
server currently has any non-open connection status, its recovery notice is
suppressed; the connection-status notice remains the only action context until
the server reconnects. Cleanup is offered only after an authoritative session
response, so initial loading cannot make an unknown tile look ended.

The action is a real button. Its accessible name includes the count and server
name, for example `Dismiss all 8 ended sessions on work-server`. A single ended
session uses singular visible and accessible text.

No terminal receives focus as a side effect. If a dismissed tile was maximized,
the existing `adoptLayout` behaviour clears maximization when that key leaves
the layout.

## Server and storage impact

There is no new endpoint, reboot identifier, database migration, or server-side
state. The daemon's existing startup reconciliation remains the source of dead
session status. Dead database rows retain their current lifecycle; this feature
addresses the stale browser layout the user must currently clean up one tile at
a time.

## Error handling

- Loading and unreachable states never classify unknown tiles as ended.
- A non-open server suppresses its recovery notice; reconnecting reveals the
  same derived cleanup set if those ended tiles still exist.
- A failed session refresh preserves the last successful list and does not
  broaden the cleanup set.
- Layout persistence uses the existing coalesced write path. The UI applies the
  same optimistic behaviour as an individual tile dismissal.
- A server removed from the configured server list retains the existing
  per-tile `server removed` handling and is not included in this feature.

## Testing

GridPage tests cover:

- two dead tiles on one server produce one notice with the correct count;
- dismissing the notice removes both dead tiles with one layout write while a
  running tile on the same server remains;
- ended tiles on another server remain and have their own notice;
- a missing session is included after a successful authoritative response;
- missing sessions are not inferred before the first successful response or
  from a failed refresh;
- the singular message and accessible action name are correct; and
- dismissing a maximized ended tile returns to the ordinary grid.

Existing model tests continue to cover batch removal and grid normalization.

## Out of scope

- preserving tmux sessions across a machine reboot;
- automatically dismissing ended tiles without user acknowledgement;
- a pre-reboot command or UI action;
- detecting the operating system's boot identity; and
- bulk deletion of dead session rows from the daemon database.
