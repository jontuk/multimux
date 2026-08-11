# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Validation

- Fix errors and warnings as they appear (compiler, lint, test, build output). Do not leave them for later or present work as complete while any remain.
- `./verify.sh` runs everything CI runs: gofmt, `go vet`, `go test ./...`, `pnpm lint`, `pnpm test`, `pnpm build`, `go build`, and `scripts/smoke.sh`. Build artefacts go to a temp dir, never the working tree.

```bash
go test ./internal/server/                      # one package
go test ./internal/server/ -run TestPTYAuth     # one test
cd web && pnpm test src/__tests__/api.test.ts   # one web test file
cd web && pnpm lint && pnpm format              # eslint + prettier --check / --write
```

**Build order matters.** `main.go` does `//go:embed all:web/dist`, so `go build` picks up whatever is in `web/dist` at that moment. Run `cd web && pnpm build` before `go build` or the binary ships stale assets. `web/dist/.gitkeep` is committed so a bare checkout still compiles — but then `/` answers 501 "web assets missing". That failure mode is exactly what `scripts/smoke.sh` catches, which is why `verify.sh` runs it after the real build.

Requires tmux on PATH — the daemon refuses to start without it, and tmux-touching tests need it too.

## Architecture

Single Go binary (`multimux`) that serves a browser-based tmux terminal grid over HTTPS. No server-side rendering, no external services, no daemon-to-daemon traffic — a multi-host grid is assembled entirely in the browser.

Request path: `cmd/serve.go` wires the pieces, then `internal/server.Handler()` stacks middleware outermost-first as `logging → CORS → setup gate → CSRF gate → auth → body cap` around a single `http.ServeMux` (`internal/server/server.go:routes`). Static assets, `/healthz`, and the `/api/auth/{setup,login}` ceremonies bypass auth; everything else 403s while the daemon is *setup-pending*.

Packages, by role:

| Package | Role |
| --- | --- |
| `cmd` | CLI: `serve`, `service`, `ca`, `auth`, `config`. Dispatch is a plain switch in `cmd.Execute`; each command owns its own `flag.FlagSet` and usage string. |
| `internal/store` | All state in one SQLite file (`$DATA_DIR/multimux.db`) via `modernc.org/sqlite` (pure Go, no cgo). Tables: settings, tools, dirs, sessions, layout, credentials, auth_sessions. |
| `internal/tmuxmgr` | Owns tmux. `Manager` runs `tmux` subcommands, `attach.go` opens a PTY per connection, `Arbiter` decides who may resize the shared window. |
| `internal/server` | HTTP/WS surface, plus `Hub` (fan-out of session/layout events to every open tab) and the background reconcile tickers. |
| `internal/auth` | WebAuthn passkeys, first-run setup codes, server-side session tokens. |
| `internal/pki` | Self-generated, name-constrained CA + auto-rotating leaf. |
| `internal/identity` | The one validated write path for hostname / extra SANs / port. |
| `internal/config` | The one definition of every user-configurable setting. |
| `internal/svc` | launchd LaunchAgent (macOS) / systemd user unit (Linux) install. |
| `internal/gitinfo` | Repo/branch/dirty state shown per session tile. |
| `web/` | React 19 + Vite + xterm.js SPA, embedded into the binary. |

State lives in `$MULTIMUX_DATA_DIR` (default `~/.local/share/multimux`): `multimux.db` and `pki/`. Two daemons never share one; a dev run points it at a scratch dir.

### Invariants worth knowing before you edit

- **`store.migrations` is append-only.** `PRAGMA user_version` tracks progress; editing a shipped entry breaks existing installs. Timestamps are stored as RFC3339 UTC text so they round-trip regardless of driver scan behaviour.
- **Hostname changes go through `identity.Apply`, never `store.SetSetting` directly.** The hostname *is* the WebAuthn RP ID, so changing it strands every registered passkey. `Apply` returns `*identity.RPChangeError` when credentials exist; both the `--hostname` flag and the settings API depend on that guard. Bare single-label hostnames (`macmini`) get a `.local` suffix because go-webauthn rejects dotless RP IDs.
- **New settings go in `internal/config`**, not into the CLI and API separately — that package exists so the two cannot disagree. Keys are stored underscored (`confirm-terminate` → `confirm_terminate`).
- **Terminal resize is arbitrated, not free.** All connections to a session share one tmux window. `Arbiter` gives size ownership to whichever *client* most recently sent keyboard input (or an `active` resize); non-owners resize only their own attach PTY. Passing `resizeWindow: true` from a non-owner is a bug. Ownership is keyed on the `client=` id the tile puts on the PTY WebSocket (`web/src/clientId.ts`, stable per browser) and outlives that client's connections by `ownerGrace` — otherwise every reconnect would drop ownership and let another machine's passive resize shrink the window under whoever is typing.
- **Token auth and cookie auth are deliberately different.** Cookies are `SameSite=Strict`, so cross-daemon tiles authenticate with an explicit bearer token (minted via `POST /api/auth/token`). Consequently the CSRF gate and the WebSocket origin check apply to cookie-authenticated requests only, and `TokenFromRequest` is explicit-token-first so a bad token cannot ride a valid cookie past a skipped check. Read the comment above `checkWSOrigin` before touching either.
- **Sessions are reconciled, not trusted.** A row is written before its tmux session exists, so `Reconcile` (every 5s, plus at startup) honours `reconcileGrace` before declaring a running row dead. `CheckGitInfo` rides the same tick; auth-session sweeping runs hourly.
- **`pki.Ensure` compares hostname slices for equality**, so `hostnames()` must return a deterministic order (hostname first, then sorted extra SANs) or the CA regenerates spuriously — which forces the user to re-trust it.
- **Frontend/backend duplicated constants**: the default theme color lives in `internal/server/pwa.go`, `web/src/App.tsx`, and `web/index.html`; the app icon SVG is templated in `pwa.go` and mirrored in `web/public/icon.svg`. Change them together.
- **Grid layout has a canonical form** (`web/src/grid/model.ts`): occupied tiles packed to the front row-major, rows derived from count, trailing nulls, cols clamped 1–4. Always round-trip through `normalize` — sessions are never dropped, the grid grows instead.
- Multi-host servers are browser-local: `web/src/servers.ts` keeps the list (and tokens) in `localStorage`. Nothing about a remote daemon is persisted server-side.

## Dev loops

Always use a throwaway data dir — never the real install (it holds real passkeys, sessions, and the trusted CA):

```bash
export MULTIMUX_DATA_DIR="/tmp/multimux-dev-$(date +%s)"
go run . serve --port 8790          # port 8686 is usually the real daemon
```

For frontend hot reload, run the daemon with `--dev` and Vite against it:

```bash
export MULTIMUX_DATA_DIR="/tmp/multimux-dev-$(date +%s)"   # --dev refuses without this
go run . serve --dev --port 8787
cd web && MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev   # proxies /api, /healthz, /ws
```

`--dev` **disables authentication entirely** — every route on the daemon is served to anyone who can reach the port, which is a shell as your user. Use it only on a network you control. It also forces the RP ID to `localhost`, allows `http://localhost:5173`, seeds the cwd as a launch dir, and refuses to start unless `MULTIMUX_DATA_DIR` is set to something other than the default install path, or if that data dir has passkeys. It derives a private tmux socket from the data dir, so dev `mm-*` sessions can never collide with the real daemon's.

**Testing on a phone.** Vite binds all interfaces, so open `http://<your-lan-host>:5173` on the phone — hot reload and live terminals both work, with no CA trust and no passkey (plain http rules out both `Secure` cookies and WebAuthn, which is why `--dev` has no auth). Any browser works; the Chrome/Firefox-only caveat applied to the passkey login that no longer exists.

Testing the passkey and first-run setup flows means running **without** `--dev`, against a throwaway `MULTIMUX_DATA_DIR` and the daemon's own `https://` origin.

Backend-only changes: restart `go run . serve` and work against the daemon's own `https://` URL (needs `--trust-ca`, or `multimux ca trust` with the same `MULTIMUX_DATA_DIR`).

Poking at state directly is often faster than driving the UI:

```bash
sqlite3 "$MULTIMUX_DATA_DIR/multimux.db" "select * from settings;"
# fake a registered passkey to test credential-gated paths
sqlite3 "$MULTIMUX_DATA_DIR/multimux.db" \
  "insert into credentials (id,name,data,created_at,last_used_at) values ('x','k','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');"
```

Env vars: `MULTIMUX_DATA_DIR`, `MULTIMUX_HOSTNAME` (default for `--hostname`), plus `MULTIMUX_INSTALL_DIR` / `MULTIMUX_MARKER` used by `install.sh`.

## Project context

- macOS and Linux only; single-user by design (no accounts, roles, or permissions). Contributions are not accepted — don't add contributor-facing scaffolding.
- Meant to be reachable only over a network the user controls (LAN, VPN, Tailscale), not the public internet. The security model and its explicit non-goals are documented in README.md — read that section before changing anything in `auth`, `pki`, or the middleware stack.
- Releases: push a `v*` tag; goreleaser builds the archives. `version` is injected via `-ldflags -X main.version=…`.
- The weekly `audit` workflow runs `govulncheck` (fails on any finding — it does reachability analysis) and `pnpm audit --prod --audit-level high`.
