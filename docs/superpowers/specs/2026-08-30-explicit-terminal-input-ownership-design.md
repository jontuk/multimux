# Explicit Terminal Input Ownership Design

## Problem

The PTY WebSocket currently treats every binary browser-to-server frame as
human input and calls `ArbConn.ClaimInput` before writing it to the attach PTY.
That assumption is false for xterm.js: its `onData` event carries both user
input and automatic replies to terminal queries.

The daemon log shows the resulting failure. A sleeping MacBook periodically
reconnects both grid tiles during wake; both tiles immediately produce an
`input` ownership claim at the MacBook's `107x58` dimensions, despite nobody
typing into two terminals simultaneously. The shared tmux windows then remain
at that smaller size until the active machine focuses each tile and restores
its `160x80` dimensions.

## Ownership Signal

Binary PTY data is transport only. Receiving a binary frame must never, by
itself, establish human presence or change shared-window ownership. The server
writes it to the attach PTY without calling `ClaimInput`.

The browser explicitly signals deliberate interaction by reusing the existing
active resize message:

```json
{"type":"resize","cols":160,"rows":80,"active":true}
```

For a follow-input desktop tile, current dimensions are sent actively before:

- keyboard input;
- paste;
- pointer input;
- wheel input; and
- input or paste through the imperative terminal handle.

This preserves the existing outcome of `ClaimInput`: if another client owned
the tmux window, deliberate activity transfers ownership and reapplies this
connection's current dimensions before its input reaches the PTY. Reusing the
resize message avoids adding a second ownership-control protocol whose only
effect would be the same resize.

Automatic xterm replies have no originating browser gesture and therefore send
only their binary payload. They still reach tmux, but they cannot mark a client
present, take ownership, or resize the shared window.

## Passive Sessions

Passive mobile policy is unchanged. Ordinary keyboard, Compose, paste, touch,
pointer, and wheel activity continues to affect only the mobile attach PTY and
must not send an active resize. **Fit session to phone** remains the sole mobile
gesture that sends `active: true` and changes the shared tmux window.

## Browser Event Handling

`TerminalTile` owns both the terminal and its WebSocket, so it remains the one
place that translates deliberate browser activity into ownership signals.
Capture-phase listeners identify keyboard, paste, pointer, and wheel gestures
without trying to infer intent from `term.onData`. Capture is important because
xterm may synchronously emit the corresponding data while processing the same
DOM event.

The terminal handle's `input` and `paste` methods explicitly perform the same
follow-input claim before sending data, since mobile controls and Compose call
those methods without necessarily producing an event inside xterm's container.
Repeated claims by the current owner are harmless and keep presence current;
the arbiter already makes same-owner transfers a no-op apart from applying the
requested dimensions.

Focus behavior remains as it is: a user-caused desktop focus sends an active
resize, while wake-generated focus is rejected using `navigator.userActivation`.
ResizeObserver, reconnect, window focus, and visibility resynchronization remain
ordinary inactive resizes.

## Compatibility

The WebSocket wire format does not change. A browser still running the previous
frontend can claim through its existing user-caused focus behavior, but binary
input alone no longer transfers ownership after the daemon update. The existing
frontend-build notice prompts that tab to reload onto the complete gesture
behavior. This brief limitation is preferable to retaining the bug for dormant
old tabs, which are the clients that trigger it.

## Testing

Backend WebSocket tests will verify that:

- binary data reaches the PTY without calling `ClaimInput` or resizing the
  shared tmux window; and
- the existing active resize frame still transfers ownership and applies its
  dimensions before later binary input.

Frontend terminal tests will verify that:

- an unprompted xterm `onData` emission sends its bytes without an active resize;
- desktop keyboard, paste, pointer, wheel, and imperative input each send an
  active resize before their data or interaction;
- reconnect, ResizeObserver, window focus, and visibility resync stay inactive;
  and
- passive mobile interaction stays inactive while confirmed phone fit remains
  the explicit exception.

Finally, `./verify.sh` will run the Go and web tests, formatting, linting,
builds, and smoke check.
