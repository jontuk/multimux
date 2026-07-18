# Maximize Session Tile Design

## Goal

Double-clicking a session tile's header maximizes it so the window shows only
that session. Double-clicking again (or pressing Escape) restores the previous
grid layout. Mirrors the equivalent feature in cheep.

## Behaviour

- Double-click on a session tile's header toggles maximized state for that
  tile.
- While maximized, the tile fills the viewport, covering the app header and
  the rest of the grid.
- Pressing Escape while a tile is maximized restores the grid. The listener is
  on `window`, so Escape also reaches the focused terminal (same trade-off as
  cheep).
- Maximized state is ephemeral: not persisted to the server layout and lost on
  reload.
- Only real session tiles maximize — not empty tiles or the "server removed"
  placeholder.
- If the maximized tile is removed from the grid, terminated, or disappears
  from the layout, the maximized state clears and the grid view returns.

## Implementation

`GridPage` gains `maximizedKey: string | null` state holding the tile key
(`serverId:sessionId`). Keying by tile key rather than grid index keeps the
maximized tile stable across drag-and-drop swaps.

The maximized tile's outer `.tile` element gets a `tile-maximized` class styled
as a fixed full-viewport overlay (`position: fixed; inset: 0`, elevated
`z-index`, app background). Because the same DOM node stays mounted,
`TerminalTile`'s WebSocket connection survives, and its existing
`ResizeObserver` re-fits xterm and resizes the PTY automatically in both
directions.

A `window` keydown effect, active only while a tile is maximized, clears the
state on Escape. A second effect clears `maximizedKey` whenever the layout no
longer contains that tile.

The tile header already suppresses text selection via CSS if present;
otherwise `user-select: none` is added so double-clicks don't select header
text.

## Testing

Frontend tests in `grid-page.test.tsx` verify:

1. Double-clicking a session tile header adds `tile-maximized` to its tile.
2. Double-clicking again removes it.
3. Escape while maximized removes it.
4. Removing the maximized tile from the grid clears the maximized state.

Validated end-to-end with `./verify.sh`.
