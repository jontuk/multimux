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

<img width="1706" height="875" alt="image" src="https://github.com/user-attachments/assets/f76b818b-55e1-407a-b630-b4239542a546" />

**Contents** — [Install](#install) · [Quick start](#quick-start) ·
[Choosing a hostname](#choosing-a-hostname) · [Trusting the CA](#trusting-the-ca) ·
[Android/mobile](#android-phones-ca-trust) ·
[Another machine](#connecting-from-another-machine) · [Using it](#using-it) ·
[Install as an app](#install-as-an-app-pwa) · [Commands](#commands) ·
[Service](#running-as-a-service) · [Upgrading](#upgrading) ·
[Settings](#settings) · [Security model](#security-model) ·
[Troubleshooting](#troubleshooting) · [Developing](#developing)

## Install

macOS or Linux (`amd64` or `arm64`); there is no Windows build. **tmux must be
installed** (`brew install tmux`, `apt install tmux`, …) — the daemon checks for
it at startup and refuses to start without it.

```sh
curl -fsSL https://raw.githubusercontent.com/jontuk/multimux/main/install.sh | sh
```

The script detects your OS/arch, downloads the latest release, verifies its
checksum, and installs the `multimux` binary to `/usr/local/bin` (using `sudo` if
that directory isn't writable). Override with environment variables:

## Quick start

1. **Run the daemon once in the foreground with `--trust-ca`.** This picks the
   hostname, generates the local CA *and* installs it into this machine's trust
   store, then prints the setup URL:

   ```
   multimux serve --hostname your-machine.example.com --trust-ca
   ```

   ```
   === multimux setup ===
   Open: https://your-machine.example.com:8686/setup?code=ABC123
   Android trust: https://your-machine.example.com:8686/trust?return=%2Fsetup%3Fcode%3DABC123
   CA SHA-256: AA:BB:CC:…
   (code expires in 15 minutes; restart to regenerate)
   ```

   `--hostname` is optional — leave it off and the daemon uses your OS hostname
   (plus a `.local` mDNS form). Either way the name is persisted, so later runs
   and `multimux service install` reuse it. Pick a name that resolves from the
   device you'll browse on and get it right *before* registering a passkey; see
   [Choosing a hostname](#choosing-a-hostname). `--trust-ca` does the same work
   as `multimux ca trust`, and failing to trust is non-fatal: the daemon still
   starts and tells you.

   Trusting the CA matters **before** you open the setup URL: browsers refuse
   WebAuthn (passkey creation) on pages served with an untrusted certificate, so
   registration fails if you skip it. Android is the bootstrap exception: on a
   LAN, VPN, or tailnet you control, bypass Chrome's certificate warning once,
   then open the **Android trust** `/trust` URL printed by the daemon and install
   and verify the CA before registering a passkey. Do not bypass the warning on
   a public or otherwise untrusted network; see
   [Android phones: CA trust](#android-phones-ca-trust).

2. **Open the setup URL** in a browser on the same machine or network. Your
   browser prompts you to create a passkey (Touch ID, Windows Hello, a security
   key, or a phone). That passkey becomes your login; the setup code is then
   consumed and the daemon is no longer setup-pending. If the code expired,
   restart the daemon to print a fresh one.

3. **Install the background service** (launchd on macOS, systemd user unit on
   Linux) so the daemon keeps running across logout/login. Stop the foreground
   daemon first:

   ```
   multimux service install
   ```

   It reuses the hostname, port, and CA you just set up. See
   [Running as a service](#running-as-a-service).

The daemon listens on **port 8686** by default and stores everything under
`~/.local/share/multimux` — a SQLite database (`multimux.db`), the PKI material
(`pki/`), and on macOS the service log. Override the location with the
`MULTIMUX_DATA_DIR` environment variable.

## Choosing a hostname

The hostname is the **WebAuthn RP ID** your passkeys are bound to, as well as
the TLS certificate's name. Two consequences:

- It must contain a dot or be literal `localhost` (`go-webauthn` rejects other
  single-label names). For a single-label OS hostname the daemon uses the
  `.local` form as the RP ID.
- **Changing it invalidates every registered passkey.** Choose a stable name
  *before* registering the first one. Once passkeys exist, `--hostname` refuses
  any change that would alter the RP ID and points you at
  `multimux auth reset --yes`; the Settings → Daemon page warns likewise.

By default the daemon derives its identity from the OS hostname plus a `.local`
(mDNS) form. On plenty of setups neither name is reachable from a browser: mDNS
is blocked or disabled on managed/corporate networks, or the machine is only
reachable through Tailscale or internal DNS. Restart with a name that does
resolve, in rough order of preference:

```sh
# 1. a Tailscale MagicDNS name — resolves anywhere on your tailnet, and stable
multimux serve --hostname your-machine.your-tailnet.ts.net --trust-ca

# 2. a name your internal/corporate DNS already serves
multimux serve --hostname mux.corp.example.com --trust-ca
```

3. As a last resort, map the daemon's name to its IP in each client's
   `/etc/hosts`. The name you put there must be one of the daemon's configured
   names, or TLS validation fails.

The name is persisted (the `MULTIMUX_HOSTNAME` environment variable works too,
handy for service units). Additional names go under **Extra SANs** on the
Settings → Daemon page once you're logged in; they become both TLS SANs and
allowed origins, while the RP ID stays fixed to the primary hostname.

Changing the hostname/SAN set makes the daemon **regenerate its CA**, because
the CA's name constraints are baked to that set — it prints a warning telling
you to re-run `multimux ca trust` on every client.

## Trusting the CA

To load `https://` without a warning — and for passkeys to work at all — install
the daemon's name-constrained CA into the OS trust store of **each client
machine** that opens the UI.

```sh
multimux ca trust                          # trust THIS host's own CA
multimux ca trust --remote user@daemon-host   # trust a remote daemon's CA (run on the client)
```

`--remote` copies `~/.local/share/multimux/pki/ca.pem` from the daemon host over
`scp` and installs it, using your existing SSH access as the transport's trust —
no chicken-and-egg of fetching a CA over an untrusted TLS connection. Add
`--remote-path` if the remote daemon runs under a custom `MULTIMUX_DATA_DIR`.
Before installing, multimux prints the CA's subject and the hostnames its name
constraints permit, so you can confirm what you are trusting.

- **macOS** adds the CA to your **login keychain** (no `sudo`; Keychain Access
  may prompt once). Verify with `curl https://your-host:8686/healthz`, which
  should succeed without `-k`.
- **Linux** copies the CA into the distribution's anchor directory and refreshes
  the system trust store, which needs `sudo`: Debian/Ubuntu
  `/usr/local/share/ca-certificates/multimux-ca.crt` + `update-ca-certificates`;
  Fedora/RHEL `/etc/pki/ca-trust/source/anchors/multimux-ca.pem` +
  `update-ca-trust`. If neither anchor directory exists, `ca trust` says so and
  prints the path to the CA file so you can install it manually.

**Linux browser caveat.** Firefox and Chromium on Linux do not use the system
trust store — they keep their own
[NSS](https://firefox-source-docs.mozilla.org/security/nss/index.html) database.
After `multimux ca trust`, also import the CA there:

```sh
certutil -A -n multimux -t "C,," -i ~/.local/share/multimux/pki/ca.pem -d sql:$HOME/.pki/nssdb
```

(`certutil` is in the `libnss3-tools` / `nss-tools` package.) Chrome and Safari
on macOS, and Chrome on Linux with non-NSS profiles, use the OS store and need
no extra step.

### Android phones: CA trust

Android needs the daemon's CA in its user trust store before Chrome can create
or use passkeys, or install the multimux PWA. On first run, the daemon prints an
**Android trust** URL and the CA's full SHA-256 fingerprint. Use the guided web
download only on a LAN, VPN, or tailnet you control:

1. Bypass Chrome's certificate warning once and open the printed `/trust` URL.
2. Tap **Download CA certificate**. The public certificate downloads as
   `multimux-ca.crt`.
3. On a Pixel or stock Android device, open **Settings → Security & privacy →
   More security settings → Encryption & credentials → Install a certificate →
   CA certificate**. Select `multimux-ca.crt` and approve Android's warning.
   On Samsung, use **Settings → Security and privacy → More security settings →
   Credential storage → Install from device storage → CA certificate** instead.
4. In Android's installed-certificate details, compare the SHA-256 fingerprint
   with the value printed in the daemon's own console or service log. The
   initially untrusted web page is not an authenticated reference. **Stop and
   remove the certificate if the fingerprints do not match.**
5. Return to Chrome and tap **Reload and check trust**. Once the page reports
   that Android trusts the daemon, continue to the original setup or login URL.

The `/ca.crt` and `/ca/info` routes are intentionally public so a phone can
bootstrap trust. They expose only the public certificate; the private
`pki/ca.key` is never exported.

If Chrome will not open the untrusted page, or you do not fully control the
network, copy `pki/ca.pem` from the daemon through an already trusted SSH
connection to a computer, rename it `multimux-ca.crt`, then transfer it to the
phone over USB or Quick Share. For the default data directory:

```sh
scp user@daemon-host:~/.local/share/multimux/pki/ca.pem multimux-ca.crt
```

Use the corresponding path if the daemon has a custom `MULTIMUX_DATA_DIR`.
Managed-device policy may forbid user-added CAs entirely; multimux cannot bypass
that policy or weaken certificate validation, so browser access from such a
device is not supported.

To remove the CA on Pixel or stock Android, open **Settings → Security &
privacy → More security settings → Encryption & credentials → User
credentials**, select the multimux CA, and remove it. On Samsung, open
**Settings → Security and privacy → More security settings → Credential storage
→ User certificates**, select the multimux CA, and remove it. OEM labels can
vary; use Settings search for “user certificates” or “user credentials” if the
path is different.

Whenever the daemon regenerates its CA — for example after its hostname set
changes or near CA expiry — repeat the download, fingerprint comparison, and
installation on every phone. Remove the old CA after the new one is trusted.

**Managed devices.** Many MDM-managed machines forbid custom root CAs, or flag
them for review. Name constraints make multimux's CA far less objectionable: it
is cryptographically unable to sign a certificate for any host outside its own
name set, so trusting it cannot be used to intercept your bank or your
employer's intranet. Where a policy allows *constrained* roots, this is the CA
to point at. multimux always terminates TLS itself, so a device that can install
no custom root at all cannot reach it in a browser.

## Connecting from another machine

The common remote setup is a daemon on a cloud box (a VPS, or an EC2/OCI
instance on your tailnet) driven from a laptop. The catch: running
`multimux ca trust` on the laptop trusts the *laptop's* CA, which is a different
key from the one the remote daemon serves.

1. **On the remote box**, pick a stable hostname up front and start the daemon,
   then install the service:

   ```sh
   multimux serve --hostname <box>.<tailnet>.ts.net   # persists the hostname
   # Ctrl-C once the setup URL prints, then:
   multimux service install
   ```

2. **On the client**, trust the remote box's CA:

   ```sh
   multimux ca trust --remote <user>@<box>.<tailnet>.ts.net
   ```

3. Open `https://<box>.<tailnet>.ts.net:8686/` on the client and register a
   passkey. On Linux clients, remember the NSS caveat above.

To drive tiles from **several daemons in one browser tab**, add the others under
Settings → **Servers** (origin, e.g. `https://other-box.ts.net:8686`, plus a
name), then click **Connect**: a popup on that daemon asks you to approve, and
posts a bearer token back to this tab. The daemons never talk to each other —
the coordination is entirely in your browser, and each remote daemon still needs
its own passkey login and its CA trusted on this client.

## Using it

**Launching.** The header launcher starts a session from a **tool** (the command
to run) and a **directory**, both managed on the Settings page. On first run the
daemon seeds one tool (`zsh` on macOS, `bash` on Linux) and your home directory,
so you can launch immediately. The optional **subdir** field is a path relative
to the chosen directory, so one directory entry covers a whole tree of repos
instead of needing one entry per project; the subdirectory must already exist
and must stay inside the chosen directory. With more than one server configured,
a server picker appears first.

**The grid.** Tiles are laid out in a column count you set with the header
stepper; rows follow. Each tile header shows the session id, tool name,
directory, the git branch with a colour dot (clean / modified / untracked), and
a GitHub link when the directory is a GitHub checkout. Double-click a tile's
title to rename the session; the name is a display label only — the tmux
session keeps its `mm-{id}` name — and clearing it restores the tool name.

| Action | How |
| --- | --- |
| Maximize / restore a tile | double-click its header, or `Escape` to restore |
| Move a tile | drag it onto another cell |
| Remove a tile from the grid | − (the tmux session keeps running) |
| Terminate the session | ✕ (kills the tmux session) |
| Re-attach a running session | pick it from the `+ #id` buttons in the header, or from an empty tile |

A session can only occupy one tile at a time. Removing a tile leaves the session
running; it reappears in the header's unplaced list.

**Mobile session view.** At a viewport width of **560 CSS pixels or less**,
multimux switches to a portrait-first, read-only session switcher. It shows all
running sessions already placed in the desktop grid first, in grid order, then
appends other running sessions in stable server/session order. Swipe
horizontally on the compact session header to move between them; terminal touch
input does not switch sessions, and swiping at the first or last session does
not wrap.

Only the selected terminal is mounted. The mobile view has no launcher and no
rename, move, remove, or terminate controls; use a wider viewport for those
actions. Selection is temporary, and neither switching sessions nor entering
or leaving mobile view changes the saved desktop grid layout. Viewports wider
than 560 pixels — including most tablets — keep the full grid.

**In the terminal.** Sessions run with tmux mouse mode on, so the wheel/trackpad
scrolls tmux's copy-mode with 50 000 lines of scrollback. Because tmux owns
click-drag, hold **Shift** (or Option on macOS) to make a native browser
selection. A copy-mode yank reaches your system clipboard via OSC 52.
**Shift+Enter** is sent as an extended key rather than a bare newline. When the
same session is open on several machines, the window size follows whoever typed
last, so switching machines and typing reclaims it at that machine's size.

**Settings tabs** — Tools, Directories (both drag-reorderable, with arrow keys
for keyboard and touch; the saved order is the launcher's order), Passkeys (add
per-device passkeys, revoke any but the last), Sessions (review and revoke login
sessions), Servers, Daemon (hostname, extra SANs, port, version), Appearance,
Preferences.

## Install as an app (PWA)

multimux is a Progressive Web App: install it from your browser and it runs in
its own window with no address bar. Installing changes the window, not the
connection — there is no offline mode, and the installed app needs the daemon
reachable exactly as a browser tab does.

In **Chrome** (desktop), open the daemon URL and click the install icon in the
address bar — or **⋮ → Cast, save, and share → Install page as app…**.
**Firefox** does not support installing PWAs on the desktop; multimux still
works in a Firefox tab.

Each daemon installs as a **separate app**, keyed to its origin, so you can
install one per host. To tell them apart, give each daemon a distinct **host
label** and **accent colour** under Settings → **Appearance**: the label becomes
the app name and the header badge, and the accent tints the icon, the header,
and the browser theme colour. Reinstall the app after changing either to pick up
the new name and icon.

Firefox also can't use your Chrome/Safari platform passkey — passkeys don't roam
between browsers' platform stores, and Firefox on macOS doesn't integrate with
Touch ID / iCloud Keychain at all. To log in from Firefox, use a hardware
security key or phone-via-QR (both work in any browser), or register a dedicated
Firefox passkey under Settings → Passkeys.

## Commands

```
multimux serve                               run the daemon in the foreground
multimux service install|uninstall|upgrade|status|logs   manage the launchd/systemd user service
multimux ca trust [--remote HOST]            install a multimux CA into the OS trust store
multimux config list|get|set                 read and change user-configurable settings
multimux auth reset --yes                    wipe credentials and return to setup-pending
multimux help [command]                      detailed help for a command
multimux --version                           print version
```

`serve` takes `--hostname <name>`, `--trust-ca`, `--port <n>` (persisted), and
`--dev` (throwaway installs only — see [Developing](#developing)).
`multimux help serve` prints the full flag list.

Environment variables: `MULTIMUX_DATA_DIR` (data directory, default
`~/.local/share/multimux`), `MULTIMUX_HOSTNAME` (default for `--hostname`),
`MULTIMUX_INSTALL_DIR` and `MULTIMUX_VERSION` (honoured by `install.sh` and
`service upgrade`).

## Running as a service

```sh
multimux service install     # write the unit, enable it, start the daemon
multimux service status
multimux service logs
multimux service uninstall   # leaves data and tmux sessions intact
```

- **macOS**: a LaunchAgent labelled `com.jontuk.multimux` in
  `~/Library/LaunchAgents/`, with `RunAtLoad` and `KeepAlive`. Stdout/stderr go
  to `~/.local/share/multimux/multimux.log`, which `service logs` pages.
  `install` is idempotent.
- **Linux**: a systemd **user** unit `multimux.service` in
  `~/.config/systemd/user/`, enabled with `Restart=on-failure` and
  `KillMode=process` so stopping, restarting, or upgrading the service signals
  only the daemon and leaves your tmux sessions running. `service logs` runs
  `journalctl --user -u multimux`. For the daemon to keep running after you log
  out, enable lingering once:

  ```sh
  sudo loginctl enable-linger $USER
  ```

The unit runs a bare `multimux serve` with no flags, so change the port or extra
SANs on the Settings → Daemon page (they live in SQLite), or persist a port
first with `multimux serve --port <n>`.

**Environment capture.** `service install` copies `MULTIMUX_DATA_DIR` and
`MULTIMUX_HOSTNAME` out of the installing shell into the unit, because launchd
and systemd start the daemon with none of your shell's environment. Without this
a custom data directory is silently lost and the service comes up on the default
one — a fresh database, a fresh CA, and a setup-pending daemon. So run
`service install` from a shell where those variables are set as you want them,
and remember the captured values are a **snapshot**: change a variable later and
you must re-run `multimux service install`.

## Upgrading

```sh
multimux service upgrade
```

That pipes the same `install.sh` used for a first install into `sh` (fetching
the latest release binary for your OS/arch), then rewrites the unit to point at
the new binary and restarts the daemon onto it. It needs network access and may
prompt for sudo if the install directory isn't writable. With no unit installed
it just replaces the binary and says so — it will not install a service you
never asked for.

To upgrade by hand: replace the binary, then `multimux service uninstall &&
multimux service install` (macOS) or `systemctl --user restart multimux`
(Linux).

Your data directory, passkeys, and CA are untouched by an upgrade. There are no
schema migrations to run. If the CA's hostname set has changed — or the CA is
within 30 days of its 10-year expiry, in which case it is renewed automatically
— the daemon prints a warning telling you to re-run `multimux ca trust`.

**Linux units installed before `KillMode=process` existed** kill the tmux server
(and every session in it) when the service stops or restarts. Re-run
`multimux service install` once to rewrite the unit.

## Settings

Some behaviour is configurable from the shell or from the Settings page's
**Preferences** tab. Both write to the same daemon database.

```sh
multimux config list                          # every setting and its value
multimux config get confirm-terminate         # one value, bare, for scripts
multimux config set confirm-terminate true    # change it
```

| Setting | Default | Effect |
| --- | --- | --- |
| `confirm-terminate` | `false` | Ask for confirmation before terminating a session. |

A change applies immediately in the browser tab that saved it; every other open
tab, and any change made from the shell, is picked up on the next reload.

## Security model

multimux is a **local, single-user tool** and its security posture reflects that.

- **Private network by default.** The daemon binds all interfaces on port 8686
  over TLS, but it is designed to be reachable only across a network you control
  (LAN, VPN, or [Tailscale](https://tailscale.com/)). Do not expose it to the
  public internet. The real root of trust is your local shell: anyone who can run
  `multimux` on the host, or read its data directory, already controls it — they
  can reset credentials, read the database, and attach to your tmux sessions
  directly. tmux sessions run as your user with the same access you have; there
  is no sandboxing between sessions.

- **First-run bootstrap uses a setup code**, printed to the daemon's own log or
  console (the same pattern as Jenkins' or Grafana's initial-admin secrets).
  Until the first passkey is registered the daemon is *setup-pending*: every API
  and WebSocket route except the setup ceremony returns `403`. The code is a
  random 6-character base32 value (~30 bits), expires in 15 minutes, is
  invalidated after 5 consecutive failed attempts, and is cleared permanently
  once the first passkey is registered.

- **Authentication is passkeys plus server-side sessions.** Login is a WebAuthn
  passkey — there are no passwords. A successful login mints a random 256-bit
  token, returned once in an `HttpOnly`, `Secure`, `SameSite=Strict` cookie. The
  server stores **only a SHA-256 hash** of it, so a leak of the SQLite database
  does not leak usable session tokens. Sessions expire on a 30-day sliding
  window and can be reviewed and revoked under Settings → Sessions.

- **The local CA is name-constrained.** The daemon generates its own certificate
  authority and signs a short-lived leaf for its own hostnames. That CA is
  **X.509 name-constrained** to exactly those hostnames (and their subdomains),
  so trusting it lets it vouch for the multimux host and **nothing else** — it
  is cryptographically unable to sign a certificate for `bank.com` or your
  employer's intranet. This is what makes it reasonable to trust on a shared or
  managed machine.

- **Cross-origin surface is closed deliberately.** Cookies are
  `SameSite=Strict`, which is exactly why the cross-daemon bearer-token popup
  exists: cross-daemon traffic authenticates with an explicit token, never an
  ambient cookie. Two daemons on one tailnet share a *site*, so the daemon also
  enforces a CSRF gate on every unsafe `/api/*` request — a cookie-authenticated
  mutation must carry a matching `Origin` and a JSON content type.
  Cookie-authenticated WebSocket upgrades are origin-checked the same way (CORS
  does not cover WebSockets); token-authenticated upgrades carry the secret
  explicitly and are allowed from any origin, which is how a remote tile
  connects.

### Non-goals (explicitly out of scope)

- **No multi-user support.** One person, one set of passkeys. There are no
  accounts, roles, or permissions.
- **No Windows.** macOS and Linux only.
- **No daemon-to-daemon communication.** Coordination happens entirely in your
  browser.
- **No dedicated native phone app.** Android Chrome and the installed PWA have a
  focused one-session mobile view, but sustained phone-first terminal work and
  a comprehensive mobile redesign of every Settings panel remain out of scope.
- **Contributions are not accepted.** This is a personal project shared as-is.
  Issues and PRs may go unanswered; fork freely.

## Troubleshooting

**The setup URL doesn't resolve.** See [Choosing a hostname](#choosing-a-hostname)
— restart with `--hostname <name-your-browser-can-reach>`.

**The browser refuses the certificate, or passkey registration fails.** The CA
isn't trusted on *that* client; see [Trusting the CA](#trusting-the-ca). On
Linux, Firefox and Chromium also need the NSS import.

**Lost every passkey.** Recover from the host's shell:

```sh
multimux auth reset --yes
```

That wipes all passkeys and login sessions and returns the daemon to
setup-pending; restart it and open the new setup URL. It requires local shell
access, the same root of trust as first-run setup.

**The database is corrupt.** State lives in a single SQLite file at
`~/.local/share/multimux/multimux.db`. Almost everything in it is rebuildable
(settings, tools, directories, tile layout, session bookkeeping); only your
credentials are not, which just means re-running setup. Stop the daemon, move
the file aside, and start it again — it creates a fresh database, seeds
defaults, and prints a new setup URL. Your PKI lives outside the database and
survives, so you usually don't need to re-run `multimux ca trust`. Running tmux
sessions keep running, but a fresh database starts with an empty grid and does
not re-discover them; reattach or kill them from the host shell.

**Where are my tmux sessions?** The daemon runs them on a **private tmux server**
(socket name `multimux`), named `mm-<session-id>`, so it never touches your
personal tmux sessions. List them with `tmux -L multimux ls`. If you upgraded
from a version that used the default tmux server, those older `mm-*` sessions
stay there — reattach with `tmux attach -t <name>` and let multimux create new
ones on its own socket.

## Developing

Prerequisites: Go, Node + pnpm, tmux.

**Run a dev daemon** in the foreground with a throwaway data dir so you don't
touch your real install (passkeys, sessions, CA). Use a fresh dir each run —
`--dev` refuses to start against a data dir that already has passkeys:

```bash
export MULTIMUX_DATA_DIR="/tmp/multimux-dev-$(date +%s)"
go run . serve
```

The dev daemon is a full install as far as auth is concerned: the same hostname
rules apply (`--hostname` persists into the throwaway data dir), the CA needs
trusting (`go run . serve --trust-ca`, or `go run . ca trust` with the same
`MULTIMUX_DATA_DIR` exported), and it prints a setup URL on which you register a
throwaway passkey — **at the daemon's own `https://` origin**, not through Vite.
For the frontend hot-reload loop none of that is needed; see below.

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

Register a throwaway passkey at `http://localhost:5173/setup?code=…` (the daemon
prints the code) and the full app — login, grid, live terminals — works at
`http://localhost:5173` with hot reload. No CA trust needed: the browser talks
plain HTTP to Vite. Caveats:

- Chrome/Firefox only — Safari does not treat `http://localhost` as trustworthy
  for `Secure` cookies, so login won't stick there.
- `--dev` refuses to run against a data dir that already has passkeys; the
  timestamped `MULTIMUX_DATA_DIR` above gives you a fresh one per shell.
- Each dev data dir uses its own private tmux server, so its `mm-*` sessions
  cannot collide with another dev run or the installed daemon.
- If nothing else is listening on 8686 you can drop `--port` and
  `MULTIMUX_DEV_TARGET` (the proxy target defaults to `https://localhost:8686`,
  see `web/vite.config.ts`).

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
