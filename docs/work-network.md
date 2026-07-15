# Running multimux on a work / managed network

Corporate and managed environments add friction that multimux is designed to work
around. This guide covers the three things that most often bite: root-CA
restrictions, `.local` name resolution, and recovering a corrupted database.

## Managed devices that block custom root CAs

Many MDM-managed machines forbid adding custom root certificate authorities, or
flag them for review. multimux mitigates this two ways:

- **Name constraints make the CA far less objectionable.** multimux's local CA is
  X.509 name-constrained to only its own hostnames (see
  [docs/security.md](security.md)). Unlike a normal root CA, it is
  cryptographically unable to sign certificates for any other domain, so trusting
  it cannot be used to MITM your bank or your employer's intranet. Where a policy
  allows _constrained_ roots, this is the CA to point at.

- **`--behind-proxy` is the escape hatch.** If you cannot add a root CA at all,
  don't. Run the daemon in proxy mode and terminate TLS at something whose
  certificate the device already trusts — most cleanly
  [Tailscale Serve](proxy.md#worked-example-tailscale-serve), whose certificate is
  trusted without any local CA install. See [docs/proxy.md](proxy.md).

## Corporate DNS that blocks mDNS / `.local`

multimux defaults to your machine's hostname plus its `.local` (mDNS/Bonjour)
form. On locked-down corporate networks, **mDNS is often blocked**, so
`your-host.local` may not resolve from another device — the tile page just fails
to load.

Fixes, in order of preference:

1. **Use a name your corporate DNS already serves.** Start the daemon with an
   internal DNS name that resolves on the network:

   ```
   multimux serve --hostname mux.corp.example.com
   ```

   The name is persisted (also settable via `MULTIMUX_HOSTNAME` for service
   units) and must contain a dot (see
   [docs/security.md](security.md#rp-id-and-the-hostname-change-warning)). Do
   this **before** registering your first passkey, because the hostname is the
   WebAuthn RP ID and changing it later invalidates passkeys — the daemon
   refuses the change and points you at `multimux auth reset --yes`. Additional names go under **Extra SANs** in the
   Daemon settings after you've logged in (these become both TLS SANs and
   allowed origins).

2. **Use a Tailscale MagicDNS name.** It resolves anywhere on your tailnet
   regardless of corporate DNS, and it is stable — ideal as the RP ID:

   ```
   multimux serve --hostname <machine>.<tailnet>.ts.net
   ```

3. **Add a `hosts` entry.** As a last resort, map the daemon's name to its IP in
   each client's hosts file (`/etc/hosts` on macOS/Linux). Make sure the name you
   put there is one of the daemon's configured SANs, or TLS validation will fail.

Remember that the hostname/SAN set is baked into the CA's name constraints: when
you change it, the daemon **regenerates the CA** and prints a warning to re-run
`multimux ca trust` on every client.

## Recovering from SQLite corruption

multimux keeps its state in a single SQLite database at
`~/.local/share/multimux/multimux.db` (or under `MULTIMUX_DATA_DIR`). If the
daemon fails to start because the database is corrupt, the good news is that
**almost everything is rebuildable** — the database holds settings, tool/directory
presets, tile layout, and session bookkeeping, all of which you can recreate. The
only thing that is _not_ recoverable is your **credentials** (passkeys and login
sessions): losing those simply means re-running first-time setup.

Recovery:

1. Stop the daemon (`multimux service uninstall`, or stop the foreground
   process).
2. Move the corrupt database aside:
   `mv ~/.local/share/multimux/multimux.db ~/.local/share/multimux/multimux.db.bad`
3. Start the daemon again. It creates a fresh database, seeds defaults, and prints
   a new setup URL.
4. Open the setup URL and register a passkey again. Your PKI (the CA under
   `~/.local/share/multimux/pki`) is separate from the database and survives, so
   you usually do **not** need to re-run `multimux ca trust`.

Your running tmux sessions are owned by tmux, not by the database, so the
underlying tmux processes keep running across this. Note, though, that multimux
only tracks sessions it created in its own database — a fresh database starts with
an empty grid and will not re-discover those orphaned tmux sessions. You can still
reattach to them directly with `tmux attach`, or kill them, from the host shell.
