# TODO — hostname bootstrap & local-dev fixes

Findings from dogfooding first-run and the dev loop on a machine where mDNS
doesn't resolve (Tailscale-only reachability). Ordered roughly by impact.

## 1. ✅ DONE — `serve --hostname <name>` — settable identity before first login

**Problem.** The daemon derives its hostname from `os.Hostname()`
(`cmd/serve.go`), and on many machines that name is unreachable from a
browser: mDNS blocked or disabled, corporate DNS, Tailscale-only setups. There
is currently **no way to set the hostname before first login** — the Settings
page sits behind a passkey login, registering a passkey needs a reachable
setup URL, and the setup URL uses the broken hostname. Chicken-and-egg; the
only escape today is hand-editing SQLite (now documented in the README, but it
shouldn't be the answer).

**Fix.**
- Add `serve --hostname <name>`, persisted to the `hostname` setting exactly
  like `--port` (and consider a `MULTIMUX_HOSTNAME` env var so service units
  can carry it).
- Validate up front: the name must contain a dot or be literal `localhost`
  (go-webauthn rejects other dotless RP IDs — that constraint is currently
  implicit in the `.local` fallback).
- The hostname is the WebAuthn RP ID: if credentials already exist and the
  flag would change the RP ID, **refuse with a clear message** pointing at
  `multimux auth reset --yes` rather than silently stranding the passkeys.
- PKI already self-heals (`pki.Ensure` regenerates CA + leaf when the name
  set changes); make sure the "re-run `multimux ca trust`" warning is loud.

## 2. ✅ DONE (core) — Print setup/listen URLs users can actually open

> Done: dotted-first URL ordering, one setup line per name, `--hostname` hint,
> listen line uses most-resolvable origin. Skipped nice-to-haves: seeding from
> macOS `LocalHostName` would change the PKI name set on existing installs
> (CA regen churn), and `--hostname` now covers the Tailscale/MagicDNS case.

**Problem.** The setup banner prints `origins[0]`, which is the **bare
single-label hostname** (`https://myhost:8686/...`) — the least likely form to
resolve from another device, and inconsistent with the README's `.local`
examples.

**Fix.**
- Prefer the dotted form (`.local` or configured FQDN) in the printed URL, or
  print one line per name.
- Append a hint: `if this doesn't resolve from your browser, restart with
  --hostname <name>` (once item 1 exists).
- Nice-to-have: on macOS prefer `LocalHostName` (what Bonjour actually
  advertises) over the kernel hostname; if a Tailscale interface is up,
  suggest the MagicDNS FQDN.

## 3. ✅ DONE — First-run ordering: CA trust must precede passkey registration

**Problem.** Browsers (Chrome for certain) refuse WebAuthn ceremonies on pages
served with an untrusted certificate, so "open the setup URL, then trust the
CA" fails at the passkey prompt. README quick start is fixed (trust is now
step 3, setup step 4), but `docs/install.md` still registers (§2) before
trusting (§3).

**Fix.** Reorder `docs/install.md` §2/§3 with the same one-line rationale.
Also fix `docs/work-network.md`, which tells users to change the hostname "in
the Daemon settings **before** registering your first passkey" — the Settings
UI is unreachable before login; point it at `--hostname` once item 1 lands.

## 4. ✅ DONE — `serve --dev` — make the Vite hot-reload loop actually work

> Done: `--dev` forces RP ID `localhost`, appends `http://localhost:5173` +
> `https://localhost:<port>` to origins, prints DEV MODE banner (incl. Safari
> caveat), refuses when credentials exist. Verified at HTTP surface (rp.id,
> refusal, banner) and, after item 5 landed, full browser acceptance:
> register → login → grid → live PTY → HMR at http://localhost:5173.

**Problem.** The documented two-terminal dev loop cannot authenticate at
`http://localhost:5173`:
- browser-side: RP ID is the daemon hostname, not a registrable suffix of
  `localhost` → `SecurityError` before any request is made;
- server-side: `http://localhost:5173` is never in `RPOrigins`
  (`cmd/serve.go computeOrigins`) → registration/login rejected anyway;
- `checkWSOrigin` (`internal/server/ws.go`) rejects cookie-carrying WS
  upgrades from the Vite origin → no events feed, no terminals.

Result: the hot-reload UI dead-ends at the login wall; only unauthenticated
screens and vitest work. (README now says so honestly.)

**Fix.** A `serve --dev` flag that sets RP ID to `localhost` and appends
`http://localhost:5173` (and `https://localhost:8686`) to the allowed
origins — that one change satisfies all three checks. Log a loud "DEV MODE"
banner; never allow it combined with a normal install (throwaway
`MULTIMUX_DATA_DIR` only, or at least refuse when credentials exist).
Caveat to document: Safari does not treat `http://localhost` as trustworthy
for `Secure` cookies — the dev loop targets Chrome/Firefox.

**Acceptance.** register → login → grid → live PTY all work at
`http://localhost:5173` with hot reload, no sqlite surgery, no CA trust.

## 5. ✅ DONE — Vite proxy target is hardcoded

> Done: `MULTIMUX_DEV_TARGET` env var (default unchanged), README dev section
> shows the two-command collision-free loop. Item 4's full browser acceptance
> ran on top of this: register → login → grid → live PTY → HMR at :5173, all
> green with a virtual-authenticator Chrome session.

**Problem.** `web/vite.config.ts` pins `https://localhost:8686`. A real
install already listening on 8686 collides with the dev daemon, and
`serve --port` can't be followed.

**Fix.** Read the target from an env var (e.g.
`MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev`) with the current value
as default; mention the port collision in the README dev section.

## 6. ✅ DONE — Latent bug: WS origin check fires even when a valid token is presented

> Done: explicit token (Authorization / ?token=) now decides the WS origin
> rule AND wins authentication over the cookie (`auth.ExplicitToken`,
> explicit-first `TokenFromRequest`), so a garbage token cannot ride a valid
> cookie past the skipped check. Regression tests: cookie+token+foreign origin
> (was 403, now passes), garbage-token+cookie (401), cookie-only CSWSH guard
> intact. Repro'd end-to-end on a live TLS daemon.

**Problem.** `checkWSOrigin` treats "request has a session cookie" as "this is
cookie auth" and then requires a same-origin `Origin` header — but browsers
attach cookies to WebSockets unconditionally (there is no `credentials: omit`
for WS). Two daemons whose names share a site are affected: on one tailnet,
`a.<tailnet>.ts.net` and `b.<tailnet>.ts.net` are same-site (`ts.net` is on
the Public Suffix List, so the site is `<tailnet>.ts.net`), meaning
`SameSite=Strict` cookies still flow between them. A cross-daemon tile then
sends daemon B its cookie plus daemon A's `Origin` → **403 despite a valid
`?token=`** — breaking the flagship multi-daemon grid for same-tailnet
daemons. Confirmed by code reading; needs an end-to-end repro.

**Fix.** When a syntactically valid bearer token is present, authenticate by
the token and ignore the cookie (or accept the upgrade if *either* check
passes). Add a regression test with cookie + token + foreign origin.

## 7. ✅ DONE — Docs cleanup once the above lands

- Shrink README "If the setup URL doesn't resolve" to just `--hostname`
  (drop the sqlite recipe).
- Restore a real two-terminal dev loop in README §Developing built on
  `serve --dev`.
- `docs/install.md` / `docs/work-network.md` updates from item 3.
- State the dotted-name / RP-ID constraint in one canonical place
  (`docs/security.md` mentions it; install/work-network should link it).
