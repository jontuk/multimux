# multimux

**multimux** is a single-binary web dashboard for your terminal sessions. It runs
a small Go daemon that manages [tmux](https://github.com/tmux/tmux) sessions and
serves an embedded React PWA: a grid of live terminal tiles you can open from any
browser on your private network — laptop, another desktop, or a tablet. Sessions
are real tmux sessions, so they survive daemon restarts, disconnects, and logout;
you reattach and your scrollback is still there. Access is gated by
[passkeys](https://fidoalliance.org/passkeys/) (WebAuthn) over TLS, with the
daemon minting its own name-constrained local certificate authority so you get
`https://` without a certificate warning and without a public CA.

> _Screenshot placeholder: the terminal grid with several tiles running Claude
> Code, a shell, and a build watcher side by side._

## Quick start

1. **Download** the release archive for your OS/arch from the
   [releases page](https://github.com/jontuk/multimux/releases) and put the
   `multimux` binary somewhere on your `PATH`.

   On macOS, Gatekeeper blocks downloaded binaries that aren't notarized with a
   message like *“Apple could not verify 'multimux' is free from malware”*.
   Remove the quarantine attribute to allow it to run:

   ```
   xattr -d com.apple.quarantine /path/to/multimux
   ```

   (Alternatively: System Settings → Privacy & Security → **Open Anyway** after
   the first blocked attempt.)

2. **Install the background service** (launchd on macOS, systemd user unit on
   Linux). This starts the daemon and keeps it running across logout/login:

   ```
   multimux service install
   ```

   On first run the daemon prints a one-time **setup URL** to its log, for
   example:

   ```
   === multimux setup ===
   Open: https://your-host.local:8686/setup?code=ABC123
   (code expires in 15 minutes; restart to regenerate)
   ```

   On macOS the log is at `~/.local/share/multimux/multimux.log`; on Linux use
   `multimux service status` / `journalctl --user -u multimux`. You can also run
   the daemon in the foreground with `multimux serve`, which prints the setup URL
   straight to the terminal.

3. **Open the setup URL** in a browser on the same machine or network. Your
   browser prompts you to create a passkey (Touch ID, Windows Hello, a security
   key, or a phone). That passkey becomes your login; the setup code is then
   consumed and the daemon is no longer setup-pending.

4. **Trust the local CA** so `https://` loads without a warning (once per client
   machine):

   ```
   multimux ca trust
   ```

   See [docs/install.md](docs/install.md) for the per-browser details on Linux.

The daemon listens on **port 8686** by default (configurable) and stores its data
under `~/.local/share/multimux` (override with the `MULTIMUX_DATA_DIR`
environment variable).

## Commands

```
multimux serve                              run the daemon in the foreground
multimux service install|uninstall|status   manage the launchd/systemd user service
multimux auth reset --yes                    wipe credentials and return to setup-pending
multimux ca trust                            install the local CA into the OS trust store
multimux --version                           print version
```

`serve` accepts `--port <n>` (overrides the stored setting) and `--behind-proxy`
(plain HTTP on localhost, trusting `X-Forwarded-*` — see
[docs/proxy.md](docs/proxy.md)).

## Security model (the honest version)

multimux is a **local, single-user tool** and its security posture reflects that.

- **Private network by default.** The daemon binds all interfaces on port 8686
  over TLS, but it is designed to be reachable only across a network you control
  (LAN, VPN, or [Tailscale](https://tailscale.com/)). Do not expose it to the
  public internet. The real root of trust is your local shell: anyone who can run
  `multimux` on the host, or read its data directory, already controls it.

- **Authentication is passkeys plus server-side sessions.** Login is a WebAuthn
  passkey — there are no passwords. A successful login sets an `HttpOnly`,
  `Secure`, `SameSite=Strict` session cookie. The server stores **only a SHA-256
  hash** of each session token, never the token itself, and sessions expire on a
  30-day sliding window.

- **The local CA is name-constrained.** The daemon generates its own certificate
  authority and signs a short-lived leaf certificate for its own hostnames. That
  CA is **X.509 name-constrained** to exactly those hostnames (and their
  subdomains). Trusting it — `multimux ca trust` — therefore lets it vouch for the
  multimux host and **nothing else**: it is cryptographically unable to sign a
  certificate for `bank.com`, your employer's intranet, or any other domain. This
  is what makes it reasonable to trust on a shared or managed machine. See
  [docs/security.md](docs/security.md).

- **First-run bootstrap uses a setup code**, printed to the daemon's own log/
  console (the same pattern as Jenkins' or Grafana's initial-admin secrets). Only
  someone who can read the host's log can complete initial registration. The code
  expires in 15 minutes and is invalidated after 5 failed attempts.

### Non-goals (explicitly out of scope)

- **No multi-user support.** One person, one set of passkeys. There are no
  accounts, roles, or permissions.
- **No Windows.** macOS and Linux only.
- **No daemon-to-daemon communication.** You can drive tiles on several daemons
  from one browser (see [docs/security.md](docs/security.md) on the bearer-token
  handoff), but the daemons never talk to each other — the coordination happens
  entirely in your browser.
- **No dedicated phone/native app in v1.** The PWA works in a mobile browser, but
  a first-class phone experience is not a goal for this release.
- **Contributions are not accepted.** This is a personal project shared as-is.
  Issues and PRs may go unanswered; fork freely.

## License

[MIT](LICENSE). © 2026 Jon Turner.

## Documentation

- [docs/install.md](docs/install.md) — binary install, CA trust per platform,
  service management, upgrades.
- [docs/security.md](docs/security.md) — threat model, passkey lifecycle, session
  storage, RP-ID/hostname warnings, cross-daemon design.
- [docs/proxy.md](docs/proxy.md) — running behind Caddy or Tailscale Serve.
- [docs/work-network.md](docs/work-network.md) — managed devices, corporate DNS,
  and recovery.
