# Mobile Terminal Handle and Compose Design

## Scope

Implement delivery sequence item 3 from `MOBILE.md`. The mobile grid gains a
deliberate Compose workflow for edited or dictated text, backed by a reusable
semantic handle owned by `TerminalTile`.

This item does not add the essential key bar, font presets, touch scrollback,
in-app speech recognition, or desktop compose controls. Direct terminal input
continues to work as it does today.

## Terminal handle

`TerminalTile` exposes a typed imperative `TerminalHandle` through its React
ref. The handle provides semantic operations rather than exposing xterm or its
WebSocket:

- `input(data)` sends terminal input as one binary WebSocket frame;
- `paste(text)` uses xterm's `paste()` path so bracketed paste is honoured when
  the foreground application has enabled it;
- `focus()` returns keyboard focus to xterm;
- `setFontSize(size)` updates xterm's font size and refits it; and
- `fit()` refits xterm and sends an ordinary passive resize.

Transport operations report whether the open connection accepted the request.
All operations become safe no-ops after cleanup or while disconnected. The
existing xterm `onData` handler remains the single transport path for xterm
keyboard and paste output; `input()` uses the same connection check and binary
encoding. No caller receives the socket, encoder, xterm instance, or fit addon.

The existing **Fit session to phone** behavior continues to claim the shared
size explicitly after refitting. The public handle's ordinary `fit()` operation
does not claim shared-size ownership.

## Mobile Compose workflow

`MobileSessionView` owns the Compose state and holds the selected terminal's
handle. A visible **Compose** toggle appears in the consolidated mobile terminal
controls alongside **Fit session to phone**. The toggle exposes its expanded
state to assistive technology.

When open, the composer is a bottom flex item beneath the terminal. It contains
a labelled multiline textarea and the actions **Insert** and **Insert & Enter**.
The existing visual-viewport sizing from delivery item 2 keeps this bottom item
above the software keyboard and causes the remaining terminal area to refit.
Opening Compose focuses the textarea so the phone keyboard, clipboard, and
keyboard-provided dictation are immediately available. Closing it through the
toggle retains the draft until the selected session changes or insertion
succeeds.

**Insert** passes the draft to `TerminalHandle.paste()` without adding Enter.
**Insert & Enter** first passes the complete draft to `paste()`, then sends `\r`
through `input()` as a distinct operation. This preserves the ordering and
separate terminal events required by `MOBILE.md`.

When the paste is accepted, either action clears the draft and closes Compose.
If the terminal is disconnected, the action leaves Compose open, preserves the
draft, and shows a concise connection status. The actions reject an empty draft
without generating terminal input.

Bracketed paste is controlled by the foreground terminal application. When it
is enabled, xterm wraps the text accordingly. Without it, embedded newlines may
be interpreted immediately; the implementation documents this limitation near
the paste operation and tests that multiline input is passed to xterm unchanged.

## Session and lifecycle safety

Compose is scoped to the currently selected mobile session. Changing the
selection remounts the keyed composer state, which closes it, clears its draft
and status, and points all future operations at the newly selected terminal.
This prevents a draft prepared for one session from being sent to another.

Only the selected terminal remains mounted. Unmounting a terminal invalidates
its handle before disposing xterm and closing the socket, so stale operations
cannot reach the old session. Loading and empty mobile states do not show a
Compose control because no terminal exists.

Desktop callers do not pass a terminal ref and receive no new controls or layout
changes.

## Failure and accessibility behavior

The textarea has a persistent accessible label. Compose is keyboard-operable,
and its toggle communicates the controlled panel and expanded state. The two
actions use buttons rather than implicit form submission so pressing Enter in
the textarea inserts a newline instead of sending the draft.

Paste and Enter are attempted synchronously in that order while the socket is
open, so browser close events cannot run between them. If the connection is not
open before the paste starts, neither operation is attempted and the draft is
preserved. A synchronous send failure is reported without automatically
replaying the draft, because the browser cannot prove which bytes reached the
server.

## Testing

Frontend tests cover:

- a terminal handle is populated while mounted and invalidated on cleanup;
- direct `input()` sends one encoded binary frame only while connected;
- `paste()` uses xterm's paste path and preserves multiline Unicode text;
- ordinary `fit()` remains passive while the existing phone-fit action remains
  active;
- the mobile header exposes Compose only for a selected terminal;
- opening Compose focuses a labelled multiline textarea;
- Insert pastes without Enter and then clears and closes;
- Insert & Enter pastes first and sends exactly one distinct Enter operation;
- a disconnected paste preserves the draft and reports the failure;
- closing manually retains the draft for the same session;
- switching sessions clears and closes the composer; and
- existing desktop terminal input, mobile swipe behavior, and single-terminal
  mounting remain unchanged.

Finally, run `./verify.sh` to exercise formatting, linting, all Go and web tests,
both builds, and the smoke check.
