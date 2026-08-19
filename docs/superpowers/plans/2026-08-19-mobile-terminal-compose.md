# Mobile Terminal Handle and Compose Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver `MOBILE.md` item 3 with a reusable semantic terminal handle and a safe mobile Compose workflow supporting Insert and Insert & Enter.

**Architecture:** `TerminalTile` forwards a typed handle whose methods delegate to effect-owned xterm, fit-addon, and WebSocket state without exposing those objects. A focused `MobileCompose` component owns draft state and portals its toggle into the consolidated mobile header; `MobileSessionView` keys it to the selected session and gives it the selected terminal handle.

**Tech Stack:** React 19, TypeScript 6, xterm.js 6, Vitest, Testing Library, CSS, Vite

---

## File structure

- Modify `web/src/term/TerminalTile.tsx`: export and populate the semantic terminal handle, preserving the private active phone-fit operation.
- Modify `web/src/__tests__/terminal-tile.test.tsx`: exercise connected/disconnected handle operations, xterm paste, passive fitting, font changes, and cleanup.
- Create `web/src/grid/MobileCompose.tsx`: own draft, focus, status, toggle portal, and the two insertion actions.
- Modify `web/src/grid/MobileSessionView.tsx`: hold the selected terminal ref and mount a session-keyed composer.
- Modify `web/src/__tests__/mobile-session-view.test.tsx`: provide a ref-capable terminal mock and cover Compose behavior and session safety.
- Modify `web/src/__tests__/mobile-viewport.test.tsx`: assert that the composer is a non-growing bottom flex item and its textarea can shrink within the viewport.
- Modify `web/src/index.css`: style the header toggle, bottom composer, textarea, actions, focus, and status within the mobile media query.
- Modify `README.md`: document reviewed/dictated Compose insertion and exact Enter behavior.

### Task 1: Semantic terminal handle

**Files:**
- Modify: `web/src/__tests__/terminal-tile.test.tsx`
- Modify: `web/src/term/TerminalTile.tsx`

- [ ] **Step 1: Extend the xterm test double and write failing handle tests**

In `web/src/__tests__/terminal-tile.test.tsx`, import `createRef`, import the handle type, and make the xterm double expose the behavior the public handle must drive:

```tsx
import { createRef } from "react";
import TerminalTile, { type TerminalHandle } from "../term/TerminalTile";

let dataListener: ((data: string) => void) | null = null;
const pasteCalls: string[] = [];
const focusSpy = vi.fn();
const terminalOptions: Array<{ fontSize?: number }> = [];

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options: { fontSize?: number };
    constructor(options: { fontSize?: number }) {
      this.options = { ...options };
      terminalOptions.push(this.options);
    }
    open() {}
    loadAddon(addon: unknown) {
      loadedAddons.push(addon);
    }
    onData(cb: (data: string) => void) {
      dataListener = cb;
      return {
        dispose() {
          dataListener = null;
        },
      };
    }
    paste(data: string) {
      pasteCalls.push(data);
      dataListener?.(data);
    }
    focus() {
      focusSpy();
    }
    onSelectionChange(cb: () => void) {
      selectionListener = cb;
      return {
        dispose() {
          selectionListener = null;
        },
      };
    }
    getSelection() {
      return selectionText;
    }
    getSelectionPosition() {
      return undefined;
    }
    registerLinkProvider(provider: unknown) {
      linkProviders.push(provider);
      return { dispose() {} };
    }
    write() {}
    dispose() {}
  },
}));
```

Reset `pasteCalls`, `focusSpy`, and `terminalOptions` in `beforeEach`, then add:

```tsx
function decodedBinaryFrames(ws: FakeWebSocket) {
  const decoder = new TextDecoder();
  return ws.sent.filter((data): data is Uint8Array => data instanceof Uint8Array).map((data) => decoder.decode(data));
}

test("terminal handle sends direct input only while connected", () => {
  const ref = createRef<TerminalHandle>();
  render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];

  expect(ref.current?.input("not sent")).toBe(false);
  expect(decodedBinaryFrames(ws)).toEqual([]);

  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());
  expect(ref.current?.input("hello 🌍")).toBe(true);
  expect(decodedBinaryFrames(ws)).toEqual(["hello 🌍"]);
});

test("terminal handle pastes multiline Unicode through xterm without adding Enter", () => {
  const ref = createRef<TerminalHandle>();
  render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  expect(ref.current?.paste("first\nsecond 🐚")).toBe(true);
  expect(pasteCalls).toEqual(["first\nsecond 🐚"]);
  expect(decodedBinaryFrames(ws)).toEqual(["first\nsecond 🐚"]);
});

test("terminal handle focuses, changes font size, and fits passively", () => {
  const ref = createRef<TerminalHandle>();
  const fitSpy = vi.spyOn(FitAddon.prototype, "fit");
  const { container } = render(
    <TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} sizePolicy="passive" />,
  );
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  const box = container.querySelector(".terminal-tile > div") as HTMLElement;
  Object.defineProperties(box, {
    clientWidth: { configurable: true, value: 390 },
    clientHeight: { configurable: true, value: 600 },
  });
  act(() => ws.onopen?.());

  act(() => {
    ref.current?.focus();
    ref.current?.setFontSize(11);
    ref.current?.fit();
  });

  expect(focusSpy).toHaveBeenCalledOnce();
  expect(terminalOptions[0].fontSize).toBe(11);
  expect(fitSpy).toHaveBeenCalled();
  expect(resizeFrames(ws)).not.toContainEqual(expect.objectContaining({ active: true }));
});

test("a captured terminal handle becomes inert after unmount", () => {
  const ref = createRef<TerminalHandle>();
  const { unmount } = render(<TerminalTile ref={ref} server={server} sessionId={7} onClose={() => {}} />);
  const handle = ref.current!;
  const ws = FakeWebSocket.instances[0];
  ws.readyState = FakeWebSocket.OPEN;
  act(() => ws.onopen?.());

  unmount();

  expect(ref.current).toBeNull();
  expect(handle.input("stale")).toBe(false);
  expect(handle.paste("stale")).toBe(false);
  expect(decodedBinaryFrames(ws)).toEqual([]);
});
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
cd web && pnpm test src/__tests__/terminal-tile.test.tsx
```

Expected: FAIL because `TerminalHandle` is not exported, `TerminalTile` does not accept a ref, and no handle methods exist.

- [ ] **Step 3: Implement the forwarded semantic handle**

In `web/src/term/TerminalTile.tsx`, add `forwardRef` and `useImperativeHandle` imports and export:

```tsx
export type TerminalHandle = {
  input(data: string): boolean;
  paste(data: string): boolean;
  focus(): void;
  setFontSize(size: number): void;
  fit(): void;
};

const inertTerminalOperations: TerminalHandle = {
  input: () => false,
  paste: () => false,
  focus: () => {},
  setFontSize: () => {},
  fit: () => {},
};
```

Convert the component declaration and add a delegating, stable public handle:

```tsx
const TerminalTile = forwardRef<TerminalHandle, Props>(function TerminalTile(
  { server, sessionId, onClose, autoFocus, sizePolicy = "follow-input", controlsSlot },
  ref,
) {
  const operationsRef = useRef<TerminalHandle>(inertTerminalOperations);

  useImperativeHandle(
    ref,
    () => ({
      input: (data) => operationsRef.current.input(data),
      paste: (data) => operationsRef.current.paste(data),
      focus: () => operationsRef.current.focus(),
      setFontSize: (size) => operationsRef.current.setFontSize(size),
      fit: () => operationsRef.current.fit(),
    }),
    [],
  );
```

Inside the terminal effect, after `fitToBox`, centralize checked input and install the operations:

```tsx
    let pasteAccepted: boolean | null = null;

    function sendInput(data: string) {
      if (ws?.readyState !== WebSocket.OPEN) {
        if (pasteAccepted !== null) pasteAccepted = false;
        return false;
      }
      try {
        ws.send(encoder.encode(data));
        return true;
      } catch {
        if (pasteAccepted !== null) pasteAccepted = false;
        return false;
      }
    }

    operationsRef.current = {
      input: sendInput,
      paste(data) {
        if (ws?.readyState !== WebSocket.OPEN) return false;
        pasteAccepted = true;
        try {
          // xterm adds bracketed-paste markers only when the foreground app
          // enabled that mode. Without it, embedded newlines remain live input.
          term.paste(data);
          return pasteAccepted;
        } catch {
          return false;
        } finally {
          pasteAccepted = null;
        }
      },
      focus: () => term.focus(),
      setFontSize(size) {
        term.options.fontSize = size;
        fitToBox();
        sendResize();
      },
      fit() {
        fitToBox();
        sendResize();
      },
    };
```

Change the existing `onData` body to `sendInput(data)`, reset `operationsRef.current = inertTerminalOperations` at the start of cleanup, close the forwarded component with `});`, and add `export default TerminalTile;`.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run:

```bash
cd web && pnpm test src/__tests__/terminal-tile.test.tsx
```

Expected: PASS with the handle tests and all existing connection, resize, selection, and link tests green.

- [ ] **Step 5: Commit the terminal handle**

```bash
git add web/src/term/TerminalTile.tsx web/src/__tests__/terminal-tile.test.tsx
git commit -m "feat(mobile): expose semantic terminal handle"
```

### Task 2: Mobile Compose behavior

**Files:**
- Create: `web/src/grid/MobileCompose.tsx`
- Modify: `web/src/grid/MobileSessionView.tsx`
- Modify: `web/src/__tests__/mobile-session-view.test.tsx`

- [ ] **Step 1: Make the terminal mock ref-aware and write failing Compose tests**

In `web/src/__tests__/mobile-session-view.test.tsx`, import `forwardRef`, `useImperativeHandle`, `userEvent`, and `TerminalHandle`. Add session-scoped handle spies:

```tsx
import userEvent from "@testing-library/user-event";
import { forwardRef, Profiler, useEffect, useImperativeHandle } from "react";
import type { TerminalHandle } from "../term/TerminalTile";

const terminalHandles = new Map<number, TerminalHandle>();
const terminalCalls: Array<{ sessionId: number; operation: "input" | "paste"; data: string }> = [];
let terminalConnected = true;

vi.mock("../term/TerminalTile", () => ({
  default: forwardRef(function TerminalTileMock(
    {
      sessionId,
      sizePolicy,
      controlsSlot,
    }: {
      sessionId: number;
      sizePolicy?: string;
      controlsSlot?: HTMLElement | null;
    },
    ref,
  ) {
    let handle = terminalHandles.get(sessionId);
    if (!handle) {
      handle = {
        input(data) {
          if (!terminalConnected) return false;
          terminalCalls.push({ sessionId, operation: "input", data });
          return true;
        },
        paste(data) {
          if (!terminalConnected) return false;
          terminalCalls.push({ sessionId, operation: "paste", data });
          return true;
        },
        focus() {},
        setFontSize() {},
        fit() {},
      };
      terminalHandles.set(sessionId, handle);
    }
    useImperativeHandle(ref, () => handle, [handle]);
    useEffect(() => () => unmounted(sessionId), [sessionId]);
    return (
      <div
        data-testid={`term-${sessionId}`}
        data-size-policy={sizePolicy}
        data-controls-slot={controlsSlot?.className}
      />
    );
  }),
}));
```

Reset the maps, calls, and connection flag in `beforeEach`, then add tests:

```tsx
test("Compose opens a focused multiline editor and manual close retains its draft", async () => {
  render(
    <MobileSessionView sessions={[session(1)]} toolsByServer={{ local: tools }} initialLoading={false} onRefresh={vi.fn()} />,
  );
  const toggle = screen.getByRole("button", { name: "Compose" });
  expect(toggle).toHaveAttribute("aria-expanded", "false");

  await userEvent.click(toggle);
  const editor = screen.getByRole("textbox", { name: "Compose terminal input" });
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  expect(editor).toHaveFocus();
  fireEvent.change(editor, { target: { value: "draft 🌍" } });

  await userEvent.click(toggle);
  expect(screen.queryByRole("textbox", { name: "Compose terminal input" })).not.toBeInTheDocument();
  await userEvent.click(toggle);
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("draft 🌍");
});

test("Insert pastes exactly the draft and clears and closes Compose", async () => {
  render(
    <MobileSessionView sessions={[session(1)]} toolsByServer={{ local: tools }} initialLoading={false} onRefresh={vi.fn()} />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "first\nsecond 🐚" },
  });

  await userEvent.click(screen.getByRole("button", { name: "Insert", exact: true }));

  expect(terminalCalls).toEqual([{ sessionId: 1, operation: "paste", data: "first\nsecond 🐚" }]);
  expect(screen.queryByRole("textbox", { name: "Compose terminal input" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("");
});

test("Insert & Enter pastes first and sends exactly one separate Enter", async () => {
  render(
    <MobileSessionView sessions={[session(1)]} toolsByServer={{ local: tools }} initialLoading={false} onRefresh={vi.fn()} />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "ship it" },
  });

  await userEvent.click(screen.getByRole("button", { name: "Insert & Enter" }));

  expect(terminalCalls).toEqual([
    { sessionId: 1, operation: "paste", data: "ship it" },
    { sessionId: 1, operation: "input", data: "\r" },
  ]);
});

test("a disconnected terminal preserves the Compose draft and reports it", async () => {
  terminalConnected = false;
  render(
    <MobileSessionView sessions={[session(1)]} toolsByServer={{ local: tools }} initialLoading={false} onRefresh={vi.fn()} />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  const editor = screen.getByRole("textbox", { name: "Compose terminal input" });
  fireEvent.change(editor, { target: { value: "keep me" } });

  await userEvent.click(screen.getByRole("button", { name: "Insert", exact: true }));

  expect(terminalCalls).toEqual([]);
  expect(editor).toHaveValue("keep me");
  expect(screen.getByRole("status")).toHaveTextContent("Terminal is disconnected. Draft not sent.");
});

test("empty Compose actions send nothing", async () => {
  render(
    <MobileSessionView sessions={[session(1)]} toolsByServer={{ local: tools }} initialLoading={false} onRefresh={vi.fn()} />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  expect(screen.getByRole("button", { name: "Insert", exact: true })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Insert & Enter" })).toBeDisabled();
  expect(terminalCalls).toEqual([]);
});

test("switching sessions closes Compose and discards the former session draft", async () => {
  render(
    <MobileSessionView
      sessions={[session(1), session(2)]}
      toolsByServer={{ local: tools }}
      initialLoading={false}
      onRefresh={vi.fn()}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Compose terminal input" }), {
    target: { value: "session one" },
  });

  swipe(document.querySelector<HTMLElement>(".mobile-session-header")!, { toX: 52 });

  expect(screen.getByTestId("term-2")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Compose" })).toHaveAttribute("aria-expanded", "false");
  await userEvent.click(screen.getByRole("button", { name: "Compose" }));
  expect(screen.getByRole("textbox", { name: "Compose terminal input" })).toHaveValue("");
});

test("loading and empty mobile states do not expose Compose", () => {
  const { rerender } = render(
    <MobileSessionView sessions={[]} toolsByServer={{}} initialLoading onRefresh={vi.fn()} />,
  );
  expect(screen.queryByRole("button", { name: "Compose" })).not.toBeInTheDocument();

  rerender(<MobileSessionView sessions={[]} toolsByServer={{}} initialLoading={false} onRefresh={vi.fn()} />);
  expect(screen.queryByRole("button", { name: "Compose" })).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run the focused mobile tests and verify RED**

Run:

```bash
cd web && pnpm test src/__tests__/mobile-session-view.test.tsx
```

Expected: FAIL because no Compose toggle, editor, or actions exist.

- [ ] **Step 3: Implement the focused Compose component**

Create `web/src/grid/MobileCompose.tsx`:

```tsx
import { createPortal } from "react-dom";
import { useEffect, useId, useRef, useState, type RefObject } from "react";
import type { TerminalHandle } from "../term/TerminalTile";

export default function MobileCompose({
  terminalRef,
  controlsSlot,
}: {
  terminalRef: RefObject<TerminalHandle | null>;
  controlsSlot: HTMLElement | null;
}) {
  const panelId = useId();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const [status, setStatus] = useState("");

  useEffect(() => {
    if (open) textareaRef.current?.focus();
  }, [open]);

  function insert(sendEnter: boolean) {
    if (!draft) return;
    setStatus("");
    const terminal = terminalRef.current;
    if (!terminal?.paste(draft)) {
      setStatus("Terminal is disconnected. Draft not sent.");
      return;
    }
    if (sendEnter) terminal.input("\r");
    setDraft("");
    setOpen(false);
  }

  const toggle = (
    <button
      className="mobile-compose-toggle"
      type="button"
      aria-controls={panelId}
      aria-expanded={open}
      onClick={() => {
        setStatus("");
        setOpen((current) => !current);
      }}
    >
      Compose
    </button>
  );

  return (
    <>
      {controlsSlot && createPortal(toggle, controlsSlot)}
      {open && (
        <section className="mobile-compose" id={panelId} aria-label="Compose terminal input">
          <label htmlFor={`${panelId}-textarea`}>Compose terminal input</label>
          <textarea
            id={`${panelId}-textarea`}
            ref={textareaRef}
            value={draft}
            rows={4}
            onChange={(event) => {
              setDraft(event.target.value);
              setStatus("");
            }}
          />
          <div className="mobile-compose-actions">
            <button type="button" disabled={!draft} onClick={() => insert(false)}>
              Insert
            </button>
            <button type="button" disabled={!draft} onClick={() => insert(true)}>
              Insert &amp; Enter
            </button>
          </div>
          {status && <p role="status">{status}</p>}
        </section>
      )}
    </>
  );
}
```

- [ ] **Step 4: Wire Compose to the selected terminal and session lifecycle**

In `web/src/grid/MobileSessionView.tsx`, import `useRef`, `MobileCompose`, and `TerminalHandle`, then add:

```tsx
  const terminalRef = useRef<TerminalHandle | null>(null);
```

Replace the selected-terminal branch with:

```tsx
        <div className="mobile-terminal">
          <TerminalTile
            ref={terminalRef}
            key={selected.key}
            server={selected.server}
            sessionId={selected.session.id}
            onClose={onRefresh}
            sizePolicy="passive"
            controlsSlot={controlsSlot}
          />
          <MobileCompose key={selected.key} terminalRef={terminalRef} controlsSlot={controlsSlot} />
        </div>
```

The session key intentionally remounts `MobileCompose`, clearing a draft that belonged to the old session.

- [ ] **Step 5: Run the focused mobile tests and verify GREEN**

Run:

```bash
cd web && pnpm test src/__tests__/mobile-session-view.test.tsx src/__tests__/terminal-tile.test.tsx
```

Expected: PASS, including existing swipe, selection reconciliation, control portal, and single-mounted-terminal cases.

- [ ] **Step 6: Commit Compose behavior**

```bash
git add web/src/grid/MobileCompose.tsx web/src/grid/MobileSessionView.tsx web/src/__tests__/mobile-session-view.test.tsx
git commit -m "feat(mobile): add terminal compose workflow"
```

### Task 3: Mobile layout, documentation, and complete verification

**Files:**
- Modify: `web/src/__tests__/mobile-viewport.test.tsx`
- Modify: `web/src/index.css`
- Modify: `README.md`

- [ ] **Step 1: Write a failing layout-contract test**

Add to `web/src/__tests__/mobile-viewport.test.tsx`:

```tsx
test("Compose stays below the shrinking terminal and above the keyboard safe area", () => {
  const styles = readFileSync(resolve(process.cwd(), "src/index.css"), "utf8");
  const mobileTerminal = styles.match(/\.app\.grid-route \.mobile-terminal\s*\{([^}]*)\}/s)?.[1];
  const terminalTile = styles.match(/\.app\.grid-route \.mobile-terminal > \.terminal-tile\s*\{([^}]*)\}/s)?.[1];
  const compose = styles.match(/\.app\.grid-route \.mobile-compose\s*\{([^}]*)\}/s)?.[1];
  const textarea = styles.match(/\.app\.grid-route \.mobile-compose textarea\s*\{([^}]*)\}/s)?.[1];

  expect(mobileTerminal).toMatch(/flex-direction:\s*column/);
  expect(mobileTerminal).toMatch(/padding-bottom:\s*env\(safe-area-inset-bottom\)/);
  expect(terminalTile).toMatch(/flex:\s*1 1 auto/);
  expect(terminalTile).toMatch(/min-height:\s*0/);
  expect(compose).toMatch(/flex:\s*none/);
  expect(textarea).toMatch(/max-height:/);
  expect(textarea).toMatch(/resize:\s*vertical/);
});
```

- [ ] **Step 2: Run the layout test and verify RED**

Run:

```bash
cd web && pnpm test src/__tests__/mobile-viewport.test.tsx
```

Expected: FAIL because `.mobile-compose` and its terminal-flex contract are not styled.

- [ ] **Step 3: Add the compact mobile Compose styling**

Inside the existing mobile media query in `web/src/index.css`, add:

```css
  .app.grid-route .mobile-terminal > .terminal-tile {
    flex: 1 1 auto;
    min-height: 0;
  }

  .app.grid-route .mobile-terminal-controls {
    gap: 0.35rem;
  }

  .app.grid-route .mobile-terminal-controls button,
  .app.grid-route .mobile-compose-actions button {
    border: 1px solid var(--border);
    border-radius: 4px;
    background: color-mix(in srgb, var(--panel) 90%, transparent);
    color: var(--ink);
    font: inherit;
  }

  .app.grid-route .mobile-compose-toggle {
    padding: 0.2rem 0.4rem;
  }

  .app.grid-route .mobile-compose {
    display: grid;
    flex: none;
    gap: 0.4rem;
    padding: 0.5rem calc(0.5rem + env(safe-area-inset-right)) 0.5rem
      calc(0.5rem + env(safe-area-inset-left));
    border-top: 1px solid var(--border);
    background: var(--panel);
  }

  .app.grid-route .mobile-compose label {
    color: var(--muted);
    font-family: var(--mono);
    font-size: 0.75rem;
  }

  .app.grid-route .mobile-compose textarea {
    box-sizing: border-box;
    width: 100%;
    max-height: min(30dvh, 12rem);
    min-height: 4.5rem;
    resize: vertical;
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0.5rem;
    background: var(--bg);
    color: var(--ink);
    font: 1rem/1.4 system-ui, sans-serif;
  }

  .app.grid-route .mobile-compose textarea:focus-visible,
  .app.grid-route .mobile-compose button:focus-visible,
  .app.grid-route .mobile-compose-toggle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .app.grid-route .mobile-compose-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }

  .app.grid-route .mobile-compose-actions button {
    min-height: 2.25rem;
    padding: 0.35rem 0.65rem;
  }

  .app.grid-route .mobile-compose p[role="status"] {
    margin: 0;
    color: var(--error);
    font-family: var(--mono);
    font-size: 0.75rem;
  }
```

- [ ] **Step 4: Document the shipped mobile writing workflow**

After the mobile viewport paragraph in `README.md`, add:

```markdown
For longer text or phone dictation, use **Compose**. It opens a multiline editor
for normal mobile editing and review. **Insert** pastes the draft into the
foreground application without Enter; **Insert & Enter** performs the same paste
and then sends exactly one Enter. Direct terminal focus remains available for
short keystrokes and interactive programs.
```

- [ ] **Step 5: Format and run focused frontend checks**

Run:

```bash
cd web && pnpm format
cd web && pnpm test src/__tests__/terminal-tile.test.tsx src/__tests__/mobile-session-view.test.tsx src/__tests__/mobile-viewport.test.tsx
cd web && pnpm lint
```

Expected: formatting completes; all focused tests PASS; ESLint and Prettier checks exit with no errors or warnings.

- [ ] **Step 6: Run the complete repository verification**

Run:

```bash
./verify.sh
```

Expected: gofmt, `go vet`, all Go tests, frontend lint, all frontend tests, frontend build, Go build, and smoke check PASS with no errors or warnings.

- [ ] **Step 7: Commit layout and documentation**

```bash
git add web/src/__tests__/mobile-viewport.test.tsx web/src/index.css README.md
git commit -m "docs(mobile): describe terminal compose"
```

- [ ] **Step 8: Review the final delivery against the spec**

Run:

```bash
git diff 9de647a..HEAD --check
git status --short
```

Expected: no whitespace errors and a clean working tree. Review the committed diff against `MOBILE.md` item 3 and `docs/superpowers/specs/2026-08-19-mobile-terminal-compose-design.md`, paying particular attention to exact Enter framing, draft retention on disconnect, stale-handle cleanup, session switching, and desktop non-regression.
