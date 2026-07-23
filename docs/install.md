# Installing multimux

multimux ships as a single static binary (`CGO_ENABLED=0`) for macOS and Linux,
on `amd64` and `arm64`. There is no Windows build.

## 1. Install the binary

Download the archive that matches your platform from the
[releases page](https://github.com/jontuk/multimux/releases). Archives are named
`multimux_<version>_<os>_<arch>.tar.gz`.

### macOS

```
tar xzf multimux_<version>_darwin_arm64.tar.gz
sudo mv multimux /usr/local/bin/
```

The binary is not notarised. The first time you run it, macOS Gatekeeper may
block it; allow it under **System Settings → Privacy & Security**, or clear the
quarantine attribute:

```
xattr -d com.apple.quarantine /usr/local/bin/multimux
```

You also need tmux installed (`brew install tmux`). multimux checks for tmux at
startup and refuses to start without it.

### Linux

```
tar xzf multimux_<version>_linux_amd64.tar.gz
sudo mv multimux /usr/local/bin/
```

Install tmux from your distribution (`apt install tmux`, `dnf install tmux`,
etc.).

## 2. First run and the setup code

Run the daemon once so it can create its data directory, generate its local CA,
and print a setup URL:

```
multimux serve
```

Data lives under `~/.local/share/multimux` by default (SQLite database, PKI
material, logs). Override the location with the `MULTIMUX_DATA_DIR` environment
variable if you need to. Export it in the same shell you later run
[`multimux service install`](#6-service-management) from — install bakes the
value into the unit, and the service would otherwise fall back to the default
directory (a fresh database and CA).

The daemon prints a setup URL containing a one-time code:

```
=== multimux setup ===
Open: https://your-host.local:8686/setup?code=ABC123
```

The default port is **8686**. If the printed name won't resolve from your
browser (mDNS blocked, corporate DNS, Tailscale-only), restart with a name that
does: `multimux serve --hostname <name>` — it must contain a dot or be
`localhost` (the name is the WebAuthn RP ID; see
[docs/security.md](security.md#rp-id-and-the-hostname-change-warning)), and it
is persisted.

**Don't open the setup URL yet.** Browsers refuse passkey (WebAuthn) ceremonies
on pages served with an untrusted certificate, so trust the CA first — next
section.

## 3. Trust the local CA

To load `https://` without a certificate warning, install the daemon's
name-constrained CA into your OS trust store. Run this on **each client machine**
that will open the UI.

**On the daemon host itself**, trust its own CA:

```
multimux ca trust
```

**From a different machine** (e.g. a laptop connecting to a cloud box over
Tailscale), you need that box's CA, not the laptop's own. Point `--remote` at the
daemon host and multimux copies the CA over `scp` and installs it — no manual file
juggling:

```
multimux ca trust --remote user@daemon-host
```

This uses your existing SSH access as the transport's trust (no chicken-and-egg
of fetching a CA over an untrusted TLS connection). It reads the CA from
`~/.local/share/multimux/pki/ca.pem` on the remote; override with `--remote-path`
if the daemon runs under a custom `MULTIMUX_DATA_DIR`. Before installing, multimux
prints the CA's subject and the hostnames its name constraints permit, so you can
confirm what you are trusting. See
[Connecting from another machine](work-network.md#connecting-from-another-machine)
for the full cloud-box walkthrough.

- **macOS** adds the CA to your **login keychain** (no `sudo`; Keychain Access
  may prompt once to confirm). Verify with:

  ```
  curl https://your-host.local:8686/healthz
  ```

  which should succeed without `-k`.

- **Linux** copies the CA into the distribution's anchor directory and refreshes
  the system trust store, which needs `sudo`:
  - Debian/Ubuntu: `/usr/local/share/ca-certificates/multimux-ca.crt` +
    `update-ca-certificates`
  - Fedora/RHEL: `/etc/pki/ca-trust/source/anchors/multimux-ca.pem` +
    `update-ca-trust`

  If neither anchor directory exists, `multimux ca trust` reports that it can't
  find one and prints the path to the CA file (`~/.local/share/multimux/pki/ca.pem`)
  so you can install it manually.

### Linux browser caveat (Firefox / Chromium)

Firefox and Chromium on Linux do **not** use the system trust store — they keep
their own [NSS](https://firefox-source-docs.mozilla.org/security/nss/index.html)
certificate database. After running `multimux ca trust`, also import the CA into
your NSS store:

```
certutil -A -n multimux -t "C,," -i ~/.local/share/multimux/pki/ca.pem -d sql:$HOME/.pki/nssdb
```

(`certutil` is in the `libnss3-tools` / `nss-tools` package.) Chrome and Safari on
macOS, and Chrome on Linux for non-NSS profiles, use the OS store and need no
extra step.

## 4. Register your passkey

With the CA trusted, open the setup URL from step 2 and register a passkey.
(If the one-time code has expired — they last 15 minutes — restart the daemon
to print a fresh one.)

## 5. Install as an app (PWA)

multimux is a Progressive Web App: install it from your browser and it runs in
its own window with no address bar, like a native app.

Installing changes the window, not the connection — the app has no offline mode.
Terminals are live tmux sessions on the daemon, so the installed app needs the
daemon reachable exactly like the browser tab does.

In **Chrome** (desktop), open the daemon URL, then click the install icon in the
address bar — or **⋮ → Cast, save, and share → Install page as app…** — and
confirm **Install**.

Each daemon installs as a **separate app**, keyed to its origin (host + port), so
you can install one per host and switch between them from your dock or app
launcher. To tell them apart, give each daemon a distinct **host label** and
**accent color** under **Settings → Appearance**: the label becomes the app name
and the accent tints its icon. After changing either, reinstall the app to pick
up the new name and icon.

**Firefox** does not support installing PWAs on the desktop. multimux still works
normally in a Firefox tab; to get the installed-app window, use Chrome.

Firefox also can't use your Chrome/Safari platform passkey — passkeys don't roam
between browsers' platform stores, and Firefox on macOS doesn't integrate with
Touch ID / iCloud Keychain at all. To log in from Firefox, either use a hardware
security key or phone-via-QR (both work in any browser), or register a dedicated
Firefox passkey under **Settings → Passkeys**. On **Linux**, Firefox keeps its
own certificate store, so import the CA into its NSS database (see the [Linux
browser caveat](#linux-browser-caveat-firefox--chromium) above) or the passkey
ceremony is blocked.

## 6. Service management

Instead of running `multimux serve` by hand, install it as a user-level service
so it starts at login and restarts on failure:

```
multimux service install     # launchd LaunchAgent (macOS) / systemd user unit (Linux)
multimux service status
multimux service uninstall
```

- **macOS**: a LaunchAgent labelled `com.jontuk.multimux` is written to
  `~/Library/LaunchAgents/` with `RunAtLoad` and `KeepAlive`. Stdout/stderr go to
  `~/.local/share/multimux/multimux.log`. `install` is idempotent — it re-bootstraps
  cleanly if already installed.
- **Linux**: a systemd **user** unit `multimux.service` is written to
  `~/.config/systemd/user/`, enabled with `--now` and `Restart=on-failure`. The
  unit sets `KillMode=process` so stopping or restarting the service signals
  only the daemon — your tmux sessions (which live in the same cgroup) survive
  service stops, restarts, and upgrades. For the daemon to keep running after
  you log out, enable lingering once:

  ```
  sudo loginctl enable-linger $USER
  ```

The service runs `multimux serve` with no extra flags. To change the port or add
extra SANs, edit them in the daemon's **Settings** page in the UI (they are stored
in SQLite), or run `serve` manually with flags.

**Environment capture.** `service install` copies `MULTIMUX_DATA_DIR` and
`MULTIMUX_HOSTNAME` out of the installing shell and writes them into the unit
(`Environment=` on Linux, `EnvironmentVariables` on macOS), because launchd and
systemd start the daemon with none of your shell's environment. Without this a
custom data directory is silently lost and the service comes up on the default
one — a fresh database, a fresh CA, and a setup-pending daemon. Two
consequences:

- Run `service install` from a shell where those variables are set as you want
  them (`export MULTIMUX_DATA_DIR=... && multimux service install`).
- The captured values are a **snapshot**. Changing the variable in your shell
  afterwards does not affect the service — re-run `multimux service install` to
  rewrite the unit.

Variables that are unset at install time are not written at all, so a default
install keeps resolving them at runtime.

## 7. Upgrading

multimux keeps no schema migrations you need to run by hand and stores nothing in
the binary. To upgrade:

1. Download the new archive and replace the `multimux` binary in place.
2. Restart the service:
   - macOS: `multimux service uninstall && multimux service install`, or
     `launchctl kickstart -k gui/$(id -u)/com.jontuk.multimux`.
   - Linux: `systemctl --user restart multimux`.

Your data directory, passkeys, and CA are untouched by an upgrade. If the CA's
hostname set has changed — or the CA is within 30 days of its 10-year expiry, in
which case it is renewed automatically — the daemon prints a warning telling you
to re-run `multimux ca trust`.

**Linux, units installed before `KillMode=process` was added:** older units kill
the tmux server (and every session in it) when the service stops or restarts.
Re-run `multimux service install` once to rewrite the unit, then restarts and
upgrades leave tmux sessions running.
