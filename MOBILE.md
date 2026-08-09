# Mobile experience: findings and options

Scope: making a **live tmux session** easier to **read**, **write to**, and **dictate to** from a
phone. Session management on mobile (launcher, kill, layout) is deliberately out of scope here.

Everything in "Measured" below was observed on a running daemon, not inferred. Reproduce with the
probe recipe at the end.

---

## 1. What exists today

`web/src/grid/MobileSessionView.tsx` is the whole mobile surface. Below 560 CSS px, or on any
coarse-pointer/no-hover device (`MOBILE_VIEW_QUERY`, `web/src/useMediaQuery.ts:8`, mirrored in
`web/src/index.css:1245`), the grid is replaced by a single full-bleed `TerminalTile` plus a compact
header. Horizontal swipe on that header moves between sessions. Only the selected terminal is
mounted.

That is a good skeleton. What is missing is everything *inside* the terminal: the phone gets a
desktop terminal at a desktop font with a desktop input model.

### Measured (iPhone 14 Pro metrics, 393×852 CSS px, DPR 3)

| | portrait 393×852 | landscape 852×393 |
| --- | --- | --- |
| terminal box | 384 × 765 px | 837 × 300 px |
| **grid** | **49 cols × 51 rows** | **106 cols × 20 rows** |
| chrome above terminal | 71 px (app header 44 + session header 28) | 71 px — **18% of the short side** |

Cell size is 7.84 × 15 px at the hardcoded `fontSize: 13` (`web/src/term/TerminalTile.tsx:63`).

- **49 columns** is the core reading problem. Anything assuming 80 wraps: `git status` hints, `ls -l`,
  test output, Claude Code's TUI. The screenshot of `fatal: not a git repository (or any of the
  parent directories): .git` wraps mid-word in portrait.
- **`scrollback: 0`** (`TerminalTile.tsx:61`) is correct — tmux owns history (`history-limit 50000`,
  `internal/tmuxmgr/manager.go:64`) — but it means **there is no way to scroll back on a phone at
  all**. tmux copy-mode is reachable only via the prefix key, and there is no key on the iOS keyboard
  that produces `Ctrl`.
- **No modifier keys.** No Esc, Tab, Ctrl, Alt, arrows, `|`, `~`, `/`. That rules out vim, `Ctrl-C`,
  `Ctrl-R`, tab completion, and Claude Code's Esc-to-interrupt.
- **No font-size control** anywhere in the UI or in `internal/config`.
- The xterm helper textarea already carries `autocorrect=off autocapitalize=off spellcheck=false` —
  good. It has no `inputmode`/`enterkeyhint`.

### Two structural gotchas found while measuring

**(a) The soft keyboard resizes the *shared* tmux window.**
`window-size manual` (`manager.go:79`) plus the arbiter (`internal/tmuxmgr/arbiter.go`) means the
connection that most recently typed owns the window size. On Android, opening the keyboard shrinks
the layout viewport → `ResizeObserver` → `fit()` → resize frame. Simulated by shrinking the viewport
to 393×516 after one keystroke from the phone tab:

```
before  = 49x51
after   = 49x29      # every other attached client now sees 29 rows
```

So on a phone, *every keyboard open and close reflows the desktop's view of that session.* On iOS it
fails the other way: Safari does not shrink `dvh` or `innerHeight` for the keyboard (only
`visualViewport`), nothing in `web/src` listens to `visualViewport`, so the terminal keeps rendering
51 rows and the prompt sits **behind** the keyboard.

**(b) `env(safe-area-inset-*)` is currently dead code.**
`index.css:1261` and `:1290` pad for the notch and home indicator, but `web/index.html:5` is
`width=device-width, initial-scale=1.0` — without `viewport-fit=cover` those insets resolve to `0px`
on iOS, and the page is letterboxed inside the safe area instead of using the full screen. Fixing
this is worth ~1 row portrait and a chunk of width in landscape.

---

## 2. Two capabilities verified over the wire

Both matter because they mean the interesting work is **frontend-only**. `internal/server/ws.go`
already forwards every binary frame straight to the PTY (`ws.go:154`); text frames are resize JSON
and nothing else. No new protocol, no new endpoint.

**Touch-drag scrollback works via synthesized mouse-wheel escapes.** tmux mouse mode is on
(`manager.go:85`), and its default wheel binding enters copy-mode. Sending SGR wheel bytes down the
existing PTY socket from page JS:

```js
ws.send(enc.encode("\x1b[<64;10;10M"));   // wheel up   ×3
// tmux: in_mode=1 scroll=10
ws.send(enc.encode("\x1b[<65;10;10M"));   // wheel down ×8
// tmux: in_mode=0            (copy-mode -e exits at the bottom)
```

That is a complete scrollback story for touch with zero backend change.

**Bracketed paste delivers multi-line text without executing it.**

```js
ws.send(enc.encode("\x1b[200~echo 'dictated line one'\necho two\x1b[201~"));
```

lands both lines in the zsh line editor, unexecuted, waiting for review. This is the primitive a
compose/dictate box needs; `term.paste(text)` in xterm 6 emits exactly this when the app has
bracketed paste on, and `term.options.fontSize` is live-settable.

---

## 3. Options

Grouped by the three goals. Each is independent; the sequencing suggestion is in §4.

### Read

**R1 — Font size control (per device).**
`fontSize` moves from the hardcoded 13 to a value the user picks, applied via
`term.options.fontSize`. Columns gained, from the measured 7.84 px cell:

| fontSize | cols portrait | cols landscape | rows portrait |
| --- | --- | --- | --- |
| 13 (today) | 49 | 106 | 51 |
| 11 | 57 | 126 | 60 |
| 10 | 63 | 138 | 66 |
| 9 | 70 | 154 | 73 |
| 8 | 79 | 173 | 82 |

80 columns in portrait needs ~8 px type — legible on a DPR-3 screen, unpleasant for long sessions.
Landscape reaches 80 at any size. **This must be browser-local (`localStorage`, like
`web/src/servers.ts`), not `internal/config`** — a daemon-side setting would push the phone's font
onto the desktop. A pinch-to-zoom gesture mapped to font size is the natural mobile control; a −/+
pair in the session header is the boring, testable one. Do both if the gesture proves reliable.

**R2 — Touch-drag scrollback (verified in §2).**
One-finger vertical drag on the terminal → SGR wheel sequences, ~1 notch per 3 rows of travel, with
inertia. Notes:
- Horizontal swipe already means "switch session" on the header only, so a vertical-drag claim on
  the terminal body does not collide with it.
- tmux mouse mode owns click-drag, so today a tap already sends press+release to tmux. Decide a
  policy: plain drag = scroll, long-press-then-drag = pass mouse through (selection). Whatever is
  chosen, it must be one rule, documented next to the `shiftDragCapture` comment in
  `TerminalTile.tsx:150`.
- Add an on-screen affordance for "you are in copy-mode" — tmux shows nothing with `status off`.

**R3 — Reclaim vertical chrome.**
71 px of header is 18% of a landscape phone. Options: hide the app header on the grid route on
mobile and fold the host label into the session header (−44 px ≈ 3 rows); or auto-hide both on scroll
/ on keyboard-open and restore on tap. Cheap, immediately felt in landscape.

**R4 — Fix the viewport contract.**
`viewport-fit=cover` in `web/index.html` (makes the existing `env(safe-area-inset-*)` padding real)
plus `interactive-widget=resizes-content` (Chrome/Android: keyboard shrinks the layout viewport
predictably) plus a `visualViewport` `resize`/`scroll` listener so iOS shrinks the terminal instead of
hiding the prompt behind the keyboard. Small diff, fixes the worst iOS symptom. Pair with R7.

**R5 — Stop the phone hijacking the shared window.**
A "don't resize the shared session from this device" toggle: keep sending `active: false` so the
arbiter never grants ownership (`arbiter.go:80`). Caveat found while reading `attach.go:40`: with
`window-size manual`, a non-owner client smaller than the window gets a **cropped** view — tmux will
not pan. So this toggle is only usable in combination with R1, i.e. when the phone's font is small
enough that its column count already meets or exceeds the desktop's. Worth having; not worth having
alone.

**R6 — `@xterm/addon-webgl`.** Not installed. Battery and scroll smoothness on a phone; costs a
dependency and a context-loss fallback path. Low priority, easy win if R2 makes scrolling frequent.

**R7 — Keyboard-aware layout.** When the keyboard is open, the useful terminal is ~20 rows portrait.
Re-fit to the *visual* viewport so the prompt is the bottom line, and keep the last row pinned.

### Write

**W1 — On-screen key bar.** A sticky row above the keyboard: `Esc Tab Ctrl Alt ↑↓←→ / | ~ -` plus
`Ctrl-C`. Requirements found in the code:
- `TerminalTile` currently traps the WebSocket inside its effect closure; a `send(bytes)` handle (or
  a `Terminal` ref calling `term.input()`) has to be lifted out for anything outside the tile to
  write. That refactor is the gate on W1, W2 and W3 alike — do it once.
- **Do not assume the prefix is `C-b`.** Sessions inherit the user's `~/.tmux.conf`. Anything
  tmux-level (copy-mode, pane switching) should go through mouse sequences or an explicit API, never
  a guessed prefix.
- Sticky modifiers (tap Ctrl, then a letter) beat chording on a touchscreen.
- `extended-keys on` and `xterm*:extkeys` are already set (`manager.go:87`), so CSI-u encodings are
  available for the awkward combinations, the same route `Shift+Enter` already takes
  (`TerminalTile.tsx:144`).

**W2 — Compose bar.** A multi-line textarea with **Send** and **Send + Enter**, delivered via
`term.paste()` (bracketed paste, verified above). This is the single highest-value item for writing
prose-shaped input on a phone: it gives an edit-before-commit step, survives autocorrect, handles
multi-line, and makes long prompts to a CLI agent bearable. It is also the whole answer for
dictation (D1).

**W3 — Explicit Paste button** using `navigator.clipboard.readText()` inside the tap gesture. Long-
press paste over xterm's 1-character-wide hidden textarea is unreliable on both platforms today. The
copy direction is already handled (OSC 52 + `ClipboardAddon`, `manager.go:91`).

**W4 — Touch selection.** Give copy-mode yank a touch path (see R2's long-press policy); OSC 52 then
puts the result on the system clipboard with no further work.

**W5 — Textarea attributes.** After `term.open()`, set `enterkeyhint="send"` and `autocomplete="off"`
on `.xterm-helper-textarea`. Two lines, marginal but free.

### Dictate

**D1 — Dictate into the compose bar (recommended).**
The keyboard's own mic button fills W2's textarea; the user reads it, fixes "cd slash user slash
jon", and taps Send. Nothing new to build once W2 exists, no permission prompt, on-device on iOS,
works inside an installed PWA. This is the option that matches how dictation actually behaves —
phrase-at-a-time, with punctuation guesses that need correcting.

**D2 — In-app mic (Web Speech API).**
`webkitSpeechRecognition` gives a mic button that does not depend on the keyboard, and supports
continuous/interim results. Costs: on iOS Safari the audio goes to Apple, on Android Chrome to
Google — a real conflict with a tool whose entire premise is "nothing leaves your network"; support
inside standalone iOS PWAs has historically been unreliable; needs a mic permission. If built, it
should be opt-in, off by default, and called out in README's security section. **D1 first, D2 only
if D1 proves too clumsy.**

**D3 — Shell-aware dictation transforms.** Map spoken `slash → /`, `dash → -`, `dot → .`, `new line
→ \n`, strip the trailing period dictation adds. Only safe inside W2's textarea where the user sees
the result before it runs. Keep it a toggle; it is the kind of feature that is delightful for two
days and infuriating on the third.

**D4 — What already happens today (and why it is dangerous).**
xterm 6 forwards `insertText` input events straight to the PTY (`_inputEvent` in
`node_modules/@xterm/xterm/lib/xterm.js`), so **dictation into a focused terminal already types into
the live shell**, uncorrected and unreviewable, with no Enter. That is a footgun rather than a
feature. Once W2 exists, consider making a tap on the terminal open the compose bar instead of
focusing the raw textarea on mobile — with an escape hatch for people who want direct keys.

---

## 4. Suggested order

1. **R4** (viewport meta + `visualViewport`) and **R3** (chrome) — smallest diffs, both fix "I can't
   see the prompt", the most acute complaint.
2. **Lift a `send`/`Terminal` handle out of `TerminalTile`** — the shared prerequisite for W1/W2/W3.
3. **W2 compose bar** → immediately delivers **D1 dictation** and most of the writing story.
4. **W1 key bar** — the other half of writing; Esc and Ctrl-C are what make a CLI agent usable.
5. **R1 font size** (localStorage) — the reading fix, and the precondition for R5.
6. **R2 touch scrollback** — verified mechanism, but the gesture policy against tmux mouse mode
   needs the most design care of anything here.
7. Optional: R5, R6, D2, D3.

## 5. To confirm on real hardware

The measurements above come from Chrome device emulation and a real daemon; these need actual
phones:

- iOS soft-keyboard behaviour with `interactive-widget` and `visualViewport` (emulation cannot
  reproduce the keyboard).
- Whether a tap reliably focuses xterm and raises the keyboard in an installed PWA.
- Dictation into a plain textarea vs. into xterm — punctuation, commit timing, `insertText` shape.
- `navigator.clipboard.readText()` inside an installed PWA on both platforms.
- Legibility of 9–10 px type on a real screen, which no emulator can answer.

## 6. Consistency notes if any of this ships

- README currently lists "sustained phone-first terminal work" as an explicit **non-goal**
  (~line 506). Anything past R3/R4 contradicts it — update that paragraph rather than leaving it
  stale.
- `MOBILE_VIEW_QUERY` is duplicated in `web/src/useMediaQuery.ts:8` and `web/src/index.css:1245`;
  any new mobile-only CSS has to respect that pairing.
- Per-device settings belong in `localStorage` next to `web/src/servers.ts`. Only genuinely
  daemon-wide behaviour belongs in `internal/config`.

---

## Probe recipe (how the numbers above were obtained)

```bash
export MULTIMUX_DATA_DIR=$(mktemp -d)
go run . serve --dev --port 8787 &                  # prints a setup code; ignore it

# Skip the passkey ceremony: a credential row clears setup-pending (SetupPending
# counts credentials), and an auth_sessions row mints a usable cookie/bearer token.
TOKEN=$(python3 -c "import secrets,base64;print(base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip('='))")
HASH=$(python3 -c "import hashlib,sys;print(hashlib.sha256(sys.argv[1].encode()).hexdigest())" "$TOKEN")
sqlite3 "$MULTIMUX_DATA_DIR/multimux.db" \
  "insert into credentials values ('x','k','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
   insert into auth_sessions values ('$HASH','probe','2026-08-09T00:00:00Z','2026-09-09T00:00:00Z');"

curl -sk -X POST https://localhost:8787/api/sessions -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"ToolID":1,"DirID":1}'

cd web && MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev   # http://localhost:5173
```

In the browser (dev mode allows the `http://localhost:5173` origin, so the WS works):
`document.cookie = "mm_session=<TOKEN>; path=/; SameSite=Strict; Secure"`, reload, then emulate a
phone viewport. Inspect the live tmux side with
`tmux -L multimux-dev-<hash> display-message -p -t mm-1 '#{window_width}x#{window_height} #{pane_in_mode}'`.

Tear down: kill the daemon and vite, then `tmux -L multimux-dev-<hash> kill-server`.
