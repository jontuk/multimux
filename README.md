# multimux

**A browser-based grid of live tmux terminals, served by a single Go binary.**

Run the daemon on any Mac or Linux box and open your sessions from any browser
on your private network — laptop, desktop, tablet, or phone. Sessions are real
tmux sessions, so they survive disconnects, daemon restarts, and logout.

<img width="1706" height="875" alt="multimux grid of terminal tiles" src="https://github.com/user-attachments/assets/f76b818b-55e1-407a-b630-b4239542a546" />

## Highlights

- **Single binary** — Go daemon with an embedded React 19 + xterm.js PWA and a
  pure-Go SQLite store. No runtime dependencies beyond tmux.
- **Passkey-only auth** — WebAuthn login, no passwords; server-side sessions
  stored as hashes.
- **Zero-config HTTPS** — the daemon mints its own *name-constrained* CA, so
  you get `https://` without a public CA and without trusting a root that could
  sign for any other site.
- **Multi-host grid** — tiles from several daemons in one tab, coordinated
  entirely in the browser via bearer tokens; daemons never talk to each other.
- **Shared-window arbitration** — the same session open on several machines
  follows whoever is actually typing, so a sleeping laptop can't shrink it.
- **Git-aware tiles** — branch, dirty state, and GitHub link per session, with
  colour-coding by working directory.
- **Mobile view** — one-session switcher for phones with swipe navigation, a
  compose box for dictation, and an Esc/Tab/Ctrl-C/arrow key row.

## Install

macOS or Linux (`amd64`/`arm64`). Requires **tmux**.

```sh
curl -fsSL https://raw.githubusercontent.com/jontuk/multimux/main/install.sh | sh
```

## Quick start

```sh
# 1. Pick a hostname, generate + trust the CA, print a setup URL
multimux serve --hostname your-machine.example.com --trust-ca

# 2. Open the printed setup URL and register a passkey

# 3. Ctrl-C, then run it as a background service (launchd / systemd user unit)
multimux service install
```

The daemon listens on **8686** and keeps its state in `~/.local/share/multimux`
(override with `MULTIMUX_DATA_DIR`).

**Choose the hostname carefully.** It is the WebAuthn RP ID, so changing it
later invalidates every passkey. It must resolve from the devices you'll browse
on — a Tailscale MagicDNS name (`box.tailnet.ts.net`) is the best choice; an
internal DNS name or an `/etc/hosts` entry also works. Without `--hostname` the
OS hostname plus `.local` is used. Extra names can be added later as SANs under
Settings → Daemon.

## Trusting the CA

Every client that opens the UI must trust the daemon's CA — browsers refuse
passkeys on untrusted certificates.

```sh
multimux ca trust                           # this machine's own daemon
multimux ca trust --remote user@daemon-host # a remote daemon's CA, fetched over SSH
```

- **macOS** — added to the login keychain.
- **Linux** — added to the system store (needs `sudo`). Firefox and Chromium
  use their own NSS store, so also run:
  `certutil -A -n multimux -t "C,," -i ~/.local/share/multimux/pki/ca.pem -d sql:$HOME/.pki/nssdb`
- **Android** — see [docs/android.md](docs/android.md).

If the hostname set changes, the CA is regenerated and must be re-trusted.

## Using it

- **+ New** opens a directory picker (recent dirs, then configured places,
  browsable with the arrow keys) and launches the selected tool there.
- **Drag** tiles to move them and boundaries to resize; **double-click** a
  header to maximise, a title to rename, a boundary to reset sizes.
- **−** removes a tile but leaves the session running; **✕** kills it.
- **Directory filter** buttons solo one directory's sessions, each with its own
  remembered layout (`Ctrl+Alt+←/→` to cycle, `Ctrl+Alt+0` for all).
- **Selecting text** copies to the clipboard; hold **Shift** to send the drag to
  tmux. Copy-mode yanks reach the clipboard via OSC 52. Lines the terminal or a
  tool such as Claude Code wrapped are rejoined when copied. That is a guess, so
  a full-width line can occasionally join the next; the **Text** view's **Raw**
  button shows and copies the original line breaks.
- **Pasting or dropping an image** (PNG, JPEG, GIF, WebP, up to 32 MiB) onto a
  tile uploads it to that tile's daemon and pastes the saved file's path, so
  image-aware tools like Claude Code can read it even when the session is on
  another machine. Uploads live in `$MULTIMUX_DATA_DIR/uploads` and are
  deleted after 24 hours.
- **Several daemons:** add them under Settings → Servers and click **Connect**.
- **Install as an app** from Chrome; give each daemon its own label and accent
  colour under Settings → Appearance to tell them apart.
- Screens **≤ 560px** wide get the mobile session switcher.

## Commands

```
multimux serve [--hostname H] [--port N] [--trust-ca]   run in the foreground
multimux service install|uninstall|upgrade|status|logs  manage the background service
multimux ca trust [--remote HOST]                       trust a daemon's CA
multimux config list|get|set                            user settings (also in the UI)
multimux auth reset --yes                               wipe passkeys, back to setup
multimux help [command]                                 full flags for a command
```

`service upgrade` installs the latest release and restarts onto it; data,
passkeys, and CA are untouched. On Linux, run `sudo loginctl enable-linger $USER`
once so the service outlives your login. `service install` snapshots
`MULTIMUX_DATA_DIR` and `MULTIMUX_HOSTNAME` from your shell — re-run it if you
change them.

## Security model

multimux is a **single-user tool for a network you control** (LAN, VPN,
Tailscale). Do not expose it to the public internet. Anyone with shell access to
the host already controls it; sessions run as your user with no sandboxing.

- **First-run setup code** — printed to the console, ~30 bits, expires in 15
  minutes, locked after 5 failures. Until a passkey exists every other route
  returns `403`.
- **Passkeys + server-side sessions** — a 256-bit token in an `HttpOnly`,
  `Secure`, `SameSite=Strict` cookie; only its SHA-256 hash is stored. 30-day
  sliding expiry, revocable under Settings → Sessions.
- **Name-constrained CA** — X.509 name constraints limit it to the daemon's own
  hostnames, so trusting it can't enable interception of any other site.
- **Closed cross-origin surface** — cross-daemon tiles use explicit bearer
  tokens, never ambient cookies. Cookie-authenticated mutations and WebSocket
  upgrades must pass an `Origin` check.

**Non-goals:** multi-user accounts, Windows, daemon-to-daemon traffic, a native
phone app. This is a personal project shared as-is; contributions aren't
accepted.

## Troubleshooting

- **Setup URL doesn't resolve** — restart with a `--hostname` your browser can
  reach.
- **Certificate refused / passkey registration fails** — the CA isn't trusted on
  that client (on Linux, check the NSS step).
- **Lost every passkey** — `multimux auth reset --yes` on the host, restart, and
  open the new setup URL.
- **Corrupt database** — stop the daemon, move `multimux.db` aside, and restart.
  Only credentials are lost; the CA survives.
- **Finding the tmux sessions** — they live on a private server:
  `tmux -L multimux ls` (named `mm-<id>`).

## Developing

Needs Go, Node + pnpm, and tmux. Always use a throwaway data dir.

```bash
export MULTIMUX_DATA_DIR="/tmp/multimux-dev-$(date +%s)"
go run . serve --dev --port 8787                                  # terminal 1
cd web && MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev     # terminal 2
```

Open `http://localhost:5173` (or `http://<lan-host>:5173` on a phone).
**`--dev` disables authentication entirely** — anyone who can reach the port
gets a shell as you, so use it only on a network you control. To test passkey
or setup flows, run without `--dev` and use the daemon's own `https://` URL.

The binary embeds `web/dist`, so build the frontend first:
`cd web && pnpm build && cd .. && go build`. `./verify.sh` runs everything CI
does. Releases are cut by pushing a `v*` tag.

## License

[MIT](LICENSE). © 2026 Jon Turner.
