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
variable if you need to.

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
that will open the UI:

```
multimux ca trust
```

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

## 5. Service management

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
  `~/.config/systemd/user/`, enabled with `--now` and `Restart=on-failure`. For
  the daemon to keep running after you log out, enable lingering once:

  ```
  sudo loginctl enable-linger $USER
  ```

The service runs `multimux serve` with no extra flags. To change the port or add
extra SANs, edit them in the daemon's **Settings** page in the UI (they are stored
in SQLite), or run `serve` manually with flags.

## 6. Upgrading

multimux keeps no schema migrations you need to run by hand and stores nothing in
the binary. To upgrade:

1. Download the new archive and replace the `multimux` binary in place.
2. Restart the service:
   - macOS: `multimux service uninstall && multimux service install`, or
     `launchctl kickstart -k gui/$(id -u)/com.jontuk.multimux`.
   - Linux: `systemctl --user restart multimux`.

Your data directory, passkeys, and CA are untouched by an upgrade. If the CA's
hostname set has changed you will be told to re-run `multimux ca trust`.
