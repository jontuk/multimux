# Directory filter buttons: solo instead of hide

Supersedes the filter semantics in `2026-08-12-dir-filter-buttons-design.md`.
Everything that spec says about placement, tinting, index translation, and
splitter handling still holds; only what a click *means* changes, and the
state behind it.

## Goal

A click on a directory button shows that directory and nothing else. A click
on the button that is already soloed goes back to showing every directory. A
click on a different button while one is soloed moves the solo there.

The old semantics — a click hides that directory, and isolating one repo means
clicking every other button — read as jarring in use. Isolating one directory
is the common case and should cost one click.

## Model

State is one nullable path, not a set:

- `soloDir(): string | null` / `setSoloDir(path: string | null)`, backed by
  `localStorage` under `multimux.soloDir`, holding a JSON string or `null`.
  Malformed or unparsable storage reads as `null`. The old
  `multimux.hiddenDirs` key is no longer read or written; a value left in a
  browser from the previous behaviour is ignored, not migrated.
- `null` means every directory shows. That is the default, so a directory seen
  for the first time shows without being enumerated anywhere.

The stored solo is never cleared as a side effect of what the daemon reports.
Instead the *effective* solo is derived per render: if the stored path names no
directory in the current button list, this render behaves as if nothing were
soloed. A soloed directory whose last session ends therefore falls back to
showing everything, and so does a page load before sessions arrive or a remote
daemon that is briefly unreachable — but the selection returns intact when its
directory does. This is the reason the count-0 button the previous design added
for stale hidden entries is no longer needed: a solo can never outlive its
button, because a solo with no button is not in effect.

## Visibility

With no effective solo, behaviour is exactly as before the filter existed:
`layout` renders directly and tile indices are real indices.

With an effective solo, a tile shows if and only if its session's directory
equals the solo. Two consequences differ from the previous design:

- A tile whose session is unknown — the server was removed, or the session is
  not in `sessionsByServer` yet — now hides. The carve-out that kept it visible
  existed because a hidden directory could lose its button; a soloed directory
  by construction has one on screen, so the escape is always a click away.
- A tile holding an *ended* session still shows when its directory is soloed:
  visibility is decided by directory whatever the status. Only the buttons are
  limited to running sessions.

The unplaced-session quick-add buttons are filtered by the same predicate.

`dirButtons(servers, sessionsByServer)` returns to its original signature —
one entry `{ path, name, count }` per distinct directory of a running session,
sorted by `name` then `path`. It no longer takes the hidden set.

Launching or attaching a session into a directory that is not the solo clears
the solo, so the new tile is visible. Moving the solo to the new session's
directory instead would silently change what else is on screen, which is not
what attaching from an empty tile asks for.

## What does not change

`filterLayout`, the view→stored index map, and every mutation translating
through it are untouched: `removeTile(map[i])`, `swapTiles(map[from], map[to])`,
terminate by session id. Hidden tiles still unmount and reattach on return.

Splitter handling is untouched, including its predicate. `filtering` stays
"the view actually dropped a tile", not "a solo is set" — so a solo that hides
nothing (one directory in use, and it is the soloed one) still persists drags
normally, and `viewSizes` still resets whenever the selection changes.

## Appearance

Same tinted pill, same `dirTintStyle` source, same `aria-pressed` mechanism,
with the states re-cast:

- No effective solo: every button reads unpressed and undimmed. Nothing is
  filtered, so nothing should look switched off.
- An effective solo: that button reads pressed; every other button takes the
  existing dimmed-and-dashed treatment.

Titles become `show only sessions in <path>` on an unpressed button and
`show all directories` on the pressed one.

## Testing

Vitest over the pure surface, written before the implementation:

- Storage: round-trip, absent key, corrupt JSON, explicit `null`.
- The effective-solo derivation: a stored path with a matching button is in
  effect; a stored path with no matching button is not, and storage is left
  alone.
- Button list: unchanged behaviour, now without the hidden-set parameter.
- Grid integration: click solos, click again clears, click a second button
  moves the solo; quick-adds follow; attaching into a non-soloed directory
  clears the solo; index translation still writes the right tile.

Then `./verify.sh`, which covers lint, the web build, and the Go side.
