# Security model

multimux is a single-user tool for a network you control. This document sets out
what it defends against, what it does not, and the design decisions behind the
auth and TLS machinery.

## Threat model and root of trust

The **root of trust is your local shell account**. Anyone who can run `multimux`
on the host, or read `~/.local/share/multimux`, already has full control — they
can reset credentials, read the SQLite database, and attach to your tmux
sessions directly. multimux does not try to defend against a local attacker who
owns your user account; it defends the daemon's **network surface**.

Assumptions:

- The daemon is reachable only over a trusted network (LAN, VPN, or Tailscale).
  It is **not** designed to face the public internet.
- tmux sessions run as your user; multimux gives its authenticated user the same
  shell access you have. There is no sandboxing between sessions.

## First-run bootstrap: the setup code

Before any passkey is registered the daemon is **setup-pending**: every API and
WebSocket route except the setup ceremony returns `403`. To register the first
passkey you need the **setup code** the daemon prints to its own log/console:

```
Open: https://your-host.local:8686/setup?code=ABC123
```

This is the same bootstrap pattern used by Jenkins (`initialAdminPassword`) and
Grafana (initial admin secret): the ability to read a secret written to the
server's local console proves you have local access, which is exactly the
privilege needed to legitimately claim the daemon. The code:

- is a random 6-character base32 value (~30 bits of entropy);
- expires **15 minutes** after it is minted;
- is invalidated after **5 consecutive failed attempts** (forcing a fresh code);
- is cleared permanently once the first passkey is registered.

Restart the daemon to mint a fresh code if the old one expired.

## Passkeys (WebAuthn)

Login is a WebAuthn passkey — Touch ID, Windows Hello, a hardware security key,
or a phone authenticator. There are no passwords anywhere in the system. You can
register additional passkeys from the **Settings → Passkeys** page (e.g. one per
device), and delete individual credentials there.

### Recovery with `auth reset`

If you lose access to every registered passkey, recover from the host's shell:

```
multimux auth reset --yes
```

This wipes **all** passkeys and **all** login sessions and returns the daemon to
setup-pending. Restart it (or wait for the next request to notice) and open the
new setup URL it prints to register a fresh passkey. Because this requires local
shell access, it respects the same root of trust as first-run setup. `auth reset`
without `--yes` refuses and explains what it would do.

## Session token storage

A successful passkey login mints a random 256-bit session token. The token is
returned to the browser exactly once, in an `HttpOnly`, `Secure`,
`SameSite=Strict` cookie. The server stores **only the SHA-256 hash** of the
token — the raw token is never written to disk, so a leak of the SQLite database
does not leak usable session tokens. Sessions expire on a **30-day sliding
window**: each use within the window renews the expiry. You can review and revoke
active sessions from **Settings → Sessions**.

## RP ID and the hostname-change warning

WebAuthn binds every passkey to a **Relying Party ID (RP ID)** — effectively the
daemon's hostname. **If the hostname changes, all existing passkeys stop
working** and you have to re-register (via `auth reset` and a fresh setup code).

Therefore: **choose a stable hostname before you register your first passkey.**
Good choices are a name that will not change, such as a
[Tailscale MagicDNS](https://tailscale.com/kb/1081/magicdns/) name
(`your-host.tailnet-name.ts.net`) or a fixed LAN name. The Settings → Daemon page
warns in red that changing the hostname invalidates all passkeys after restart.

Implementation notes:

- The daemon seeds its hostname from `os.Hostname()` on first run (stripping a
  trailing `.local`), and adds the `.local` form as an extra name automatically
  for single-label hostnames.
- Because `go-webauthn` rejects a bare single-label RP ID (anything without a dot
  except literal `localhost`), the daemon uses the `.local` form as the RP ID for
  a single-label hostname.
- You can add more names (for TLS SANs and origins) via **Extra SANs** in the
  Daemon settings; note these feed the certificate and allowed origins, while the
  RP ID itself stays fixed to the primary hostname.

## Cross-daemon bearer-token handoff

You can drive terminal tiles that live on **several** multimux daemons from a
single browser tab. The daemons never talk to each other — the browser holds a
token for each. The handoff works like an OAuth-style popup:

1. Your primary daemon's UI opens the other daemon's `#/connect?opener=<origin>`
   page in a popup.
2. That page verifies the opener origin is `https://` (or dev localhost), and,
   if you approve, calls its own `POST /api/auth/token` — which is authenticated
   by your existing cookie on that daemon — to **mint a fresh bearer token**.
3. The popup `postMessage`s the token back to the exact opener origin, and the
   primary tab stores it in `localStorage` for that server.

Thereafter, cross-daemon API calls and WebSocket upgrades carry the token
explicitly (`Authorization: Bearer …`, or `?token=` for WebSockets) — never a
cookie.

### Why cookies stay `SameSite=Strict`

Session cookies are `SameSite=Strict`, which means a browser will **never** attach
them to a cross-origin request. That is deliberate and is the whole reason the
bearer-token handoff exists: cross-daemon traffic must authenticate with an
explicit token, so a malicious page cannot ride your cookie to another daemon.
The trade-off is that cross-origin control needs the one-time popup handoff, which
is a fair price for closing the cross-site request surface entirely.

Because cross-origin API calls authenticate only with bearer tokens (never
cookies), the daemon can safely answer `/api/*` with
`Access-Control-Allow-Origin: *` without credentials — a reflected wildcard is
harmless when no ambient credential is ever attached.

## CSWSH (cross-site WebSocket hijacking) policy

WebSockets are not covered by CORS, so multimux guards the PTY and events
sockets explicitly:

- A **cookie-authenticated** upgrade must carry an `Origin` header that matches
  one of this daemon's own origins (`https://<hostname>:<port>` for each
  configured name). A mismatched origin is rejected with `403` before any session
  detail is revealed. This blocks a malicious page from opening a socket with your
  ambient cookie.
- A **token-authenticated** upgrade (the cross-daemon case) carries the secret
  explicitly, so any origin is allowed — that is exactly how a remote tile
  connects.
