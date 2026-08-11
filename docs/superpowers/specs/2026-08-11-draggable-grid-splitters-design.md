# Draggable grid splitters

## Problem

Every tile in the grid is the same size. `GridPage.tsx:390` renders the grid as
`repeat(rows, 1fr) / repeat(cols, 1fr)`, so a 2x2 is always four equal
quarters. In practice tiles do not deserve equal space: one session is being
watched closely, another is a long-running build whose last few lines are all
that matter.

The goal is dragging the boundary between tiles to give some tiles more room —
including dragging a vertical boundary in the bottom row without disturbing the
top row.

## Resize model

Row heights are shared across the whole grid; column widths are per row.

```
+-----+----------+
|  A  |    B     |   row 0 widths [0.35, 0.65]
+=====+====+=====+   <- row boundary, full width
|  C       |  D  |   row 1 widths [0.70, 0.30]
+----------+-----+
```

A horizontal divider moves one row boundary across the full width. A vertical
divider moves one boundary within its own row only.

Rejected: a full tmux-style split tree. It would replace the whole layout model
(`normalize`, `addTile`, `removeTile`, `swapTiles`, the column stepper) and the
persisted document, for freedom the grid does not otherwise want.

## Layout mechanics

**Tiles are positioned absolutely; the CSS grid goes away.**

Per-row column widths cannot be expressed by one CSS grid, because grid columns
are global to the grid. The obvious alternative — a flex row element per row —
nests tiles under row parents, and React unmounts a child whose parent changes.
Any swap or insertion that moved a tile across rows would tear down xterm and
reconnect its PTY WebSocket. The identity-keying comment at `GridPage.tsx:398`
exists precisely to prevent that, so the flex-rows shape is not available.

Therefore `.grid` becomes `position: relative`, and each tile keeps its flat
key and its flat position in the children list, rendered with
`position: absolute` and `left/top/width/height` in percent computed from
`rowSizes` and `colSizes[row]`. `--tile-gap` is applied as an inset via
`calc()`, so the visual gutter is unchanged. Maximize (`.tile-maximized`, which
is `position: fixed; inset: 0`) is unaffected.

## New module: `web/src/grid/sizes.ts`

Pure functions, no React, unit-testable in isolation:

- `equalSizes(n): number[]` — `n` tracks each `1/n`.
- `normalizeSizes(shape, rowSizes?, colSizes?)` — applies the keep-what-fits
  rule below, and repairs anything malformed (wrong length, non-finite values,
  negatives, or a sum that is not 1 within tolerance) by falling back to equal
  for that axis or that row.
- `resizeTracks(sizes, boundaryIndex, deltaFraction, minFraction)` — moves one
  boundary, taking from exactly the two adjacent tracks so all other tracks are
  untouched, clamped so neither adjacent track falls below `minFraction`, with
  snap-to-equal.
- `tileRect(shape, rowSizes, colSizes, index)` — `{left, top, width, height}`
  in percent for one tile.

### Keep what still fits

When the shape changes:

- Row heights are kept when the row count is unchanged, otherwise reset to
  equal.
- Row *r*'s column widths are kept when the column count is unchanged,
  otherwise that row resets to equal. Rows are identified by index.

Tiles are padded with trailing nulls to a full rectangle (`model.ts:15`), so
every row always has exactly `shape.cols` cells; "column count unchanged" is
therefore "`shape.cols` unchanged". Closing a tile that drops a whole row
re-equalizes row heights but leaves surviving rows' widths alone.

## Dividers and drag

Dividers are siblings of the tiles inside the same absolute container, rendered
from the layout rather than as tile children:

- `rows - 1` row dividers, full width, at each row boundary, `cursor: row-resize`.
- For each row, `cols - 1` column dividers spanning only that row's height,
  `cursor: col-resize`.

Hit area is ~9px on the drag axis, centred on the boundary, so the handle stays
grabbable with a 4px `--tile-gap`. Handles set `touch-action: none`. The mobile
breakpoint (`MOBILE_VIEW_QUERY`) renders `MobileSessionView` instead of the
grid, so touch means tablets only.

Drag uses Pointer Events with `setPointerCapture`, so there are no window-level
listeners to leak and the drag survives the pointer crossing an xterm canvas.

- `pointerdown` records the container rect and the starting sizes.
- `pointermove` converts the pixel delta to a fraction of that rect's axis and
  calls `resizeTracks`, updating React state only — no persist, no network.
- `pointerup` releases capture and persists once.

Minimum tile size is 120px, converted to a fraction of the current container
axis at drag start, so it is a real pixel floor at any window size. Snap: if a
boundary lands within 8px of its equal-split position it takes exactly the equal
value.

Double-clicking a divider resets that axis to equal and persists: a row divider
resets all row heights, a column divider resets its own row's widths. There is
no reset button in the header.

Divider drags cannot be confused with the existing HTML5 tile drag-and-drop
swap: that is `draggable` plus `dragstart` on the tile element, and dividers are
separate non-draggable elements handling pointer events.

## Reflow gating

`TerminalTile`'s `ResizeObserver` (`TerminalTile.tsx:183`) calls `fit()` and
`sendResize()` whenever its box changes. During a drag the boxes change every
frame, which would mean an xterm reflow and a resize message per visible tile
per frame.

A new module `web/src/term/reflowGate.ts` holds a module-level dragging flag
and a subscribe function. While the gate is closed, the `ResizeObserver`
callback returns early after setting a per-tile dirty bit. `pointerup` opens the
gate, and each dirty tile performs exactly one `fit()` + `sendResize()`.

The arbiter sees exactly what it sees today: the deferred call is
`sendResize()` with no `active` argument, so `resizeWindow` stays false for
non-owners. A drag does not claim size ownership.

## Data model and persistence

`Layout` gains two optional fields, so documents written before this change —
and the deliberately loose `isLayout` check at `GridPage.tsx:17` — keep working.
Absent means equal.

```ts
export type Layout = {
  shape: GridShape;
  tiles: Tile[];
  rowSizes?: number[];    // length rows, sums to 1
  colSizes?: number[][];  // rows x cols, each row sums to 1
};
```

`normalize` in `model.ts` is the single choke point. It gains an optional sizes
argument (existing call sites keep their current arity), computes the new shape
as it does today, then runs `normalizeSizes` against that shape. Every existing
mutation then inherits correct behaviour: `swapTiles` leaves the shape untouched
so the geometry stays put and the two sessions trade boxes; `addTile`,
`removeTile` and `setCols` reset only the tracks whose count changed.

Persistence reuses the existing plumbing. A completed drag calls `persist()`,
which PUTs the whole document to `/api/layout` and broadcasts `layout_changed`
to other tabs. Loading round-trips through `normalize` (`GridPage.tsx:222`), so
a corrupt or hand-edited `rowSizes` cannot produce a broken grid.

Sizes are fractions, so they carry across screen sizes; storing them server-side
means the proportions follow the tiles to every browser, as the tiles themselves
already do.

**No Go change.** The daemon treats the layout document as opaque JSON
(`internal/server/sessions.go:314`) and the 64KB body cap is nowhere near
threatened by at most 4xN floats.

## Testing

- `sizes.test.ts` — clamp refuses to starve a neighbour; snap lands exactly on
  the equal value inside the threshold and not outside it; the
  `normalizeSizes` keep/reset matrix (row added, row removed, cols changed,
  tiles swapped); repair of malformed input; `tileRect` geometry including gap
  insets.
- `model.test.ts` additions — sizes survive `swapTiles`, reset on `setCols`,
  documents with no sizes load as equal.
- A `GridPage` interaction test driving synthetic pointer events across a
  divider: asserts the persisted document's fractions, and that no PTY resize is
  sent mid-drag.
- `./verify.sh` before the work is called done.
