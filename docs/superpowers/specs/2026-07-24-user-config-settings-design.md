# User-configurable settings

**Date:** 2026-07-24
**Status:** Approved

## Goal

Give multimux a set of user-configurable settings that persist across restarts,
are editable from both the CLI (`multimux config`) and the web Settings page,
and are easy to extend as new settings appear.

The first setting is `confirm-terminate`: whether the browser asks for
confirmation before terminating a session. It defaults to `false` (no
confirmation), which changes today's always-confirm behaviour — see
"Behaviour change" below.

## Storage

Settings live in the existing SQLite `settings` key/value table
(`internal/store`). No new file format and no migration: the table has shipped
since the first migration, and `GetSetting`/`SetSetting`/`SetSettings` already
exist.

Both the daemon and the CLI open the same database. `multimux config` opens
`dataDir()/multimux.db` directly, the way `multimux auth reset` already does
(`cmd/auth.go`). SQLite handles the two processes.

## Naming

One setting, three spellings, converted at the boundaries:

| Context | Spelling |
| --- | --- |
| CLI key | `confirm-terminate` |
| `settings` table key | `confirm_terminate` |
| JSON field | `confirmTerminate` |

## Component 1: `internal/config`

A new package that is the single definition of every user-configurable setting,
so the CLI, the HTTP API, and the web UI cannot disagree about defaults or
validation.

```go
type Kind int

const KindBool Kind = iota

type Key struct {
    Name    string // CLI name, e.g. "confirm-terminate"
    Kind    Kind
    Default string
    Help    string
}

var Keys = []Key{{
    Name:    "confirm-terminate",
    Kind:    KindBool,
    Default: "false",
    Help:    "ask for confirmation before terminating a session",
}}
```

API:

- `Lookup(name string) (Key, bool)` — find a key by its CLI name.
- `Normalize(k Key, raw string) (string, error)` — validate and canonicalise a
  value. For `KindBool`, accept `true` and `false` only, rejecting anything
  else with an error naming the accepted values.
- `Get(st *store.Store, name string) (string, error)` — the effective value:
  the stored row, or the key's `Default` when no row exists.
- `Set(st *store.Store, name, value string) error` — normalise, then write.
- `Bool(st *store.Store, name string) (bool, error)` — typed convenience for
  callers that want a `bool`.

Unknown names return an error from every entry point, so a typo never silently
writes an orphan row.

Dependencies: `config` imports `store` and nothing else in the project. `cmd`
and `internal/server` import `config`. Nothing imports `cmd`.

## Component 2: `multimux config` CLI

New file `cmd/config.go`, modelled on `cmd/auth.go`.

```
usage: multimux config <list|get|set> [key] [value]

  list             print every setting, its effective value, and whether it is
                   still at its default
  get <key>        print the effective value of one setting
  set <key> <val>  set a value
```

Behaviour:

- `list` iterates `config.Keys` and prints an aligned table: name, effective
  value, and `(default)` when no row is stored. Exit 0.
- `get` prints the bare value with no decoration, so it is usable in scripts.
  Unknown key writes to stderr and exits 2.
- `set` normalises the value, writes it, and echoes `key = value`. It then
  prints one line noting that open browser tabs pick the change up on reload.
  Unknown key or invalid value writes to stderr and exits 2.
- Any other subcommand, or a missing argument, prints `configUsage` to stderr
  and exits 2.
- Store or write failures print the error and exit 1.

Wiring: a `case "config"` in `Execute`'s switch, a `config` line in the top
level `usage` string, and a `configUsage` case in `helpFor`.

## Component 3: HTTP API

Two handlers in `internal/server/api.go`, registered in `server.go` beside the
existing appearance routes and behind the same authentication:

- `GET /api/settings/preferences` → `{"confirmTerminate": false}`
- `PUT /api/settings/preferences` ← the same shape

`PUT` decodes into a struct, converts each field through `config.Set`, and
returns 400 on a malformed body. It responds with the stored state so the
client can reconcile.

The handlers read and write through `internal/config`, never touching
`store.GetSetting` directly, so defaults stay in one place.

## Component 4: web UI

`web/src/settings/PreferencesPanel.tsx`, modelled directly on
`AppearancePanel.tsx`:

- `useFetch<Preferences>("/api/settings/preferences", seed)` with `PanelState`
  for the loading and error states.
- A single checkbox for "Ask before terminating a session", plus a save button
  that `PUT`s and reloads.
- On a successful save it dispatches a `multimux:preferences` CustomEvent
  carrying the new value — the same mechanism `APPEARANCE_EVENT` uses — so an
  open grid updates without a reload.

`SettingsPage.tsx` renders the new panel alongside the existing ones.

`App.tsx` fetches `/api/settings/preferences` at startup, holds
`confirmTerminate` in state, listens for `multimux:preferences`, and passes the
value down to `GridPage`.

`GridPage.tsx` takes `confirmTerminate` as a prop. Its `terminateSession`
guard becomes:

```ts
if (confirmTerminate && !window.confirm(`Terminate session #${sessionId}?`)) return;
```

The existing `window.confirm` stays; this design does not introduce a custom
dialog component.

## Behaviour change

Today the browser always confirms before terminating. With `confirm-terminate`
defaulting to `false`, the confirmation disappears for everyone on upgrade
unless they turn it on. This is the intended default.

## Data flow

1. `multimux config set confirm-terminate true` → `config.Set` → `settings`
   row. A daemon serving requests reads the row on the next `GET`, so no
   restart is needed; open tabs pick it up on their next load.
2. Web save → `PUT /api/settings/preferences` → `config.Set` → same row, and
   the CustomEvent updates the current tab immediately.

The CLI runs in a separate process from the daemon and so cannot push a live
event to open tabs. That is accepted, and the `set` output says so.

## Error handling

- Unknown setting name: error naming the setting, from CLI (exit 2) and API
  (400).
- Invalid value: error naming the accepted values, from CLI (exit 2) and API
  (400).
- Missing row: not an error — `Get` returns the key's default.
- Database open or write failure: CLI prints the error and exits 1; API returns
  500 through the server's existing error path.

## Testing

- `internal/config`: table tests over defaults for an unset key, `Lookup` of an
  unknown name, bool parsing and rejection, and a `Set`/`Get` round trip
  against a temporary store.
- `cmd/config_test.go`, mirroring `cmd/auth_test.go`: `list` output against a
  temporary data directory, `get` after `set`, and exit codes for an unknown
  key, an invalid value, and a missing argument.
- `internal/server/api_test.go`: `GET` returns the default, `PUT` then `GET`
  round trips, and `PUT` with a bad body returns 400.
- `web/src/__tests__/preferences.test.tsx`, mirroring `appearance.test.tsx`:
  the panel renders the fetched value, saves, and dispatches the event.
- `web/src/__tests__/grid-page.test.tsx`: terminating skips `window.confirm`
  when the setting is off and calls it when on.

## Documentation

The README gains a short `multimux config` section under the existing CLI
material, listing the subcommands and the `confirm-terminate` setting.

`./verify.sh` covers formatting, tests, and the build.

## Out of scope

- A config file on disk. Settings live in SQLite; if hand-editing is wanted
  later it is a separate change.
- Non-boolean setting kinds. `Kind` exists so they can be added, but only
  `KindBool` is implemented.
- Live push of CLI-originated changes to open browser tabs.
