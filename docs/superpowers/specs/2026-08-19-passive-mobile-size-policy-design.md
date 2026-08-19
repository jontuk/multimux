# Passive Mobile Size Policy Design

## Scope

Implement delivery sequence item 1 from `MOBILE.md`. Mobile terminal connections
must not change the shared tmux window size through ordinary resize, focus, or
input activity. A deliberate **Fit session to phone** action may perform one
shared resize. Desktop behavior remains unchanged.

This item does not add keyboard-aware viewport handling, consolidated mobile
chrome, Compose, the terminal imperative handle, key controls, font presets, or
touch scrollback. Those remain in later delivery items.

## Connection capability

`TerminalTile` gains a size-policy prop whose default is the existing desktop
policy. `MobileSessionView` opts its selected tile into the passive policy. A
passive tile adds `size=passive` to its PTY WebSocket URL; an absent or unknown
value uses the existing active desktop policy for compatibility with older
clients.

The server parses the query parameter at connection registration and records the
policy on that connection's `ArbConn`. The capability affects only shared-window
arbitration. Every valid resize still sizes the connection's own attach PTY, and
all binary input is still written to that PTY.

## Arbiter behavior

An active-policy connection retains the existing rules:

- a permitted passive resize can establish or retain ownership;
- an `active: true` resize claims ownership; and
- binary input claims ownership and reapplies the connection's last dimensions
  when ownership changes.

A passive-policy connection follows these rules:

- an `active: false` resize records its dimensions and invokes the resize callback
  with `resizeWindow=false`, including when the connection was the most recent
  explicit owner;
- `ClaimInput` is a no-op, so keyboard, Compose, and future scroll input cannot
  claim or reapply shared dimensions; and
- an `active: true` resize is the one explicit escape hatch: it claims ownership
  and invokes the callback with `resizeWindow=true` once.

The explicit resize does not change the connection policy. Later ordinary mobile
resizes remain local. The next active desktop input transfers ownership and
reapplies that desktop connection's recorded dimensions through the existing
arbiter behavior.

## Mobile fit action

Only a passive `TerminalTile` shows **Fit session to phone**. Activating it asks
for confirmation with copy explaining that other attached clients will reflow.
Cancellation sends nothing. Confirmation refits xterm to its current box and
sends one `active: true` resize using the terminal's current rows and columns.

The action is disabled until the PTY WebSocket is open. It lives with
`TerminalTile` for this delivery item so the WebSocket stays encapsulated and the
broader imperative terminal handle is not introduced before delivery item 3.

Ordinary terminal focus on a passive tile does not send an active resize. Desktop
focus continues to claim dimensions as it does today.

## Failure behavior

The fit action uses the existing connection state. It is unavailable while the
socket is connecting, offline, exited, missing, or unauthorized. A connection
that closes between confirmation and send simply sends nothing; existing
reconnect and overlay behavior is unchanged.

Malformed or unknown size-policy query values fall back to active desktop
behavior. This avoids silently changing established clients.

## Testing

Backend arbiter tests cover:

- passive resize never touching the shared window;
- passive input never transferring ownership or reapplying dimensions;
- one active resize from a passive connection claiming exactly once;
- later passive resizes remaining local; and
- desktop input reclaiming its previously recorded dimensions.

Server tests cover query-policy parsing and its backward-compatible default.

Frontend tests cover:

- mobile tiles selecting the passive policy;
- passive PTY URLs carrying the capability;
- passive focus and ordinary reflow never producing an active resize;
- cancelled fit confirmation producing no active resize;
- confirmed fit producing exactly one active resize; and
- desktop tiles retaining the existing focus-claim behavior.

Finally, run `./verify.sh` to exercise all Go and web tests, linting, formatting,
builds, and the smoke check.
