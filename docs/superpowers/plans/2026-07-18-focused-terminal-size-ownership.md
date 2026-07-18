# Focused Terminal Size Ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the visible, focused multimux page establish and reclaim the shared tmux window size so background tabs cannot leave dotted padding in the foreground terminal.

**Architecture:** Add an `active` bit to browser resize messages and determine it from document visibility plus window focus. Handle that bit atomically in the existing per-session resize arbiter: active resizes claim ownership, while inactive resizes continue sizing only their attachment PTY unless they are the current owner or no owner exists.

**Tech Stack:** React 19, TypeScript, xterm.js, Vitest, Go, gorilla/websocket, tmux

---

### Task 1: Encode focused-page ownership in terminal resize messages

**Files:**
- Modify: `web/src/term/protocol.ts`
- Modify: `web/src/term/TerminalTile.tsx`
- Test: `web/src/__tests__/protocol.test.ts`

- [ ] **Step 1: Write the failing protocol test**

Replace the existing resize test with explicit active and inactive cases:

```ts
test("encodeResize marks an active page", () => {
  expect(JSON.parse(encodeResize(120, 40, true))).toEqual({
    type: "resize",
    cols: 120,
    rows: 40,
    active: true,
  });
});

test("encodeResize marks an inactive page", () => {
  expect(JSON.parse(encodeResize(80, 24, false))).toEqual({
    type: "resize",
    cols: 80,
    rows: 24,
    active: false,
  });
});
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd web && pnpm test src/__tests__/protocol.test.ts
```

Expected: FAIL because `encodeResize` ignores its third argument and the parsed messages have no `active` field.

- [ ] **Step 3: Add the active field to the encoder**

Change the encoder to:

```ts
export function encodeResize(cols: number, rows: number, active: boolean): string {
  return JSON.stringify({ type: "resize", cols, rows, active });
}
```

- [ ] **Step 4: Send ownership state from the terminal**

In `TerminalTile.tsx`, make `sendResize` derive ownership at send time:

```ts
function sendResize() {
  if (ws?.readyState === WebSocket.OPEN) {
    const active = document.visibilityState === "visible" && document.hasFocus();
    ws.send(encodeResize(term.cols, term.rows, active));
  }
}
```

After installing the `ResizeObserver`, install page-activity listeners:

```ts
window.addEventListener("focus", sendResize);
document.addEventListener("visibilitychange", sendResize);
```

Remove both listeners during effect cleanup:

```ts
window.removeEventListener("focus", sendResize);
document.removeEventListener("visibilitychange", sendResize);
```

WebSocket open and ResizeObserver callbacks already call `sendResize`, so they automatically carry the current page activity.

- [ ] **Step 5: Run frontend tests and type-check**

Run:

```bash
cd web && pnpm test src/__tests__/protocol.test.ts && pnpm build
```

Expected: all protocol tests PASS and the TypeScript/Vite build completes.

- [ ] **Step 6: Commit the browser protocol change**

```bash
git add web/src/term/protocol.ts web/src/term/TerminalTile.tsx web/src/__tests__/protocol.test.ts
git commit -m "feat(web): mark focused terminal resizes active"
```

### Task 2: Atomically transfer resize ownership on the server

**Files:**
- Modify: `internal/tmuxmgr/arbiter.go`
- Modify: `internal/tmuxmgr/arbiter_test.go`
- Modify: `internal/server/ws.go`

- [ ] **Step 1: Write the failing arbiter regression test**

Update existing `Resize` calls in `arbiter_test.go` to pass `false` as the third argument. Then add:

```go
func TestActiveResizeClaimsAndTransfersOwnership(t *testing.T) {
	a := NewArbiter()
	foreground := a.Register("mm-1")
	background := a.Register("mm-1")
	defer foreground.Unregister()
	defer background.Unregister()

	if !foreground.Resize(129, 76, true) {
		t.Fatal("active foreground resize must be allowed")
	}
	if background.Resize(97, 76, false) {
		t.Fatal("inactive background resize must not override the active owner")
	}
	if !background.Resize(97, 76, true) {
		t.Fatal("newly active connection must take ownership")
	}
	if foreground.Resize(129, 76, false) {
		t.Fatal("previous owner must not override the newly active connection")
	}
}
```

- [ ] **Step 2: Run the arbiter test and verify RED**

Run:

```bash
go test ./internal/tmuxmgr -run 'TestActiveResizeClaimsAndTransfersOwnership|TestOwnershipFollowsInput' -count=1
```

Expected: FAIL to compile because `ArbConn.Resize` does not yet accept the active/claim argument.

- [ ] **Step 3: Implement atomic active-resize ownership**

Change `ArbConn.Resize` to:

```go
// Resize records the dims this conn wants and reports whether it may resize
// the shared tmux window. An active resize atomically claims ownership.
func (c *ArbConn) Resize(cols, rows uint16, active bool) bool {
	c.arb.mu.Lock()
	defer c.arb.mu.Unlock()
	c.cols, c.rows = cols, rows
	s := c.arb.sessions[c.tmuxName]
	if s == nil {
		return true
	}
	if active {
		s.owner = c
		return true
	}
	return s.owner == nil || s.owner == c
}
```

Update every existing Go call to `Resize` with `false` except the new active-resize assertions.

- [ ] **Step 4: Parse and forward the active flag**

Extend the WebSocket message type:

```go
type resizeMsg struct {
	Type   string `json:"type"`
	Cols   uint16 `json:"cols"`
	Rows   uint16 `json:"rows"`
	Active bool   `json:"active"`
}
```

Pass the flag into arbitration:

```go
allowed := arb.Resize(msg.Cols, msg.Rows, msg.Active)
```

Old clients remain compatible because a missing JSON boolean decodes as `false`.

- [ ] **Step 5: Run focused and complete Go tests**

Run:

```bash
go test ./internal/tmuxmgr ./internal/server -count=1
```

Expected: all tmux manager and server tests PASS.

- [ ] **Step 6: Commit the server ownership change**

```bash
git add internal/tmuxmgr/arbiter.go internal/tmuxmgr/arbiter_test.go internal/server/ws.go
git commit -m "fix: let active terminal resize claim ownership"
```

### Task 3: Verify the complete feature

**Files:**
- Verify: all repository packages and frontend sources

- [ ] **Step 1: Format changed sources**

Run:

```bash
gofmt -w internal/tmuxmgr/arbiter.go internal/tmuxmgr/arbiter_test.go internal/server/ws.go
cd web && pnpm exec prettier --write src/term/protocol.ts src/term/TerminalTile.tsx src/__tests__/protocol.test.ts
```

Expected: formatting completes without errors.

- [ ] **Step 2: Run end-to-end repository verification**

Run:

```bash
./verify.sh
```

Expected: gofmt, go vet, all Go tests, frontend lint, Vitest, and the production build pass, ending with `verify OK`.

- [ ] **Step 3: Inspect the final diff**

Run:

```bash
git status --short
git diff --check HEAD~2
git diff HEAD~2 -- web/src/term internal/tmuxmgr/arbiter.go internal/tmuxmgr/arbiter_test.go internal/server/ws.go
```

Expected: only the planned focused-size ownership changes are present and `git diff --check` reports no whitespace errors.
