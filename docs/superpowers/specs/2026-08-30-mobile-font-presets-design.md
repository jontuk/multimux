# Mobile font presets — design

## Goal

Finish delivery sequence item 5 from `MOBILE.md`: let a phone user choose a
readable terminal font size without reconnecting or changing daemon-wide state.

## Scope

Add four explicit terminal font sizes: 13, 11, 10, and 9 px. The current 13 px
size remains the default. The selection is browser-local, applies to every
mobile session viewed by that browser, and survives reloads.

Pinch zoom, arbitrary font sizes, per-session font sizes, desktop font controls,
and daemon settings remain out of scope.

## Experience

Place a compact native select labelled **Terminal font size** in the consolidated
mobile header alongside Compose and Fit session to phone. Its visible values are
`13 px`, `11 px`, `10 px`, and `9 px`.

Changing the selection immediately updates the mounted terminal and refits it to
the available space without reconnecting. Switching to another mobile session
applies the same selected size to the newly mounted terminal.

A native select is preferred over a cycling button because it exposes all
choices, identifies the current value, is keyboard-accessible, and uses the
phone's standard picker UI. It is preferred over a Settings-page control because
the setting has immediate, terminal-local visual feedback and belongs with the
existing mobile terminal controls.

## State and persistence

Keep storage access in a small mobile-font module rather than embedding parsing
rules in the view. The module owns:

- the allowed sizes and 13 px default;
- reading the saved value from `localStorage`;
- rejecting malformed or unsupported values; and
- writing a selected allowed value.

Storage failures must not prevent the terminal from rendering or resizing. A
missing, invalid, or unreadable value falls back to 13 px. A failed write still
applies the choice for the current page lifetime.

The state lives above the selected terminal in `MobileSessionView`, so changing
sessions does not reset it. The view calls the semantic `TerminalHandle`
`setFontSize` operation; it does not reach into xterm or its WebSocket.

## Components and data flow

1. `MobileSessionView` initializes its font state from browser storage.
2. `MobileFontSize` renders the native select into the mobile controls portal.
3. A selection change updates view state, saves it, and calls
   `terminalRef.current.setFontSize(size)`.
4. When a different terminal mounts, the view reapplies the current size through
   the same handle.
5. `TerminalTile.setFontSize` continues to update xterm, fit to the tile, and send
   a passive resize for mobile connections.

## Testing

Unit tests cover default, valid, invalid, and unavailable browser storage.
Component tests cover the selector's options, immediate application and
persistence, and reapplication after session switching. Existing terminal tests
continue to cover refitting without reconnection.

Run the focused frontend tests during development, then `./verify.sh` before
completion. The automated suite cannot certify physical-device readability or
browser/PWA keyboard behavior, so the real-device checklist in `MOBILE.md`
remains a release check rather than an automated completion claim.
