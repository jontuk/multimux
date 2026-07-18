# TODO — codebase audit

Audit date: 2026-07-18

Audited revision: `e6b1547` (`main`)

This replaces the completed hostname/dev-loop checklist. Its history remains in
Git. Items below are ordered by user impact, exploitability, and likelihood.

## Baseline

- `./verify.sh`: pass (Go format/vet/tests; frontend lint, 43 Vitest tests, and
  production build).
- `go test -race ./...`: pass.
- `go test -cover ./...`: pass. Excluding the root executable package (0.0%),
  the lowest package coverage is `internal/svc` (32.8%); `cmd` is 41.6%.
- `go build -o /tmp/multimux-audit .`: pass.
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` (scanner v1.6.0,
  database updated 2026-07-08): no reachable vulnerabilities. One vulnerability
  is present in the module graph, but no affected symbol is called.
- `pnpm audit --prod --audit-level high`: no known production dependency
  vulnerabilities.
- The frontend build emits one 561.94 kB JavaScript bundle (155.39 kB gzip).

## Priority key

- **P0:** security, data-loss, or core-product blocker; fix before the next
  release.
- **P1:** high-impact correctness/reliability issue; fix next.
- **P2:** meaningful efficiency, resilience, or usability improvement.
- **P3:** engineering hygiene or optional product polish.

## P0 — release blockers

- [ ] **[security] Block same-site CSRF on cookie-authenticated API writes.**
  The API reflects `Access-Control-Allow-Origin: *` but never validates
  `Origin`, and its JSON decoder accepts a CORS-safelisted `text/plain` body
  ([server.go](internal/server/server.go#L149),
  [server.go](internal/server/server.go#L214)). A hostile sibling origin on the
  same site can therefore send the victim's `SameSite=Strict` cookie and create
  an arbitrary tool/session; CORS prevents reading the response, not executing
  the mutation ([api.go](internal/server/api.go#L28),
  [sessions.go](internal/server/sessions.go#L25)). This follows the browser
  rules for [CORS-safelisted request content
  types](https://fetch.spec.whatwg.org/#cors-safelisted-request-header) and
  [same-site cookies](https://datatracker.ietf.org/doc/html/draft-ietf-httpbis-rfc6265bis-18#section-5.2).
  - Done when unsafe cookie-authenticated requests require an allowed exact
    origin (or a CSRF token), explicit bearer-token requests retain
    cross-origin support, mutation endpoints require JSON, and regression tests
    cover foreign sibling origins, absent origins, and valid bearer requests.

- [ ] **[bug] Never retarget an orphaned remote tile to the local daemon.**
  Removing a server leaves its tiles in the persisted layout. Rendering then
  falls back to `localServer()`, so remote session `#1` may show or terminate
  local session `#1` ([GridPage.tsx](web/src/grid/GridPage.tsx#L183),
  [ServersPanel.tsx](web/src/settings/ServersPanel.tsx#L42)).
  - Done when an unknown `serverId` renders a non-interactive “server removed”
    state, removal offers to clean affected tiles, and tests prove neither
    terminal attachment nor termination can reach another server.

- [ ] **[reliability] Keep tmux sessions alive across Linux service restarts.**
  The systemd user unit uses the default `KillMode=control-group`, while tmux is
  spawned beneath the daemon; stopping/upgrading the service can kill the tmux
  server and defeats multimux's persistence promise
  ([svc.go](internal/svc/svc.go#L42),
  [manager.go](internal/tmuxmgr/manager.go#L140)).
  - Done when multimux-owned tmux sessions survive stop/start and binary
    upgrades on Linux, with an integration test exercising the installed unit's
    actual process/cgroup lifecycle.

## P1 — correctness and reliability

- [ ] **[security] Remove shell interpolation from Linux CA trust.**
  `sudo sh -c` interpolates a CA path derived from the invoking user's
  `MULTIMUX_DATA_DIR`; Go `%q` is not POSIX shell escaping, and command
  substitution inside the resulting double quotes can run as root when that
  user authorizes `multimux ca trust` via sudo
  ([ca.go](cmd/ca.go#L13), [trust.go](internal/pki/trust.go#L17)).
  - Done when copying and trust-store refresh use fixed argv or stdin with no
    shell-expanded path, and tests include spaces, quotes, `$()`, backticks, and
    leading-dash paths.

- [ ] **[security] Validate and commit daemon identity settings atomically.**
  The settings API writes hostname, SANs, and port one at a time, bypasses
  `applyHostname`'s passkey lockout protection, and reports the RP-ID warning
  only after saving ([api.go](internal/server/api.go#L121),
  [serve.go](cmd/serve.go#L76),
  [DaemonPanel.tsx](web/src/settings/DaemonPanel.tsx#L28)).
  - Done when CLI and API share canonical validation, invalid/partial writes
    leave all settings unchanged, RP-ID changes with credentials require
    confirmation before mutation, the UI clearly explains restart/CA-trust
    consequences, and transaction/validation tests cover empty names, ports,
    IPs, duplicate/invalid SANs, and case normalization.

- [ ] **[bug] Only advertise origins compatible with the WebAuthn RP ID.**
  `hostnames` adds a bare hostname, its `.local` form, and arbitrary extra SANs,
  then `computeOrigins` registers all of them against one RP ID
  ([serve.go](cmd/serve.go#L32), [serve.go](cmd/serve.go#L103)). WebAuthn
  requires the RP ID to equal or be a registrable-domain suffix of the origin's
  effective domain ([WebAuthn Level
  3](https://www.w3.org/TR/webauthn-3/#sctn-validating-origin)).
  Bare aliases, sibling SANs, and IP origins can have valid TLS while passkeys
  still fail.
  - Done when startup/settings reject or clearly classify TLS-only aliases,
    every printed setup/login origin is RP-ID compatible, IP handling is
    explicitly supported or rejected, and tests cover `.local`, MagicDNS/FQDN,
    extra SAN, uppercase, and IP cases. If related-origin requests are adopted,
    serve and test the required well-known document.

- [ ] **[bug] Scope WebAuthn ceremonies to individual flows.**
  The auth manager stores one global registration session and one global login
  session; a second tab/request overwrites the first, and any finish consumes
  the shared state ([auth.go](internal/auth/auth.go#L42),
  [webauthn.go](internal/auth/webauthn.go#L78)).
  - Done when begin returns a random flow ID backed by independent, expiring,
    single-use ceremony state; concurrent logins/registrations cannot interfere;
    setup-code abuse cannot invalidate unrelated valid flows; and rate/size
    limits prevent unauthenticated state exhaustion.

- [ ] **[bug] Make the browser cookie follow the server's sliding expiry.**
  token validation extends the database expiry, but the cookie retains its
  original fixed 30-day lifetime and is never refreshed
  ([auth.go](internal/auth/auth.go#L124),
  [authapi.go](internal/server/authapi.go#L10)).
  - Done when a successful near-expiry cookie authentication renews both DB
    state and `Set-Cookie`, bearer tokens have a documented expiry policy, and
    clock-injected tests cover renewal, expiry, and logout.

- [ ] **[security] Prevent concurrent deletion of the last passkey.**
  credential count and deletion are separate operations, so two requests can
  both observe two keys and delete both ([authapi.go](internal/server/authapi.go#L147)).
  - Done when the invariant “at least one passkey remains” is enforced in one
    database transaction/conditional statement, missing IDs return 404, and a
    concurrent regression test always leaves one credential.

- [ ] **[bug] Make `auth reset` transactional and coordinate the running daemon.**
  Credentials and auth sessions are deleted separately; already-upgraded
  WebSockets stay usable, and the success text claims the daemon will notice
  even though it does not mint/print a new setup code until restart
  ([auth.go](cmd/auth.go#L27), [events.go](internal/server/events.go#L57)).
  - Done when reset is all-or-nothing, invalidates/actively closes authenticated
    sockets, and either safely signals/restarts the daemon and prints a fresh
    setup URL or accurately requires a restart. Test partial failure and live
    REST/WS access after reset.

- [ ] **[reliability] Use a private tmux server in production.**
  Production passes an empty socket name, sharing the user's default tmux
  server; multimux changes server-global options and names sessions `mm-N`,
  allowing collisions or control of an unrelated session
  ([serve.go](cmd/serve.go#L277),
  [manager.go](internal/tmuxmgr/manager.go#L55)).
  - Done when each install has a stable private socket, foreign sessions and
    global options remain untouched, existing multimux sessions have a
    documented migration/reattach path, and integration tests start adversarial
    `mm-*` sessions on the default server.

- [ ] **[bug] Reconcile from one authoritative tmux listing and preserve errors.**
  Every five seconds the daemon runs `has-session` once per DB row, and
  `IsAlive` maps every tmux error to “missing,” so a transient failure can
  permanently mark live sessions dead
  ([events.go](internal/server/events.go#L96),
  [sessions.go](internal/server/sessions.go#L159),
  [manager.go](internal/tmuxmgr/manager.go#L125)).
  - Done when each pass performs one list operation, distinguishes “no server”
    from command/transport errors, only marks confirmed absent names dead, and a
    100-session test has O(1) subprocesses per tick.

- [ ] **[bug] Do not report a failed tmux termination as success.**
  The kill handler discards `KillSession` errors, marks the database row dead,
  and returns 204. Reconciliation then skips that non-running row, leaving a
  live but permanently untracked tmux session
  ([sessions.go](internal/server/sessions.go#L72),
  [sessions.go](internal/server/sessions.go#L159)).
  - Done when a confirmed kill failure returns a useful error and preserves the
    running row for retry/reconciliation, “already absent” has explicit
    idempotent semantics, and injected kill-failure tests prove no live session
    becomes untracked.

- [ ] **[reliability] Treat the TLS leaf certificate and key as one recoverable unit.**
  Separate temporary-file renames can leave mismatched cert/key files after a
  crash, while rotation checks do not validate the pair
  ([pki.go](internal/pki/pki.go#L181)).
  - Done when `Ensure` detects missing/corrupt/mismatched pairs and self-heals,
    generation checks close/fsync/rename errors, and fault-injection tests cover
    interruption at every publish boundary without making the next startup
    unrecoverable.

- [ ] **[reliability] Make schema migrations atomic and reject future schemas.**
  Each migration body and `PRAGMA user_version` update are separate commits,
  and a database from a newer binary opens silently
  ([store.go](internal/store/store.go#L83)).
  - Done when each migration plus version bump commits in one transaction,
    rollback leaves the prior version usable, future versions fail with an
    actionable error, and failure-injection tests cover both properties.

- [ ] **[bug] Serialize/coalesce layout persistence so the latest edit wins.**
  Every grid action fires an unawaited PUT and swallows failures; responses may
  complete out of order, while handlers also derive updates from captured
  layout state ([GridPage.tsx](web/src/grid/GridPage.tsx#L56)).
  - Done when local updates are functional/reducer-based, persistence has at
    most one write in flight and eventually writes the newest state, failures
    are visible/retriable, and deferred-response tests finish requests in
    reverse order.

- [ ] **[bug] Render terminal failure/dead states instead of reconnecting forever.**
  Dead or missing sessions still mount a terminal; HTTP 404/409 WebSocket
  failures become an endless “daemon unreachable” loop
  ([GridPage.tsx](web/src/grid/GridPage.tsx#L195),
  [TerminalTile.tsx](web/src/term/TerminalTile.tsx#L47)). The offline/exited
  overlay also lacks positioning styles and is clipped behind the terminal
  ([TerminalTile.tsx](web/src/term/TerminalTile.tsx#L104),
  [index.css](web/src/index.css#L406)).
  - Done when REST state gates connection, terminal handshakes distinguish
    missing/dead/auth/network failures, permanent states stop retrying, and a
    visible accessible overlay offers dismiss/reconnect actions. Test with a
    real or protocol-faithful WebSocket rather than only mocking the tile.

- [ ] **[bug] Disable the launcher while a server switch is loading.**
  Switching servers leaves the previous tool/directory IDs selectable until
  new fetches resolve, so “New” can launch mismatched IDs on the new daemon
  ([HeaderLauncher.tsx](web/src/grid/HeaderLauncher.tsx#L23)).
  - Done when selection state is cleared immediately, stale requests cannot
    win, launch is disabled until both lists belong to the selected server, and
    an overlapping-request test proves no cross-server launch.

- [ ] **[usability] Provide a complete remote-server auth lifecycle.**
  An expired remote token only offers “Reload,” which reloads the same token;
  reconnects mint additional server sessions, and remove has no best-effort
  revocation/logout path ([GridPage.tsx](web/src/grid/GridPage.tsx#L163),
  [servers.ts](web/src/servers.ts#L31),
  [authapi.go](internal/server/authapi.go#L188)).
  - Done when remote auth expiry offers reconnect/remove, reconnect replaces or
    revokes the old token where possible, removal can revoke the remote session,
    local logout is exposed in the UI, and no flow loops deterministically on
    stale credentials.

- [ ] **[reliability] Make event reconnects converge to current state.**
  The hub silently drops messages for a slow subscriber and the initial
  `hello` carries no snapshot/sequence, so a connected tab can remain stale
  forever ([events.go](internal/server/events.go#L38),
  [events.go](internal/server/events.go#L57)). A delayed classification request
  can also overwrite a later successful `open` status
  ([useEvents.ts](web/src/useEvents.ts#L35)).
  - Done when subscribe/reconnect performs an authoritative resync or uses
    sequence numbers with gap detection, stale probes are aborted/generation
    guarded, and tests cover queue overflow, disconnect/reconnect, and
    classify-after-open ordering.

- [ ] **[bug] Roll back tmux creation after post-create failure.**
  If `new-session` succeeds but a later `respawn-pane` fails, the API deletes
  only its DB row and leaves an untracked tmux session
  ([manager.go](internal/tmuxmgr/manager.go#L55),
  [sessions.go](internal/server/sessions.go#L25)).
  - Done when any error after creation kills the exact newly-created session,
    preserves the primary error plus cleanup context, and fault-injection
    tests prove no orphan remains.

## P2 — efficiency, resilience, and usability

- [ ] **[efficiency] Initialize tmux server-wide options once and correctly.**
  `history-limit` is attempted before the first tmux server exists, so the first
  pane keeps the ~2,000-line default; terminal-feature appends then repeat for
  every session and accumulate duplicate values
  ([manager.go](internal/tmuxmgr/manager.go#L55)).
  - Done when private-server initialization precedes the first pane, global
    options are idempotent and batched, per-session creation runs only
    session-specific commands, and a fresh-socket test retains more than 2,000
    lines without duplicate feature entries.

- [ ] **[reliability] Bound WebSocket resources and support graceful shutdown.**
  Event and PTY sockets have no read limits/deadlines or pong-based liveness,
  ping-writer failure does not necessarily unblock the PTY reader, and
  background `time.Tick` goroutines cannot be stopped
  ([ws.go](internal/server/ws.go#L90),
  [ws.go](internal/server/ws.go#L147),
  [events.go](internal/server/events.go#L68),
  [events.go](internal/server/events.go#L96)).
  - Done when input size and idle time are bounded, pong/read deadlines detect
    dead peers, all goroutines share cancellation, HTTP shutdown closes sockets
    and tickers, and leak/oversize/half-open tests pass under `-race`.

- [ ] **[usability] Distinguish loading, auth, daemon, and validation failures.**
  App startup maps network/500 errors from `/me` to the login page; settings
  panels often show permanent loading, fake empty lists, `console.error`, or
  swallowed failures, and API helpers discard useful backend error detail
  ([App.tsx](web/src/App.tsx#L24),
  [DaemonPanel.tsx](web/src/settings/DaemonPanel.tsx#L16),
  [GridPage.tsx](web/src/grid/GridPage.tsx#L61)).
  - Done when the API exposes a typed error, screens distinguish setup/401/403/
    unreachable/5xx/empty states, mutations have busy/error/retry feedback,
    terminate does not remove a tile after an unknown failure, and request
    cancellation prevents stale screen updates.

- [ ] **[bug] Validate persisted browser state and remote origins at runtime.**
  Stored server data is blindly cast from JSON, layout validation checks only
  two property names, and server entry accepts paths, insecure origins,
  duplicates, or malformed URLs ([servers.ts](web/src/servers.ts#L9),
  [GridPage.tsx](web/src/grid/GridPage.tsx#L12),
  [ServersPanel.tsx](web/src/settings/ServersPanel.tsx#L34)).
  - Done when versioned schemas validate/normalize/quarantine bad localStorage
    and layout data, remote entries canonicalize to `URL.origin`, production
    requires HTTPS (with an explicit local-dev exception), duplicates/local
    origins are rejected, and popup messages validate origin, source, state,
    and token shape.

- [ ] **[bug] Use stable tile identity and validate drag data.**
  Tiles are keyed by array index, so swapping/reordering reuses each cell
  component for a different session and makes `TerminalTile` dispose/rebuild
  its xterm and WebSocket from the changed URL. Drop also accepts arbitrary
  external data and `Number("") === 0`, which can swap tile zero
  ([GridPage.tsx](web/src/grid/GridPage.tsx#L183)).
  - Done when occupied tiles use server/session identity, empty cells have
    stable cell identity, only the custom MIME type with an in-range integer is
    accepted, drag begins from a handle instead of the terminal, and tests
    assert terminal mount/dispose counts.

- [ ] **[usability] Make the grid responsive, touch/keyboard operable, and accessible.**
  The header does not wrap, the grid uses a fixed `100vh - 60px`, native drag
  has no touch/keyboard alternative, and settings navigation lacks full tab/
  current-page semantics ([App.tsx](web/src/App.tsx#L39),
  [GridPage.tsx](web/src/grid/GridPage.tsx#L173),
  [index.css](web/src/index.css#L194)).
  - Done when 320 px and 768 px layouts remain operable using dynamic viewport
    units, tiles can be moved without mouse drag, tabs/tables/forms/statuses
    follow accessible patterns, focus is visible/preserved, and automated
    accessibility plus keyboard tests cover the core flows.

- [ ] **[efficiency] Lazy-load terminal and route-only frontend code.**
  `App` eagerly imports the grid and xterm stack, producing one ~562 kB bundle
  even for login/setup/settings ([App.tsx](web/src/App.tsx#L1),
  [TerminalTile.tsx](web/src/term/TerminalTile.tsx#L1)).
  - Done when terminal/grid and other route-only code are split behind lazy
    boundaries, login/setup avoid downloading xterm, and CI enforces explicit
    initial/chunk gzip budgets instead of merely raising Vite's warning limit.

- [ ] **[reliability] Harden service install/uninstall and preserve configuration.**
  Generated units omit data-dir/hostname/proxy options; Linux escaping handles
  neither all systemd specifiers nor arbitrary environment values. Uninstall
  ignores stop failures, removes the unit anyway, and skips daemon-reload
  ([svc.go](internal/svc/svc.go#L42), [svc.go](internal/svc/svc.go#L105),
  [svc.go](internal/svc/svc.go#L141)).
  - Done when install accepts/persists explicit serve configuration using native
    escaping, reinstall preserves it, uninstall propagates real stop errors,
    reloads the manager and verifies inactivity before success, and tests use an
    injectable command runner for both OS implementations.

- [ ] **[reliability] Rotate or explicitly recover an expiring CA.**
  `Ensure` rotates leaf certificates but does not treat an expired/near-expiry
  CA as invalid, so a long-lived install can keep signing unusable leaves
  ([pki.go](internal/pki/pki.go#L50)).
  - Done when time-injected checks detect CA expiry sufficiently early, perform
    a recoverable regeneration with a prominent re-trust instruction, never
    issue a leaf beyond its issuer's validity, and boundary tests cover both CA
    and leaf dates.

- [ ] **[bug] Apply strict, consistent HTTP API semantics.**
  JSON decoding accepts trailing values and unknown fields, content type is not
  enforced, layout's 64 KiB limiter can misclassify an exact-prefix oversized
  body, and updates/deletes may report success for missing rows
  ([server.go](internal/server/server.go#L208),
  [sessions.go](internal/server/sessions.go#L138),
  [api.go](internal/server/api.go#L42)).
  - Done when mutation endpoints enforce JSON media type, one document, known
    fields, and explicit size errors; store mutations expose rows affected;
    missing resources consistently return 404; conflicts return 409; and table
    tests cover the contract.

- [ ] **[efficiency] Refresh only the server affected by an event.**
  Event callbacks discard source identity: any session event refetches sessions
  from every daemon, and a remote `layout_changed` refetches the local layout
  ([GridPage.tsx](web/src/grid/GridPage.tsx#L16),
  [GridPage.tsx](web/src/grid/GridPage.tsx#L61)).
  - Done when bridges pass their server ID, session events fetch only that
    server, only the layout owner can trigger layout refresh, bursts are
    coalesced, and request-count tests cover multiple daemons.

## P3 — product and engineering polish

- [ ] **[usability] Define an honest service-worker/offline policy.**
  The worker registers only after the first page load, precaches nothing, and
  does not await cache writes, so a first offline launch has no shell despite
  the PWA-like behavior ([main.tsx](web/src/main.tsx#L12),
  [sw.js](web/public/sw.js#L1)).
  - Done when the project either precaches a versioned app shell with tested
    update/offline behavior, or removes the worker and any offline claim.

- [ ] **[engineering] Make verification release-equivalent.**
  `verify.sh` builds the frontend but never builds the Go binary containing the
  embedded assets, and vulnerability scans are manual
  ([verify.sh](verify.sh#L5)).
  - Done when CI builds/smoke-tests the final binary after `pnpm build`, checks
    the embedded `/` response, runs `go test -race` on supported platforms, and
    schedules `govulncheck` plus production dependency audit with a documented
    triage policy.

## Completion policy

For every item:

1. Add a regression test that fails for the reported behavior before the fix.
2. Keep `./verify.sh`, `go test -race ./...`, and the production build green.
3. Update user/security/operations documentation when behavior or recovery
   steps change.
4. Avoid combining unrelated refactors with the fix; split oversized items into
   reviewable implementation PRs while retaining the acceptance criteria above.
