# TODO — triaged backlog

Audit date: 2026-07-18 at `e6b1547`; triaged 2026-07-18 against the product
positioning in [README.md](README.md) (single-user, private network, personal
tool; the local shell is the root of trust). Claims spot-checked at `299809d`.
Struck items and reasons are listed at the bottom.

## Baseline

- `./verify.sh`, `go test -race ./...`, `go build`: pass.
- `govulncheck` and `pnpm audit --prod`: no reachable/known vulnerabilities.

## Priority key

- **P0:** security, data-loss, or core-product blocker; fix before the next
  release.
- **P1:** high-impact correctness/reliability issue; fix next.
- **P2:** small, cheap hardening or usability fixes; batch opportunistically.
- **P3:** polish.

## P0 — release blockers

- [x] **[security] Block same-site CSRF on cookie-authenticated API writes.**
  The API reflects `Access-Control-Allow-Origin: *`, never validates `Origin`,
  and accepts CORS-safelisted `text/plain` bodies
  ([server.go](internal/server/server.go#L149),
  [server.go](internal/server/server.go#L214)). A same-site sibling origin
  (relevant on shared suffixes like `ts.net`) can fire cookie-carrying
  mutations. Cheap fix, real exposure.
  - Done when unsafe cookie-authenticated requests require an allowed exact
    origin, mutation endpoints require a JSON content type, and tests cover
    foreign origin, absent origin, and bearer-token requests.

- [x] **[bug] Never retarget an orphaned remote tile to the local daemon.**
  Removing a server leaves its tiles in the persisted layout; rendering falls
  back to `localServer()`, so remote session `#1` may show or terminate local
  session `#1` ([GridPage.tsx](web/src/grid/GridPage.tsx#L212),
  [ServersPanel.tsx](web/src/settings/ServersPanel.tsx#L42)). Wrong-target
  terminate is the one data-loss path in the UI.
  - Done when an unknown `serverId` renders a non-interactive "server removed"
    state and a test proves neither attachment nor termination can reach
    another server.

- [x] **[reliability] Keep tmux sessions alive across Linux service restarts.**
  The systemd user unit uses default `KillMode=control-group`, so stopping or
  upgrading the service kills the tmux server underneath it
  ([svc.go](internal/svc/svc.go#L42)). This breaks the core persistence
  promise in the README's first paragraph.
  - Done when multimux-owned tmux sessions survive service stop/start and
    binary upgrade on Linux, verified against the installed unit.

## P1 — correctness and reliability

- [x] **[reliability] Use a private tmux server in production.**
  Production passes an empty socket name ([serve.go](cmd/serve.go#L162)),
  sharing the user's default tmux server: multimux mutates server-global
  options and could collide with or kill a hand-made `mm-*` session. Dev mode
  already uses a private socket; production should match.
  - Done when production uses a stable private socket and a README note covers
    reattaching pre-existing `mm-*` sessions (pre-1.0; no automated migration).

- [ ] **[security] Validate daemon identity settings once, atomically, with
  the passkey lockout guard.** The settings API writes hostname/SANs/port one
  at a time and bypasses `applyHostname`'s lockout protection, so the UI can
  brick your own passkey login ([api.go](internal/server/api.go#L121),
  [serve.go](cmd/serve.go#L76)). Related: setup/login output advertises
  origins (bare alias, IPs, extra SANs) that can never pass WebAuthn RP-ID
  validation ([serve.go](cmd/serve.go#L103)).
  - Done when CLI and API share one validation path, invalid/partial writes
    change nothing, RP-ID-affecting changes with existing credentials require
    explicit confirmation, and only RP-ID-compatible origins are printed.

- [ ] **[bug] Render terminal failure/dead states instead of reconnecting
  forever.** Dead or missing sessions still mount a terminal; 404/409
  WebSocket failures become an endless "daemon unreachable" loop, and the
  offline/exited overlay lacks positioning styles so it is clipped behind the
  terminal ([TerminalTile.tsx](web/src/term/TerminalTile.tsx#L47),
  [index.css](web/src/index.css#L406)). Daily-use UX bug.
  - Done when permanent states (missing/dead/auth) stop retrying and show a
    visible overlay with dismiss/reconnect actions.

- [ ] **[bug] Reconcile from one tmux listing and don't kill live rows on
  transient errors.** The daemon runs `has-session` per DB row every 5s, and
  `IsAlive` maps every tmux error to "missing," so one transient failure
  permanently marks live sessions dead
  ([events.go](internal/server/events.go#L96),
  [manager.go](internal/tmuxmgr/manager.go#L125)).
  - Done when each pass does one list call, distinguishes "no server" from
    command errors, and only confirmed-absent sessions are marked dead.

- [ ] **[bug] Make session create/kill failures leave consistent state.**
  Two adjacent holes: the kill handler discards `KillSession` errors, marks
  the row dead, and returns 204, orphaning a live session
  ([sessions.go](internal/server/sessions.go#L72)); and a `respawn-pane`
  failure after `new-session` deletes only the DB row, same orphan
  ([manager.go](internal/tmuxmgr/manager.go#L55)).
  - Done when kill failure returns an error and preserves the row, and any
    post-create failure kills the just-created tmux session.

- [ ] **[bug] Serialize/coalesce layout persistence so the latest edit wins.**
  Every grid action fires an unawaited PUT from captured state; out-of-order
  responses can persist a stale layout
  ([GridPage.tsx](web/src/grid/GridPage.tsx#L56)).
  - Done when local updates are reducer-based and persistence keeps at most
    one write in flight, always eventually writing the newest state.

- [ ] **[usability] Give remote-server auth expiry a way out.**
  An expired remote token only offers "Reload," which reloads the same dead
  token ([GridPage.tsx](web/src/grid/GridPage.tsx#L163),
  [servers.ts](web/src/servers.ts#L31)). Multi-daemon is a headline feature;
  its failure mode is a dead end.
  - Done when expiry offers reconnect (replacing the stored token) or remove.
    Best-effort only — no cross-daemon revocation machinery.

## P2 — small, cheap fixes; batch opportunistically

- [ ] **[bug] Initialize tmux server options before the first pane.**
  `history-limit` is set before the tmux server exists, so the first pane
  keeps the ~2,000-line default — against the "scrollback is still there"
  promise ([manager.go](internal/tmuxmgr/manager.go#L55)). Pairs naturally
  with the private-socket item (init once at server start, idempotently).

- [ ] **[bug] Key tiles by server/session identity, not array index.**
  Index keys make swaps rebuild xterm and the WebSocket for the wrong session
  ([GridPage.tsx](web/src/grid/GridPage.tsx#L183)). Also accept only the
  custom drag MIME type with an in-range integer (`Number("") === 0` swaps
  tile zero today).

- [ ] **[usability] Make tile reorder work on touch.** README names tablets
  as a target; native HTML drag has no touch path. A simple move/swap
  affordance is enough — no broader a11y/320px program.

- [ ] **[reliability] Resync state on event-socket reconnect.** The hub drops
  messages for slow subscribers and `hello` carries no snapshot, so a
  reconnected tab can stay stale forever
  ([events.go](internal/server/events.go#L57)). Refetch sessions/layout on
  (re)connect; skip sequence-number machinery.

- [ ] **[bug] Make `auth reset` transactional and honest.** Credentials and
  sessions are deleted in separate statements, and the success text claims
  the daemon notices without restart when it doesn't
  ([auth.go](cmd/auth.go#L27)). One transaction; accurate "restart the
  daemon" message.

- [ ] **[bug] Refresh the session cookie when the server extends expiry.**
  Sliding DB expiry, fixed 30-day cookie
  ([authapi.go](internal/server/authapi.go#L10)). Re-issue `Set-Cookie` on
  near-expiry authentication.

- [ ] **[security] Replace `sudo sh -c` in Linux CA trust with fixed argv.**
  Go `%q` is not POSIX quoting; a hostile `MULTIMUX_DATA_DIR` can run
  commands as root ([trust.go](internal/pki/trust.go#L17)). Marginal threat
  (attacker already controls your env), trivial fix.

- [ ] **[reliability] Persist serve configuration into the service unit.**
  Generated units bake in only PATH ([svc.go](internal/svc/svc.go#L42)); a
  custom `MULTIMUX_DATA_DIR` is silently lost under the service, which
  resolves to a fresh data dir, new CA, and setup-pending. Also propagate
  uninstall stop failures instead of ignoring them.

- [ ] **[reliability] Commit each schema migration atomically and reject
  future schemas.** Migration body and `PRAGMA user_version` are separate
  commits; a newer binary's DB opens silently
  ([store.go](internal/store/store.go#L83)). Both are a few lines.

- [ ] **[reliability] Self-heal a mismatched TLS leaf cert/key pair.**
  Separate renames can crash into a mismatched pair `Ensure` never rechecks
  ([pki.go](internal/pki/pki.go#L181)). Leaf regeneration is cheap: validate
  the pair, regenerate on any doubt. No fault-injection matrix needed.

- [ ] **[bug] Clear launcher selections while a server switch loads.**
  Stale tool/directory IDs stay selectable and can launch mismatched IDs on
  the new daemon ([HeaderLauncher.tsx](web/src/grid/HeaderLauncher.tsx#L23)).
  Clear on switch, disable New until fetches for the selected server resolve.

## P3 — polish

- [ ] **[usability] Surface real error states in the UI.** Startup maps
  network/500 from `/me` to the login page; settings panels show permanent
  spinners or fake empty lists ([App.tsx](web/src/App.tsx#L24),
  [DaemonPanel.tsx](web/src/settings/DaemonPanel.tsx#L16)). Incremental:
  typed API error, then distinguish unreachable/401/5xx/empty per screen.

- [ ] **[engineering] Remove the service worker and any offline claim.**
  It precaches nothing and can't deliver a useful offline experience for a
  live-terminal app ([sw.js](web/public/sw.js#L1)). Decision: remove rather
  than build out a tested offline shell.

- [ ] **[engineering] Make `verify.sh`/CI build the real binary.** The Go
  binary with embedded assets is never built in verification
  ([verify.sh](verify.sh#L5)). Build it after `pnpm build`, smoke-test `/`,
  and add a scheduled CI `govulncheck` + `pnpm audit`.

- [ ] **[reliability] Warn on and recover from CA expiry.** `Ensure` rotates
  leaves but never checks the CA's own validity
  ([pki.go](internal/pki/pki.go#L50)). Detect early, regenerate with a
  prominent re-trust instruction, never issue a leaf outliving its issuer.

- [ ] **[docs] Replace the README screenshot placeholder** with a real capture
  of the grid before the next release.

## Struck (won't do) — conflicts with positioning

Removed from the audit; reasons recorded so they aren't re-litigated:

- **Scope WebAuthn ceremonies per flow.** Single user; the two-tab race
  self-heals on retry, and unauthenticated state exhaustion is not in the
  private-network threat model.
- **Prevent concurrent deletion of the last passkey.** One user racing
  themselves; `multimux auth reset` from the shell is the documented recovery.
- **Bound WebSocket resources against abuse.** DoS by the sole trusted user on
  a private network is out of scope. (If a real half-open-connection problem
  shows up in use, revisit narrowly.)
- **Versioned schemas/quarantine for localStorage and remote origins.** Own
  browser, own data; the dangerous consequence (orphaned tile → wrong daemon)
  is fixed directly by the P0 item.
- **Lazy-load/split the frontend bundle.** 155 kB gzip, served once over LAN,
  cached by the PWA. Complexity for nothing.
- **Strict HTTP API semantics as a program.** The only client is the embedded
  frontend. JSON content-type enforcement folded into the P0 CSRF item; the
  rest is API-design ceremony without a consumer.
- **Per-server event refresh scoping.** Refetching 2–3 daemons' session lists
  on an event is negligible at personal scale.
- **Automated accessibility/320 px responsive program.** Tablet operability
  kept (P2 touch item); a WCAG test suite for a single-user tool is not.

## Completion policy

1. P0/P1 fixes get a regression test that fails before the fix. P2/P3: test
   where it's cheap; don't build test infrastructure to satisfy this line.
2. Keep `./verify.sh` and `go test -race ./...` green.
3. Update docs when behavior or recovery steps change.
4. Don't combine unrelated refactors with a fix.
