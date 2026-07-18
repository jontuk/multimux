# Maximize Session Tile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Double-clicking a session tile's header maximizes that tile to fill the viewport; double-clicking again or pressing Escape restores the grid.

**Architecture:** `GridPage` holds ephemeral `maximizedKey: string | null` state keyed by `tileKey` (`serverId:sessionId`). The maximized tile's `.tile` element gets a `tile-maximized` class styled as a fixed full-viewport overlay. The tile's DOM node stays mounted, so `TerminalTile`'s WebSocket survives and its existing `ResizeObserver` re-fits xterm/PTY automatically.

**Tech Stack:** React 18 + TypeScript, Vitest + Testing Library, plain CSS (`web/src/index.css`).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-18-maximize-tile-design.md`
- Only real session tiles maximize — not empty tiles, not the "server removed" placeholder.
- Maximized state is ephemeral (never persisted to `/api/layout`).
- Run tests from `web/`: `npm test -- --run src/__tests__/grid-page.test.tsx`. Full check: `./verify.sh` from repo root.

---

### Task 1: Double-click header toggles maximize

**Files:**
- Modify: `web/src/grid/GridPage.tsx`
- Modify: `web/src/index.css` (after the `.tile` rule, ~line 461; `user-select` on `.tile-header` ~line 470)
- Test: `web/src/__tests__/grid-page.test.tsx`

**Interfaces:**
- Consumes: existing `tileKey(t)` helper, `placed` memo in `GridPage.tsx`.
- Produces: `maximizedKey` state + `setMaximizedKey` setter (used by Tasks 2 and 3); `tile-maximized` CSS class on the maximized `.tile` element.

- [ ] **Step 1: Write the failing test**

Append to `web/src/__tests__/grid-page.test.tsx`:

```tsx
test("double-clicking a tile header maximizes the tile; double-clicking again restores", async () => {
  const layout = {
    shape: { rows: 1, cols: 2 },
    tiles: [
      { serverId: "local", sessionId: 1 },
      { serverId: "local", sessionId: 2 },
    ],
  };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const header = screen.getByText("#1 · claude").closest(".tile-header")!;
  const tile = header.closest(".tile")!;

  await userEvent.dblClick(header);
  expect(tile.className).toContain("tile-maximized");

  await userEvent.dblClick(header);
  expect(tile.className).not.toContain("tile-maximized");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `web/`): `npm test -- --run src/__tests__/grid-page.test.tsx`
Expected: new test FAILS — `tile.className` never contains `tile-maximized`; existing tests PASS.

- [ ] **Step 3: Implement state, handler, class, and CSS**

In `web/src/grid/GridPage.tsx`:

Add state next to the other `useState` calls at the top of `GridPage`:

```tsx
// Ephemeral: which tile fills the viewport (tile key), or null for grid view.
const [maximizedKey, setMaximizedKey] = useState<string | null>(null);
```

Change the tile wrapper div (currently `className="tile"`) to:

```tsx
<div
  key={i}
  className={`tile${tile && tileKey(tile) === maximizedKey ? " tile-maximized" : ""}`}
```

In the real-session branch (the JSX returned when `server` and the tile exist — NOT the "server removed" branch), add `onDoubleClick` to the `.tile-header` div:

```tsx
<div
  className="tile-header"
  onDoubleClick={() =>
    setMaximizedKey((k) => (k === tileKey(tile) ? null : tileKey(tile)))
  }
>
```

In `web/src/index.css`, after the `.tile` rule (~line 461):

```css
.tile-maximized {
  position: fixed;
  inset: 0;
  z-index: 50;
  background: var(--bg);
  border-radius: 0;
}
```

And add `user-select: none;` to the existing `.tile-header` rule (~line 470) so double-clicks don't select header text:

```css
.tile-header {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  padding: 0.25rem 0.5rem;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  font-family: var(--mono);
  font-size: 0.75rem;
  cursor: grab;
  user-select: none;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (from `web/`): `npm test -- --run src/__tests__/grid-page.test.tsx`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/grid/GridPage.tsx web/src/index.css web/src/__tests__/grid-page.test.tsx
git commit -m "feat: double-click tile header to maximize session"
```

---

### Task 2: Escape restores the grid

**Files:**
- Modify: `web/src/grid/GridPage.tsx`
- Test: `web/src/__tests__/grid-page.test.tsx`

**Interfaces:**
- Consumes: `maximizedKey` / `setMaximizedKey` from Task 1.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append to `web/src/__tests__/grid-page.test.tsx`:

```tsx
test("Escape restores the grid while a tile is maximized", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const header = screen.getByText("#1 · claude").closest(".tile-header")!;
  const tile = header.closest(".tile")!;

  await userEvent.dblClick(header);
  expect(tile.className).toContain("tile-maximized");

  await userEvent.keyboard("{Escape}");
  expect(tile.className).not.toContain("tile-maximized");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `web/`): `npm test -- --run src/__tests__/grid-page.test.tsx`
Expected: new test FAILS — class still present after Escape.

- [ ] **Step 3: Implement the Escape listener**

In `GridPage`, next to the other `useEffect` calls:

```tsx
// Listener only exists while maximized; Escape also reaches the focused
// terminal (same trade-off as cheep).
useEffect(() => {
  if (!maximizedKey) return;
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") setMaximizedKey(null);
  };
  window.addEventListener("keydown", onKey);
  return () => window.removeEventListener("keydown", onKey);
}, [maximizedKey]);
```

- [ ] **Step 4: Run test to verify it passes**

Run (from `web/`): `npm test -- --run src/__tests__/grid-page.test.tsx`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/grid/GridPage.tsx web/src/__tests__/grid-page.test.tsx
git commit -m "feat: Escape restores grid from maximized tile"
```

---

### Task 3: Clear maximized state when the tile leaves the grid

**Files:**
- Modify: `web/src/grid/GridPage.tsx`
- Test: `web/src/__tests__/grid-page.test.tsx`

**Interfaces:**
- Consumes: `maximizedKey` / `setMaximizedKey` from Task 1; existing `placed` memo (Set of tile keys currently in the layout).
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append to `web/src/__tests__/grid-page.test.tsx`. Re-adding the same session after removal proves the state was cleared, not merely hidden:

```tsx
test("removing the maximized tile clears maximized state", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  render(<GridPage />);
  await screen.findByTestId("term-1");

  const header = screen.getByText("#1 · claude").closest(".tile-header")!;
  await userEvent.dblClick(header);
  expect(document.querySelector(".tile-maximized")).not.toBeNull();

  await userEvent.click(screen.getByLabelText("remove session 1 from grid"));
  expect(document.querySelector(".tile-maximized")).toBeNull();

  // Re-add the same session: it must come back un-maximized.
  await userEvent.click(await screen.findByText("+ #1 claude"));
  await screen.findByTestId("term-1");
  expect(document.querySelector(".tile-maximized")).toBeNull();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `web/`): `npm test -- --run src/__tests__/grid-page.test.tsx`
Expected: new test FAILS on the final assertion — the re-added tile still carries `tile-maximized` because stale `maximizedKey` matches its key again.

- [ ] **Step 3: Implement the cleanup effect**

In `GridPage`, after the `placed` memo:

```tsx
// A maximized tile that leaves the layout (removed, terminated, server-side
// layout change) must not leave the page stuck fullscreen — or re-maximize
// if the same session is later re-added.
useEffect(() => {
  if (maximizedKey && !placed.has(maximizedKey)) setMaximizedKey(null);
}, [maximizedKey, placed]);
```

- [ ] **Step 4: Run test to verify it passes**

Run (from `web/`): `npm test -- --run src/__tests__/grid-page.test.tsx`
Expected: all PASS.

- [ ] **Step 5: Run full verification**

Run from repo root: `./verify.sh`
Expected: formatting, tests, and build all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/grid/GridPage.tsx web/src/__tests__/grid-page.test.tsx
git commit -m "fix: clear maximized state when tile leaves the grid"
```
