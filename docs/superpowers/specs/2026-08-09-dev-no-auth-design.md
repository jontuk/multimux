# Dev mode: no auth, reachable from a phone

## Problem

Testing frontend changes on a phone is currently impractical.

`--dev` forces the WebAuthn RP ID to `localhost` (`cmd/serve.go:284`), so a passkey
can only be registered from a browser on the daemon's own machine. A phone cannot
authenticate at all. The Vite dev server also binds loopback only, so the phone
cannot reach `:5173` even to try.

The goal is a dev loop where the phone opens `http://<lan-host>:5173`, gets hot
reload, and can drive real terminals against a throwaway daemon.

## Approach

`--dev` implies no authentication. The dev daemon serves every route
unauthenticated; Vite binds all interfaces and proxies to the daemon as it does
today.

Plain `http://` from a LAN address rules out the alternatives: `Secure` cookies
are not sent, and WebAuthn requires a secure context. Any design that keeps auth
in the loop cannot work over the chosen entry point.

## Security boundary

This turns a dev daemon into an unauthenticated shell server on every interface
of the machine. Anyone who can reach port 8787 or 5173 gets a terminal running as
the invoking user. It is acceptable only on a network the user controls, and only
for the duration of a dev session.

Two independent guards, both refusals at startup:

1. **Existing:** `--dev` refuses when the data dir has registered passkeys
   (`cmd/serve.go:264`).
2. **New:** `--dev` refuses unless `MULTIMUX_DATA_DIR` is set explicitly and
   resolves to something other than the default install path
   (`~/.local/share/multimux`). This is what stops the worst accident — no-auth
   against the real install.

Plus a loud multi-line startup banner naming the exposure, and a `slog.Warn` at
start so the condition is visible in logs.

`NoAuth` is reachable only from the `--dev` flag. No env var, no setting, no API.

## Backend changes (`internal/server`)

Add `Config.NoAuth bool`. Three touch points, all existing choke points:

| Location | Change |
| --- | --- |
| `authGate` (`server.go:186`) | When `NoAuth`, pass through — never construct `auth.Manager.Middleware`. |
| `setupGate` (`server.go:199`) | When `NoAuth`, pass through. Dev data dirs have no passkey, so otherwise every protected route 403s "setup pending". |
| `handleHealthz` (`server.go:278`) | Report `setupPending: false` when `NoAuth`; otherwise `App.tsx:196` renders `SetupPage`. |
| `handleMe` (`authapi.go:92`) | Return a fixed name (`"dev"`) when `NoAuth`, since no credential exists to name. |

Nothing else needs changing:

- `csrfGate` (`server.go:144`) only enforces the origin rule when a session cookie
  is present. With no auth there is no cookie, so it passes. The
  `application/json` content-type rule still applies and the SPA already complies.
- `checkWSOrigin` (`ws.go:35`) returns true when neither an explicit token nor a
  cookie is present — "no credentials at all", which the auth gate would normally
  reject. Under `NoAuth` that is every request, so WS upgrades from the phone's
  origin pass without touching `Origins`.
- CORS already reflects `*` for `/api/`.

The daemon keeps listening on `:port` (all interfaces). No bind-address flag.

## Frontend changes (`web/vite.config.ts`)

- `server.host: true` — bind `0.0.0.0` so the phone can reach `:5173`.
- `server.allowedHosts: true` — Vite rejects unknown `Host` headers otherwise, and
  the LAN hostname or IP the phone uses is not known ahead of time.

The existing proxy config is unchanged: `/api`, `/healthz`, and `/ws` already
target the daemon over `https` with `secure: false`.

## Tradeoff accepted

With `--dev` implying no-auth, the passkey and first-run setup flows can no longer
be exercised under `--dev`. Changes to `internal/auth` must be tested against a
non-`--dev` daemon pointed at a throwaway `MULTIMUX_DATA_DIR`.

## Tests

`internal/server`:

- A protected route (`GET /api/sessions`) succeeds with no credentials when
  `NoAuth` is set.
- A protected route still 401s when `NoAuth` is unset, with no credentials — the
  existing behaviour must be untouched.
- `/healthz` reports `setupPending: false` under `NoAuth` with no credentials, and
  `true` without it.
- `/api/auth/me` returns a name under `NoAuth`.

`cmd`:

- `--dev` produces a server config with `NoAuth` set.
- `--dev` refuses when `MULTIMUX_DATA_DIR` is unset or is the default path, with a
  message naming the fix.

## Docs

- `CLAUDE.md` "Dev loops": document the phone loop — export a scratch
  `MULTIMUX_DATA_DIR`, run `go run . serve --dev --port 8787`, run
  `MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev`, open
  `http://<lan-host>:5173` on the phone. Note that `--dev` means no auth.
- `README.md`: the dev-mode note must state the no-auth exposure explicitly,
  alongside the existing security-model section.
- `serveUsage` in `cmd/serve.go`: the `--dev` line must say "no authentication".
