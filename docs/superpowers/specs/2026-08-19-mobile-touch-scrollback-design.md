# Mobile Touch Scrollback Design

## Scope

Implement delivery sequence item 6 from `MOBILE.md`. A one-finger vertical
drag over the selected mobile terminal scrolls tmux history through xterm's
existing wheel-input path. The gesture enters and moves through tmux copy mode;
the existing mobile Esc key remains the explicit way to leave copy mode.

This item does not add inertia, touch selection, copy-mode-specific controls,
pinch gestures, desktop drag scrolling, or xterm-owned scrollback. It also does
not implement delivery sequence item 5, font presets; touch scrollback remains
independently usable at the existing terminal font size.

## Gesture recognition

Only the primary touch pointer can start a scroll gesture. Mouse and pen input,
secondary touches, and additional pointers are ignored.

A new touch starts as a tap candidate. Movement must cross a 12 CSS pixel
activation threshold before it is classified. It becomes a scroll only when
vertical movement is greater than horizontal movement. A movement that crosses
the threshold with horizontal movement greater than or equal to vertical
movement is rejected for the rest of that pointer sequence. A tap or rejected
gesture emits no wheel input.

Once a vertical drag activates, the terminal captures the pointer and prevents
the browser default for subsequent movement. Movement is accumulated, and each
24 CSS pixels emits one wheel step. Finger movement down emits wheel-up to move
towards older output; finger movement up emits wheel-down to move towards newer
output. Direction changes retain only the signed unconsumed remainder, so the
gesture tracks the user's current movement without a delayed burst from the
previous direction. Pointer up, cancellation, or lost capture clears all
gesture state. There is no velocity tracking or inertial continuation.

The mobile terminal disables native touch panning in its terminal region so
pointer movement remains observable, but an unactivated gesture is not
cancelled in JavaScript. This preserves ordinary terminal taps and focus.

## Xterm and tmux integration

Add a focused `touchScroll` helper that owns the pointer state machine and
dispatches synthetic, bubbling, cancelable `WheelEvent`s to xterm's root
element. Each event uses line delta mode and the latest pointer coordinates so
xterm can translate it using its negotiated mouse protocol and terminal cell
position.

`TerminalTile` gains an optional `touchScrollback` property. When enabled, it
installs the helper after xterm opens and removes it during the existing effect
cleanup. `MobileSessionView` enables the property for its selected terminal;
desktop terminal callers retain the default disabled behavior.

The helper receives a readiness callback and emits wheel events only while
`term.modes.mouseTrackingMode` is not `none`. This avoids xterm's no-scrollback
fallback converting an early drag into application cursor-key input before
tmux has enabled mouse reporting. The implementation does not expose xterm or
the WebSocket through `TerminalHandle`, manually encode SGR mouse bytes, assume
a tmux prefix, or add a backend protocol operation.

Tmux remains the sole owner of the 50,000-line history. Its existing mouse
configuration receives wheel-up, enters copy mode, and applies its configured
wheel bindings. Wheel movement works the same way after copy-mode entry. Esc
from the essential key bar sends the existing terminal byte and exits copy
mode.

## Lifecycle and interaction safety

The helper is scoped to one mounted `TerminalTile`. Session switching unmounts
the old tile, removes all pointer listeners, and clears any active gesture
before the next selected terminal mounts. A disconnected WebSocket needs no
separate gesture state: xterm may still translate a drag while reconnecting,
but the tile's normal data path refuses the resulting transport write. Cleanup
disposes xterm and its negotiated mouse state.

Touch scrolling does not claim shared tmux window dimensions. It produces
terminal input through the passive mobile connection but does not call the
active resize path. The shared desktop size remains protected by delivery item
1's size policy.

## Testing and documentation

Focused helper tests cover:

- movement below the threshold producing no wheel event;
- vertical activation and pointer capture;
- downward and upward drags producing opposite wheel directions;
- 24-pixel accumulation, multiple steps, and signed remainder behavior;
- horizontal and diagonal rejection;
- mouse, pen, non-primary, and secondary pointer rejection;
- inactive mouse tracking producing no input;
- pointer up, cancellation, lost capture, and cleanup resetting state; and
- coordinates and wheel-event options passed to xterm.

Terminal integration tests cover the option being disabled by default,
installed only when requested, and cleaned up on unmount. Mobile view tests
cover the selected terminal enabling touch scrollback. CSS tests cover native
touch panning being disabled only for the opted-in terminal.

Update README's mobile terminal section to describe dragging down for older
output, dragging up for newer output, and using Esc to leave copy mode. Run the
focused web tests during development and `./verify.sh` before completion.

The automated suite cannot replace the release check on hardware. Verify on a
current iPhone and Android phone, in both browser and installed-PWA modes, that
a downward drag enters copy mode, both directions move reliably without
triggering session switching, taps still focus/interact, Esc exits copy mode,
and no inertial movement continues after release.
