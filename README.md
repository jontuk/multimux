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

## Install

macOS or Linux, one line:

```sh
curl -fsSL https://raw.githubusercontent.com/jontuk/multimux/main/install.sh | sh
```

It detects your OS/arch, downloads the latest release, verifies its checksum,
and installs the `multimux` binary to `/usr/local/bin` (using `sudo` if that
directory isn't writable). Override with environment variables:

```sh
# install a specific version to a custom directory
MULTIMUX_VERSION=v0.2.0 MULTIMUX_INSTALL_DIR="$HOME/.local/bin" \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/jontuk/multimux/main/install.sh)"
```

**Manual download** — grab the archive for your OS/arch from the
[releases page](https://github.com/jontuk/multimux/releases), extract the
`multimux` binary, and put it on your `PATH`. On macOS, Gatekeeper blocks
binaries downloaded via a browser (*“Apple could not verify 'multimux' is free
from malware”*); clear the quarantine attribute with
`xattr -d com.apple.quarantine /path/to/multimux`, or use System Settings →
Privacy & Security → **Open Anyway**. (Binaries fetched by the install script or
`curl` aren't quarantined, so this only affects browser downloads.)

## Quick start

1. **Install the background service** (launchd on macOS, systemd user unit on
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

   View the daemon log with `multimux service logs` (less on
   `~/.local/share/multimux/multimux.log` on macOS, `journalctl --user -u
   multimux` on Linux). You can also run
   the daemon in the foreground with `multimux serve`, which prints the setup URL
   straight to the terminal.

2. **Trust the local CA** — and do it **before** opening the setup URL:
   browsers refuse WebAuthn (passkey creation) on pages served with an
   untrusted certificate, so registration fails if you skip this. The CA
   exists as soon as the daemon has started once. Run once per client machine:

   ```
   multimux ca trust
   ```

   See [docs/install.md](docs/install.md) for the per-browser details on Linux.

3. **Open the setup URL** in a browser on the same machine or network. Your
   browser prompts you to create a passkey (Touch ID, Windows Hello, a security
   key, or a phone). That passkey becomes your login; the setup code is then
   consumed and the daemon is no longer setup-pending.

   The URL uses your machine's hostname — **check that it resolves from the
   device you're browsing on** (`ping your-host.local`). If it doesn't, fix
   the hostname *first* (next section), so you don't register a passkey
   against a name you can't reach.

The daemon listens on **port 8686** by default (configurable) and stores its data
under `~/.local/share/multimux` (override with the `MULTIMUX_DATA_DIR`
environment variable).

### If the setup URL doesn't resolve

The daemon derives its identity from the OS hostname, plus a `.local` (mDNS)
form when the hostname has no dot. On plenty of setups neither name is
reachable from a browser: mDNS is blocked or disabled, or the machine is only
reachable through Tailscale or internal DNS. Restart with a name that does
resolve:

```bash
multimux serve --hostname your-machine.your-tailnet.ts.net
```

The name is persisted (the `MULTIMUX_HOSTNAME` environment variable works too,
handy for service units) and must contain a dot or be literal `localhost` — it
doubles as the WebAuthn **RP ID** your passkey is bound to; see
[docs/security.md](docs/security.md#rp-id-and-the-hostname-change-warning).
Good choices: a Tailscale MagicDNS FQDN or a name your internal DNS serves —
see [docs/work-network.md](docs/work-network.md). The daemon regenerates its
CA and leaf certificate for the new name, so re-run `multimux ca trust`.
Do this **before** registering your passkey: once passkeys exist, `--hostname`
refuses any change that would alter the RP ID and points you at
`multimux auth reset --yes`.

## Commands

```
multimux serve                              run the daemon in the foreground (--port, --hostname, --dev, --behind-proxy)
multimux service install|uninstall|status|logs   manage the launchd/systemd user service
multimux auth reset --yes                    wipe credentials and return to setup-pending
multimux ca trust                            install the local CA into the OS trust store
multimux --version                           print version
```

`serve` accepts `--port <n>` (overrides the stored setting) and `--behind-proxy`
(plain HTTP on localhost, trusting `X-Forwarded-*` — see
[docs/proxy.md](docs/proxy.md)).

The daemon runs its sessions on a **private tmux server** (socket name
`multimux`), so it never touches your personal tmux sessions. If you upgraded
from a version that used the default tmux server, existing `mm-*` sessions
stay on the default server; reattach to them directly with
`tmux attach -t <name>` and let multimux create new sessions on its own
socket (`tmux -L multimux ls` lists them).

## Security model 

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

## Developing

Prerequisites: Go, Node + pnpm, tmux.

**Run a dev daemon** in the foreground with a throwaway data dir so you don't
touch your real install (passkeys, sessions, CA). Use a fresh dir each run —
`--dev` refuses to start against a data dir that already has passkeys:

```bash
export MULTIMUX_DATA_DIR="/tmp/multimux-dev-$(date +%s)"
go run . serve
```

The dev daemon is a full install as far as auth is concerned: the same
hostname rules apply (see *If the setup URL doesn't resolve* above —
`--hostname` persists into the throwaway data dir), the CA needs trusting
(`go run . ca trust` with the same `MULTIMUX_DATA_DIR` exported), and it prints a
setup URL on which you register a throwaway passkey — **at the daemon's own
`https://` origin**, not through Vite. For the frontend hot-reload loop none
of that is needed; see below.

**Backend loop.** Work against the daemon's own URL. Go changes: restart
`go run . serve`. Frontend changes: `cd web && pnpm build`, then restart the
daemon — `go run` re-embeds `web/dist` on every start.

**Frontend hot reload.** Two terminals:

```bash
# terminal 1 — dev daemon; --dev forces the RP ID to localhost and allows the Vite origin
export MULTIMUX_DATA_DIR="/tmp/multimux-dev-$(date +%s)"
go run . serve --dev --port 8787

# terminal 2 — Vite, proxying /api, /healthz and /ws to the dev daemon
cd web && pnpm install && MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev
```

Register a throwaway passkey at `http://localhost:5173/setup?code=…` (the
daemon prints the code) and the full app — login, grid, live terminals —
works at `http://localhost:5173` with hot reload. No CA trust needed: the
browser talks plain HTTP to Vite. Caveats:

- Chrome/Firefox only — Safari does not treat `http://localhost` as
  trustworthy for `Secure` cookies, so login won't stick there.
- `--dev` refuses to run against a data dir that already has passkeys; the
  timestamped `MULTIMUX_DATA_DIR` above gives you a fresh one per shell.
- Each dev data dir uses its own private tmux server, so its `mm-*` sessions
  cannot collide with another dev run or the installed daemon.
- If nothing else is listening on 8686 you can drop `--port` and
  `MULTIMUX_DEV_TARGET` (the proxy target defaults to
  `https://localhost:8686`, see `web/vite.config.ts`).

**Building the real binary.** The Go binary embeds `web/dist` (`go:embed` in
`main.go`), so build the frontend first or your binary ships stale assets:

```bash
cd web && pnpm build && cd ..
go build -o multimux .
```

**Testing an install-like setup** without touching your real service: run the
freshly built binary in the foreground (`./multimux serve`) with
`MULTIMUX_DATA_DIR` pointed somewhere disposable. To exercise the actual
launchd/systemd path, `multimux service install` picks up whatever binary you
point it at — but it will replace your existing unit, so uninstall/reinstall
deliberately.

**Before committing / releasing:**

```bash
./verify.sh   # gofmt, go vet, go test, pnpm lint + test + build
```

CI (`.github/workflows/ci.yml`) runs the same checks on macOS and Linux.
Releases are cut by pushing a `v*` tag; goreleaser
(`.github/workflows/release.yml`) builds the archives for the releases page.

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
