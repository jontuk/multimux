# Per-directory view layout

Extends `2026-08-13-dir-filter-solo-design.md`. The solo semantics there are
unchanged; this spec gives each soloed directory its own arrangement, and
replaces the "What does not change" section's account of splitter handling.

## Goal

A soloed directory keeps the arrangement the user gave it. Column count,
splitter positions, and tile order are remembered per directory, so switching
to another directory and back — or out to the unfiltered grid and back — brings
the arrangement back with it.

Today none of that survives. `viewSizes` holds splitter drags only for the life
of the current selection and is discarded whenever the solo changes; the column
stepper and tile drag-swap write straight through to the stored layout, so an
edit made while filtered rearranges the unfiltered grid too.

## What stays server-side

`/api/layout` is unchanged: same document, same events, same round-trip. It
remains the record of *which sessions are placed in the grid*, which is
genuinely shared — a second tab or a phone should see the same tiles — plus the
arrangement of the unfiltered view.

Per-directory arrangement is presentation, and is browser-local for the same
reason the solo itself is (`dirFilter.ts`): it describes what this browser
renders and nothing else. This leaves a deliberate split — the unfiltered
view's presentation lives server-side, each soloed view's lives in
`localStorage`. Closing that split means moving all presentation out of the
layout document, which is a store and API change this spec does not take on.

## Model

A new module `web/src/grid/viewLayout.ts`, sibling to `dirFilter.ts`:

```ts
type Overlay = { cols: number; order: string[]; rowSizes: number[]; colSizes: number[][] };
// localStorage "multimux.viewLayout" -> Record<dirPath, Overlay>
```

Keyed by full directory path, the same key `dirButtons` and the stored solo
use. Only soloed views have overlays; the unfiltered view (`activeSolo ===
null`) has none, by construction — it is the stored layout.

`order` holds `tileKey` strings (`serverId:sessionId`). Indices into a filtered
list go stale the moment a session is launched or terminated; keys do not.

Reads tolerate anything: absent key, corrupt JSON, a non-object value, or an
entry whose fields are the wrong shape all read as "no overlay", which renders
as today. Nothing is migrated from a previous version, because there was
nothing persisted.

A directory has no overlay until the first edit is made in its view. That edit
writes an overlay seeded from what is on screen at the time — so the view
inherits the stored column count and its current equal or derived sizes rather
than jumping to a default.

## Render pipeline

With no effective solo: unchanged. `layout` renders directly, tile indices are
real indices, no overlay is consulted.

With an effective solo:

1. `filterLayout(layout, …)` as today, giving the visible tiles in stored order
   and `map`, each view slot's index in the stored layout.
2. `applyOverlay` reorders those `(tile, realIndex)` pairs by `order`: keys
   named in `order` come first in that sequence, keys absent from it append in
   stored order, and `order` entries naming no visible tile are ignored. `map`
   is rebuilt alongside the reorder — it is what remove, terminate, and swap
   target, so it must follow the tiles rather than the original filter order.
3. `normalize(ordered, overlay.cols, overlay.rowSizes, overlay.colSizes)`.
   `normalizeSizes` already resets any track array whose count no longer
   matches the shape, so a session appearing or leaving heals the sizes without
   extra code, exactly as it does for the stored layout.

Step 2's append rule is what makes a launch into a soloed directory behave:
the new tile lands at the end of that view, and the rest keeps its places.

## Writes

While a solo is in effect:

- Column stepper writes `overlay.cols`. The stored layout's column count is
  left alone.
- A divider drag writes `overlay.rowSizes` / `overlay.colSizes`.
- A tile drag-swap reorders `overlay.order` — computed over the view's key
  sequence, then stored.

None of the three touch the stored layout, which is the point: the unfiltered
grid no longer changes under an edit made inside a filtered view.

Membership changes still write the stored layout, because membership is shared:

- Terminate and remove call `persist((l) => removeTile(l, map[i]))` as today. No
  overlay is rewritten; the departed key simply names no tile on the next
  render and is ignored by step 2.
- Launch and attach append to the stored layout as today.

## What goes away

- `viewSizes` state and the effect that clears it on every solo change.
- The `filtering` heuristic — "the view actually dropped a tile", not "a solo
  is set". It existed only because a drag made in a view that hid nothing would
  have been silently discarded on reload. Overlays persist, so the gate becomes
  plain `activeSolo !== null`, and a solo that happens to hide nothing keeps its
  arrangement like any other.

## Not doing

Overlays are not pruned when a directory stops appearing in the button list.
`dirFilter.ts` deliberately keeps a stale solo so a briefly unreachable daemon
or a page load before sessions arrive does not lose the selection; the same
reasoning applies here, and an overlay is a few numbers and a short string
array. There is no reset-this-view control.

## Testing

Vitest over the pure surface first, then the integration:

- `view-layout.test.ts`: storage round-trip, absent key, corrupt JSON,
  wrong-shaped entry; order merge when a session is added (appends), removed
  (ignored), and both at once; unknown keys append in stored order; sizes reset
  when the row or column count changes; `cols` clamped through `normalize`.
- `grid-page.test.tsx`: solo a directory, change columns and drag a tile, solo
  another, come back — arrangement restored; the unfiltered grid unchanged by
  those edits; terminating under one solo removes the tile from every view;
  launching into a soloed directory appends rather than reshuffling.

Then `./verify.sh`.
