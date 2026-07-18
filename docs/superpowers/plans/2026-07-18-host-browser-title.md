# Host Browser Title Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Include the configured host label in the browser title and keep it synchronized with Appearance setting changes.

**Architecture:** `App` already owns the health state that supplies the header host label and receives live appearance updates. A single React effect will derive `document.title` from `health.hostLabel`, preserving the static HTML title as the pre-render fallback.

**Tech Stack:** React 19, TypeScript, Vitest, Testing Library

---

## File Structure

- Modify `web/src/App.tsx` to derive the document title from the existing health state.
- Modify `web/src/__tests__/appearance.test.tsx` to cover the fetched label, live appearance updates, and missing-label fallback.

### Task 1: Synchronize the Browser Title

**Files:**

- Modify: `web/src/__tests__/appearance.test.tsx:17-42`
- Modify: `web/src/App.tsx:25-47`

- [ ] **Step 1: Write the failing title tests**

Extend the existing health appearance test and add a fallback test:

```tsx
test("app appearance follows healthz and live updates", async () => {
  const fetchMock = mockFetchByURL({
    "/healthz": () =>
      new Response(
        JSON.stringify({
          status: "ok",
          setupPending: false,
          version: "test",
          hostLabel: "work-mac",
          accentColor: "#3fb950",
        }),
      ),
    "/api/auth/me": () => new Response("{}", { status: 200 }),
    "/api/sessions": () => new Response("[]"),
    "/api/layout": () => new Response("{}"),
    "/api/tools": () => new Response("[]"),
    "/api/dirs": () => new Response("[]"),
  });

  const { container } = render(<App />);
  await screen.findByText("@work-mac");
  const header = container.querySelector("header.host-accented") as HTMLElement;
  expect(header).not.toBeNull();
  expect(header.style.getPropertyValue("--host-accent")).toBe("#3fb950");
  expect(document.title).toBe("multimux @work-mac");

  window.dispatchEvent(
    new CustomEvent(APPEARANCE_EVENT, {
      detail: { hostLabel: "home-server", accentColor: "#ff0000" },
    }),
  );

  await waitFor(() => expect(document.title).toBe("multimux @home-server"));
  fetchMock.mockRestore();
});

test("browser title falls back to multimux without a host label", async () => {
  document.title = "stale title";
  const fetchMock = mockFetchByURL({
    "/healthz": () =>
      new Response(
        JSON.stringify({
          status: "ok",
          setupPending: false,
          version: "test",
        }),
      ),
    "/api/auth/me": () => new Response("{}", { status: 200 }),
    "/api/sessions": () => new Response("[]"),
    "/api/layout": () => new Response("{}"),
    "/api/tools": () => new Response("[]"),
    "/api/dirs": () => new Response("[]"),
  });

  render(<App />);

  await waitFor(() => expect(document.title).toBe("multimux"));
  fetchMock.mockRestore();
});
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run:

```bash
cd web && pnpm test src/__tests__/appearance.test.tsx
```

Expected: FAIL because `document.title` remains `multimux` when `work-mac` is loaded and does not react to the health state.

- [ ] **Step 3: Add the minimal title synchronization effect**

Add this effect in `App`, alongside the existing route and appearance effects:

```tsx
useEffect(() => {
  document.title = health?.hostLabel ? `multimux @${health.hostLabel}` : "multimux";
}, [health?.hostLabel]);
```

- [ ] **Step 4: Run the focused test to verify it passes**

Run:

```bash
cd web && pnpm test src/__tests__/appearance.test.tsx
```

Expected: PASS with all appearance tests green and no warnings.

- [ ] **Step 5: Run full repository verification**

Run:

```bash
./verify.sh
```

Expected: formatting, lint, frontend tests, Go tests, and builds all pass without warnings or errors.

- [ ] **Step 6: Commit the implementation**

```bash
git add web/src/App.tsx web/src/__tests__/appearance.test.tsx
git commit -m "feat: include host label in browser title"
```
