# Mobile Viewport and Chrome Design

## Scope

Implement delivery sequence item 2 from `MOBILE.md`. The mobile grid route gets
one compact header, full safe-area coverage, and keyboard-aware terminal sizing.
The existing **Fit session to phone** action moves into the combined header.

This item does not add Compose, the general imperative terminal handle, the
essential key bar, font presets, or touch scrollback. Desktop grid and Settings
layouts remain unchanged.

## Viewport contract

The HTML viewport metadata opts into edge-to-edge safe-area layout with
`viewport-fit=cover` and asks supporting Android browsers to resize content for
the software keyboard with `interactive-widget=resizes-content`.

The mobile grid route also measures `window.visualViewport.height` when that API
is available. A dedicated React hook owns the measurement. It reads the initial
height synchronously, listens for visual viewport resize events, and publishes a
new height only after a trailing debounce. Repeated events during a keyboard or
orientation transition replace the pending update, so the app shell and its
terminal settle once at the final visible height. Cleanup removes the listener
and cancels a pending timer.

The measured height is exposed to the mobile route as a CSS custom property.
CSS continues to provide `100vh` and `100dvh` fallbacks when `visualViewport` is
absent. The route remains a constrained flex column; when its height changes,
the existing `ResizeObserver` in `TerminalTile` refits xterm and sends an
ordinary passive resize. No new terminal-specific viewport listener is added.

## Consolidated mobile header

The separate application header is hidden only for the mobile grid route. Other
routes and the desktop grid retain the current application header unchanged.

`App` passes the local host label to `GridPage`, which passes it to
`MobileSessionView`. The mobile view renders one compact header containing:

- the local host label when configured;
- the selected session title;
- branch and directory context;
- the session position;
- mobile terminal controls; and
- an accessible Settings link.

The whole header retains the existing slider semantics, keyboard navigation,
pointer capture, and horizontal swipe behavior when sessions exist. Metadata
continues to ellipsize instead of expanding the route beyond the viewport.

The header also remains present while sessions are loading or when no sessions
are running. In those states it keeps the host identity and Settings navigation
available, omits session slider attributes and gestures, and leaves the existing
status copy in the content area.

Safe-area insets are applied to the combined header at the top and horizontal
edges. The terminal keeps bottom safe-area padding. This supports portrait and
landscape layouts after `viewport-fit=cover` enables content beneath display
cutouts and browser chrome.

## Fit control placement

The Fit action remains owned by `TerminalTile`: connection state, confirmation,
refitting, and the one active resize continue to use its private terminal and
WebSocket state. `TerminalTile` accepts an optional controls portal target. A
passive terminal portals its existing Fit button into that target when supplied
and otherwise renders the button in its current in-terminal position.

`MobileSessionView` provides a controls target in the combined header. This
moves the visible action without introducing item 3's broader imperative
terminal handle or exposing transport state. The button remains disabled until
the PTY socket is open, retains its warning confirmation, and continues to send
exactly one active resize when confirmed.

## Failure and compatibility behavior

Browsers without `visualViewport` keep the existing dynamic-viewport CSS path.
An unavailable host label simply leaves that field out of the combined header.
A socket that is not open leaves Fit disabled, and a socket closing between
confirmation and transmission still sends nothing.

The mobile view continues to mount only the selected terminal. Switching
sessions changes the terminal and its portalled control together without
altering the saved desktop layout. Existing stale-build and server-status
banners remain outside the consolidated header because they are transient
status messages rather than application chrome.

## Testing

Frontend tests cover:

- the exact viewport metadata tokens;
- initial visual viewport measurement, trailing debounce, and cleanup;
- the app shell receiving the measured mobile height;
- the desktop and Settings headers remaining unchanged;
- one combined mobile header with host label, session metadata, position, and
  Settings navigation;
- the combined header remaining usable during loading and empty states;
- the passive Fit button rendering in the combined header rather than over the
  terminal;
- Fit remaining disabled until connected and retaining its existing confirmed
  and cancelled resize behavior; and
- session switching retaining one mounted terminal and one matching Fit action.

Finally, run `./verify.sh` to exercise formatting, linting, all Go and web tests,
both builds, and the smoke check.
