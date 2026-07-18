# Operational Logging Design

## Goal

Make `multimux service logs` useful for understanding normal activity and
diagnosing failures. The daemon will expose request outcomes and meaningful
state transitions while keeping authentication material, terminal contents,
and request payloads out of the log.

## Scope

This change extends the existing `log/slog` output in two places:

1. HTTP middleware records application requests consistently.
2. Handlers and background reconciliation record successful domain events
   where route and status alone do not explain what changed.

This is operational logging, not a durable audit trail. Logs remain in the
service destinations already used by multimux: the launchd log file on macOS
and journald on Linux.

## Request Logging

The outer request middleware will emit one completion entry when either:

- the request path begins with `/api/` or `/ws/`; or
- the response status is 400 or greater.

This includes all API and WebSocket outcomes while suppressing successful
static assets and `/healthz` probes. A failed static or health request remains
visible.

Each request entry uses the message `http` and contains:

- `method`
- `path`, taken from `URL.Path` and never including the query string; route
  placeholders replace credential IDs and auth-session hashes
- `status`
- `duration`
- `error` for a 5xx JSON error response when one is available

Responses below 500 are logged at `Info`, including expected 4xx client
failures. Responses at or above 500 are logged at `Error`.

The response recorder will honor the first response status, as `net/http`
does. Its `Hijack` implementation will record status 101 before forwarding the
operation, allowing WebSocket requests to be identified as upgrades. A
WebSocket completion entry is written when its handler exits, so its duration
represents connection lifetime.

`writeJSON` will pass the `error` value from a 5xx `map[string]string`
response to the recorder through a narrow internal interface. It will not
buffer, parse, or log response bodies. Errors that are intentionally masked as
`internal` stay masked in the request entry; code that currently has the
underlying error will continue to log it explicitly when needed.

## Domain Events

Domain entries use short, stable messages and structured attributes. They are
written only after the corresponding operation succeeds.

The following state transitions are included:

- session created, killed, dismissed, or found dead by reconciliation;
- an orphaned tmux session replaced during creation;
- tool created, updated, or deleted;
- directory created or deleted;
- settings, appearance, or layout changed;
- initial passkey setup completed;
- login succeeded or logout completed;
- passkey registered or deleted;
- auth session revoked or cross-daemon bearer session minted.

Session events may include the database session ID, tmux name, tool ID, and
directory ID as applicable. Tool and directory events may include their
database ID and display name. Configuration events contain only the changed
setting names, not values. Authentication events contain the action and, where
already available without extra lookup, a user-supplied passkey display name;
they do not contain credential IDs or session hashes.

Startup reconciliation reports each session it changes. Periodic maintenance
does not log no-op ticks, avoiding an entry every five seconds. Existing
background and certificate errors remain at `Error`.

## Sensitive Data Rules

The implementation must never log:

- request or response bodies, apart from the single existing 5xx JSON `error`
  string passed directly to the recorder;
- URL query strings;
- terminal input or output;
- cookies, bearer tokens, authorization headers, or setup codes;
- WebAuthn credential material or credential IDs;
- authentication session hashes;
- user-agent strings;
- directory paths or setting values.

These exclusions apply even if future handlers add fields to their payloads.
Domain logs therefore select attributes explicitly rather than serializing
models or request objects.

## Error Handling

Logging is observational and must not change request results or domain
operations. No log write is allowed to become a new failure path. Existing
5xx responses retain their current status and body; this change only makes
their error string available to the request completion entry.

The existing explicit `Error` entries for background work remain. The request
entry supplies request context for handler failures without duplicating a
second handler-level error entry at every call site.

## Testing and Validation

Focused tests will verify:

- successful API requests appear at `Info` with method, path, status, and
  duration;
- query strings and sensitive request data do not appear;
- successful static and health requests are suppressed;
- 4xx responses on otherwise suppressed paths are recorded;
- 5xx API responses are emitted at `Error` with the available JSON error;
- WebSocket hijacking records status 101 without breaking upgrades;
- representative state mutations emit their safe domain event and fields;
- no-op reconciliation does not produce domain-event noise.

The full repository validation finishes with `./verify.sh`, covering Go
formatting, tests, lint checks, frontend checks, and builds.
