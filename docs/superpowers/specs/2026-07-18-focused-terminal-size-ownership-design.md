# Focused Terminal Size Ownership

## Problem

When the same tmux session is displayed in multiple multimux browser windows,
each terminal attachment sends its own dimensions. Before either attachment
receives input, the resize arbiter has no owner, so whichever attachment sends
the last resize determines the shared tmux window size. A larger foreground
terminal can therefore show tmux's dotted padding when a smaller background
window wins the initial race.

## Desired behavior

The visible, focused browser window owns the shared tmux window size. A terminal
in that window must establish its fitted dimensions when its WebSocket opens
and reclaim ownership when the page becomes focused or visible. Keyboard or
mouse input continues to transfer ownership as it does today.

Background terminals remain connected and their individual attach PTYs are
still resized to their own panes. They must not change the shared tmux window
while another connection owns it.

If no browser window identifies itself as active, ordinary resize messages keep
the existing no-owner fallback so non-browser clients and unusual browser
states still receive a usable initial size.

## Design

The terminal resize protocol gains an optional `active` boolean. The browser
sets it to `true` only when the document is visible and its window has focus.
It sends the current fitted dimensions on WebSocket open, ResizeObserver
notifications, window focus, and document visibility changes.

The server parses the optional field and passes it into one atomic arbiter
operation. That operation always records the connection's latest dimensions.
An active resize claims ownership and may resize the shared tmux window.
An inactive resize may resize the shared window only when there is no owner or
the connection already owns it. The connection's own attach PTY is resized in
all cases.

The existing input claim path remains unchanged. Disconnecting the owner
continues to release ownership.

## Testing

- Protocol tests verify active and inactive resize encodings.
- Arbiter tests verify that an active resize claims ownership, background
  resizes cannot override it, and a different active connection can transfer
  ownership with its own dimensions.
- Existing input-transfer and disconnect behavior remains covered.
- The complete repository verification script must pass without warnings.

