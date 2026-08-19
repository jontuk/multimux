# Mobile terminal experience

## Goal

Make multimux useful from a phone for checking a live session and making a brief,
safe intervention while the same session may still be open on a desktop.

The mobile experience should make it easy to:

- read recent output and scroll through tmux history;
- write or dictate a message, review it, then send it;
- use the few terminal keys missing from phone keyboards; and
- keep the prompt visible when the software keyboard opens.

This is not a phone IDE. Session launching, termination, layout management,
full-screen editors, and a comprehensive mobile Settings redesign remain out of
scope.

## Current state

The existing mobile shell is a good foundation. On a narrow or touch-only device,
`MobileSessionView` shows one running terminal and a compact, swipeable session
header. Only the selected terminal is mounted, and changing the mobile selection
does not alter the saved desktop grid.

The terminal itself still has a desktop input model:

- a 393 px-wide portrait viewport produces about 49 columns at the hardcoded
  13 px font size;
- tmux owns 50,000 lines of history, but a phone has no usable scroll gesture;
- iOS and Android keyboards do not provide reliable Esc, Tab, Ctrl, or arrow
  keys;
- long text and dictation go directly into the live PTY with no review step; and
- the software keyboard can either hide the prompt or resize the shared tmux
  window for every attached client.

The last point is the architectural constraint. Every binary input frame currently
calls `ArbConn.ClaimInput`, so merely sending resize frames with `active: false`
does not stop a phone from taking ownership of the shared window size.

## Recommended experience

### Preserve the desktop by default

Mobile connections should default to a passive size policy. They may resize their
own attach PTY and local xterm, but typing, composing, and scrolling must not claim
the shared tmux window size.

This requires an explicit connection capability in the PTY protocol and arbiter;
it is not a frontend-only toggle. Desktop connections keep the current behavior.

If the passive phone view is too cropped to use, expose a deliberate **Fit session
to phone** action. That action may claim the shared size, with clear copy warning
that other attached clients will reflow. The next active desktop input can reclaim
its saved size through the existing arbiter behavior.

### Use one compact mobile header

Remove the separate app header from the mobile grid route. Fold the host label,
session title, directory/branch context, position, Settings link, and mobile
terminal controls into one compact bar.

Keep horizontal session switching on this header. Do not make horizontal terminal
gestures switch sessions.

Use `viewport-fit=cover`, the existing safe-area insets, and keyboard-aware viewport
measurement. On iOS, size the terminal from `visualViewport`; on Android, opt into
`interactive-widget=resizes-content`. Debounce keyboard transitions so the terminal
refits once after the viewport settles.

### Make reading adjustable

Offer a small set of browser-local font presets, initially 13, 11, 10, and 9 px.
Persist the choice in `localStorage`; it must not become a daemon-wide setting.

Use explicit controls rather than pinch-to-zoom. Pinch competes with terminal
mouse input, selection, and browser gestures.

Map a one-finger vertical drag over the terminal to tmux mouse-wheel sequences.
This gives the phone access to tmux copy-mode and existing history without adding
xterm scrollback. Keep the first version simple: drag to scroll, tap to interact,
and no inertia.

Touch selection is deferred. It conflicts with terminal mouse handling and is less
important than reliable history navigation.

### Make compose the primary writing path

Add a visible **Compose** control that opens a multiline textarea above the keyboard.
The textarea supports normal phone editing, clipboard paste, and the keyboard's own
dictation button.

It has two explicit actions:

- **Insert** pastes the text into the foreground terminal application without an
  extra Enter;
- **Insert & Enter** pastes it, then sends Enter as a separate input event.

Use `term.paste()` so bracketed paste is honoured when the foreground application
has enabled it. Bracketed paste is not a universal safety boundary: an application
that has not enabled it may interpret embedded newlines immediately. Keep that
limitation out of normal UI copy, but cover it in tests and developer comments.

Do not replace every terminal tap with Compose. Direct focus remains available for
TUIs and short keystrokes; Compose is the deliberate path for prose and dictation.

Expose a small imperative terminal handle from `TerminalTile` rather than leaking
its WebSocket. The handle should provide semantic operations such as `input`,
`paste`, `focus`, `setFontSize`, and `fit`. The key bar, compose sheet, and scroll
gesture then share the same terminal-owned transport and connection checks.

### Add only the essential key bar

Show a sticky key row while direct terminal input or Compose is active:

`Esc` · `Tab` · `Ctrl-C` · `←` · `↑` · `↓` · `→` · `Enter`

These buttons always target the terminal, even while the Compose textarea has
focus. That makes it possible to interrupt a process without discarding a draft.
A generic sticky Ctrl, Alt, and a large punctuation palette can be added later if
real use shows they are needed.

Never assume the user's tmux prefix is `C-b`. Tmux-specific actions must use mouse
sequences or an explicit protocol operation.

## Delivery sequence

1. **Passive mobile size policy.** Add the connection capability, prevent mobile
   input from claiming shared dimensions, and add the explicit Fit session to phone
   escape hatch.
2. **Viewport and chrome.** Enable safe areas, handle the visual viewport and
   software keyboard, and collapse the two headers into one.
3. **Terminal handle and Compose.** Add Insert and Insert & Enter; keyboard dictation
   works through the textarea without a separate speech API.
4. **Essential key bar.** Add Esc, Tab, Ctrl-C, arrows, and Enter.
5. **Font presets.** Store them locally and refit without reconnecting.
6. **Touch scrollback.** Translate vertical drags into tmux wheel input and verify
   copy-mode entry and exit on real devices.

Each step should be independently usable and tested before starting the next.

## Deferred

- in-app speech recognition;
- shell-aware dictation substitutions;
- pinch font sizing;
- inertial scrolling;
- touch selection and copy-mode gesture controls;
- WebGL rendering;
- mobile session management; and
- a full mobile Settings redesign.

## Real-device release checks

Test both browser and installed-PWA modes on a current iPhone and Android phone:

- opening and closing the keyboard keeps the prompt visible;
- passive mobile input does not change the shared tmux dimensions;
- Fit session to phone claims the size deliberately, and desktop input reclaims it;
- session switching does not leak sockets or alter the desktop layout;
- font presets are readable and persist on the device;
- Compose handles multiline text, dictation, emoji, and pasted Unicode;
- Insert never adds Enter, while Insert & Enter adds exactly one;
- Esc, Tab, Ctrl-C, arrows, and Enter produce the expected terminal bytes, even
  while Compose has focus;
- vertical drag enters, moves through, and exits tmux copy-mode correctly; and
- safe-area padding works in portrait and landscape.

## Consistency notes

- Keep `MOBILE_VIEW_QUERY` synchronized between `web/src/useMediaQuery.ts` and
  `web/src/index.css`.
- Per-device presentation settings belong in browser storage. Shared daemon and
  tmux behavior belongs in the backend configuration or PTY protocol.
- Update README's mobile description when this ships. The intended promise is
  brief phone interventions, not sustained phone-first terminal work.
