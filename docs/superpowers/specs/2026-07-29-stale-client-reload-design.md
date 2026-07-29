# Stale client reload prompt

## Problem

The daemon and the browser tab are versioned independently. Rebuilding and
restarting the daemon leaves every open tab running the previously embedded
JavaScript, with no signal that it is stale. A user who adds a frontend feature,
rebuilds, restarts the daemon, and then cannot find the feature has no way to
tell that the tab — not the build — is at fault.

## Signal: the embedded frontend build, not the version string

`s.cfg.Version` is injected via `-ldflags` and is `"dev"` for every local
`go build`, so it cannot distinguish one local build from the next. A process
start ID would fire on every restart, including backend-only rebuilds that need
no reload.

The precise signal is the content of the embedded `index.html`. Vite emits
content-hashed asset filenames, so `index.html` changes exactly when the
frontend build changes, and not otherwise.

**Build ID** = first 12 hex characters of the SHA-256 of the embedded
`index.html`, computed once per process via `sync.Once` (the assets are
`//go:embed`, immutable for the process lifetime).

A bare checkout ships only `web/dist/.gitkeep`; there is no `index.html` to
hash. In that case the build ID is the empty string and is omitted from the
wire, and the client never prompts.

## Channel: the existing `hello` frame

`internal/server/events.go` already writes `{"type":"hello"}` as the first frame
on every `/ws/events` connection. It gains a field:

```json
{ "type": "hello", "build": "a1b2c3d4e5f6" }
```

A daemon restart drops the socket; `useEvents` reconnects with backoff and
receives a fresh hello. Detection is immediate, needs no polling, and adds no
endpoint. `/healthz` is unchanged.

Only the origin daemon's build matters. Remote daemons in a multi-host grid
serve API and WebSocket traffic only — their embedded assets never execute in
this tab — so their hello frames are ignored for this purpose.

## Client

`useEvents` gains an optional `onHello?: (build: string) => void`, fired from the
existing `onmessage` handler when `type === "hello"`. Existing GridPage call
sites are unaffected.

`App` mounts one `useEvents(localServer(), …)` for this purpose alone. It
records the first build seen in a ref; a later hello carrying a different,
non-empty build sets `staleBuild`. An empty build never triggers the prompt.

This costs one extra WebSocket to the local daemon. The alternative — threading
the build up from GridPage — would couple the grid to app-level chrome for no
functional gain.

## UI

A banner renders between `<header>` and `<main>`:

> multimux was updated · **Reload** ×

`Reload` calls `location.reload()`. There is no service worker (retired in
`web/src/retire-sw.ts`), so a plain reload picks up the new assets. Session
state lives in tmux, so a reload loses only scrollback.

`×` dismisses the banner. A subsequent hello with a build different from the one
that triggered the dismissed banner shows it again.

The banner is non-blocking and never steals keyboard focus from a terminal
pane. It is not rendered on the login, setup, or trust routes, which `App`
returns before reaching the main layout.

## Testing

Go (`internal/server/events_test.go`):

- the hello frame carries a `build` field
- the same `WebFS` yields the same build ID across two connections
- a different `index.html` yields a different build ID
- a `WebFS` without `index.html` omits the `build` key

Web:

- `useEvents` invokes `onHello` with the build from the hello frame
- `App` shows the banner after a hello with a changed build
- `App` does not show it on a same-build reconnect, nor on an empty build
- dismissing hides the banner; a later differing build re-shows it
- the reload button calls `location.reload`
