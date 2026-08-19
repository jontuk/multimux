# Mobile Essential Key Bar Design

## Scope

Implement delivery sequence item 4 from `MOBILE.md`. The selected mobile
terminal gains a focus-only key row containing Esc, Tab, Ctrl-C, the four arrow
keys, and Enter. These controls target the terminal whether direct terminal
input or Compose currently has focus.

This item does not add sticky modifier keys, Alt, punctuation palettes, font
presets, touch scrollback, touch selection, or desktop key controls. It also
applies the approved Compose copy refinement: rename **Insert** to **Add**,
rename **Insert & Enter** to **Add & Enter**, and remove the visible Compose
label while retaining an accessible textarea name.

## Component structure

Add a focused `MobileKeyBar` component alongside `MobileCompose`. It receives
the selected terminal's existing `TerminalHandle` ref and owns the fixed mapping
from visible key labels to terminal input bytes:

- Esc sends `\x1b`;
- Tab sends `\t`;
- Ctrl-C sends `\x03`;
- Left, Up, Down, and Right send `\x1b[D`, `\x1b[A`, `\x1b[B`, and `\x1b[C`;
  and
- Enter sends `\r`.

Each activation calls `TerminalHandle.input()` exactly once. The component does
not receive xterm, the WebSocket, or transport encoding details. It remains a
mobile-only child of `MobileSessionView`, rendered after `MobileCompose` so the
approved layout is terminal, optional Compose panel, then the bottom-most key
row.

## Focus and keyboard behavior

The key bar remains mounted for the selected session but CSS hides it until
`.mobile-terminal:focus-within`. It therefore appears while xterm's input,
Compose, or the key bar itself has focus and consumes no terminal height while
the mobile view is idle.

Pointer-down on a key button prevents the browser's default focus transfer.
The subsequent click still sends the key while leaving the xterm input or
Compose textarea focused. This keeps the software keyboard open and ensures a
key press never changes, clears, or submits a Compose draft. Keyboard-triggered
button activation remains supported through native buttons.

The row uses compact, touch-sized native buttons in the order specified by
`MOBILE.md`: Esc, Tab, Ctrl-C, Left, Up, Down, Right, Enter. Arrow glyphs are
visible, while explicit accessible names identify each arrow direction.

## Connection and lifecycle behavior

`TerminalHandle.input()` already checks connection state and catches synchronous
send failures. The key bar relies on that contract: a disconnected key press is
a safe no-op. It does not display a persistent failure status because an
individual control-key press has no draft to preserve or safe replay action.

The component reads `terminalRef.current` at activation time, so it always
targets the currently selected terminal. Changing sessions remounts the keyed
mobile content and retargets the ref. Loading and empty states render neither a
terminal nor a key bar.

## Compose copy refinement

The Compose textarea keeps its programmatic accessible name, **Compose terminal
input**, but its visible label is removed to save vertical space. Its action
labels become **Add** and **Add & Enter**. Their existing behavior is unchanged:
Add pastes without Enter, while Add & Enter pastes and then sends one distinct
carriage return.

## Testing

Frontend behavior tests cover:

- all eight visible controls in their specified order;
- the exact byte sequence sent by every control;
- exactly one `input()` operation per activation;
- operation while Compose retains focus and its draft remains unchanged;
- pointer-down preserving the current terminal or Compose focus;
- the bar being absent from loading and empty states;
- session switching targeting the newly selected terminal;
- disconnected input remaining a safe no-op;
- focus-only visibility and bottom-most layout rules;
- the Compose label remaining accessible but not visible; and
- the revised Add and Add & Enter copy preserving insertion semantics.

Finally, run `./verify.sh` to exercise formatting, linting, all Go and web tests,
both builds, and the smoke check.
