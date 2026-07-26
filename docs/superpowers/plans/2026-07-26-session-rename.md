# Session Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user give any session a human-readable label, shown in the tile header instead of the tool name, editable by double-clicking the tile title.

**Architecture:** A new `label` column on the `sessions` table, written through `PUT /api/sessions/{id}/label`, broadcast as `session_renamed` so every open tab refreshes. The tmux session name stays `mm-{id}` — nothing in attach, reconcile, or orphan-replace changes.

**Tech Stack:** Go 1.x + `modernc.org/sqlite`, `net/http` `ServeMux` pattern routing, React 19 + TypeScript, Vitest + Testing Library.

## Global Constraints

- `store.migrations` is append-only. Add a new entry at the end; never edit a shipped one.
- Labels are capped at **64 runes** after trimming; control characters are rejected; an empty label clears the label.
- The daemon must never log the label text itself (same privacy rule as directory paths — logs record only whether a label is set).
- Fix every compiler, vet, lint, and test warning as it appears. `./verify.sh` must pass before the work is called done.
- Source spec: `docs/superpowers/specs/2026-07-26-session-rename-design.md`.

---

### Task 1: Store — label column, field, and setter

**Files:**
- Modify: `internal/store/store.go:21-67` (append a migration)
- Modify: `internal/store/sessions.go:13-21` (Session struct), `:54-68` (scanSession, sessionCols), end of file (new setter)
- Test: `internal/store/sessions_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `store.Session.Label string` with JSON tag `label,omitempty`
  - `func (s *Store) SetSessionLabel(id int64, label string) error` — returns `store.ErrNotFound` when no row has that id.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/sessions_test.go`:

```go
func TestSessionLabel(t *testing.T) {
	s := openTestStore(t)
	tool, _ := s.CreateTool("zsh", "zsh")
	sess, err := s.CreateSession(tool.ID, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Label != "" {
		t.Fatalf("new session label = %q, want empty", sess.Label)
	}

	if err := s.SetSessionLabel(sess.ID, "api refactor"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(sess.ID)
	if err != nil || got.Label != "api refactor" {
		t.Fatalf("GetSession = %+v, %v", got, err)
	}
	list, _ := s.ListSessions()
	if len(list) != 1 || list[0].Label != "api refactor" {
		t.Fatalf("list = %+v", list)
	}

	// Empty clears it.
	if err := s.SetSessionLabel(sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSession(sess.ID); got.Label != "" {
		t.Fatalf("label after clear = %q", got.Label)
	}

	if err := s.SetSessionLabel(sess.ID+999, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetSessionLabel unknown id = %v, want ErrNotFound", err)
	}
}

func TestSessionLabelSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := s.CreateTool("zsh", "zsh")
	sess, _ := s.CreateSession(tool.ID, "/tmp")
	if err := s.SetSessionLabel(sess.ID, "kept"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Re-running migrations on an existing file must not re-add the column or
	// lose the value.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { s2.Close() })
	if got, _ := s2.GetSession(sess.ID); got.Label != "kept" {
		t.Fatalf("label after reopen = %q, want kept", got.Label)
	}
}
```

`TestSessionLabelSurvivesReopen` needs `path/filepath` in the import block; `errors` is already imported.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run TestSessionLabel -v`
Expected: FAIL to compile — `sess.Label undefined` and `s.SetSessionLabel undefined`.

- [ ] **Step 3: Append the migration**

In `internal/store/store.go`, add as the **last** element of `migrations` (after the tools/dirs `position` entry):

```go
	// Optional user-chosen display label for a session. Empty means "no
	// label" — the UI falls back to the tool name. The tmux session name is
	// unaffected; it stays mm-{id}.
	`ALTER TABLE sessions ADD COLUMN label TEXT NOT NULL DEFAULT '';`,
```

- [ ] **Step 4: Add the field, column list, and scan**

In `internal/store/sessions.go`, add the field to `Session`:

```go
// Session is one managed tmux session.
type Session struct {
	ID        int64     `json:"id"`
	TmuxName  string    `json:"tmuxName"`
	ToolID    int64     `json:"toolId"`
	Dir       string    `json:"dir"`
	Status    string    `json:"status"` // "running" | "dead"
	CreatedAt time.Time `json:"createdAt"`
	// Label is the user's display name for this session, or "" for none.
	Label string `json:"label,omitempty"`
}
```

Extend the column list and the scan (label last, matching the struct):

```go
const sessionCols = `id, tmux_name, tool_id, dir, status, created_at, label`
```

```go
func scanSession(scan func(...any) error) (Session, error) {
	var sess Session
	var created string
	if err := scan(&sess.ID, &sess.TmuxName, &sess.ToolID, &sess.Dir, &sess.Status, &created, &sess.Label); err != nil {
		return Session{}, err
	}
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return Session{}, err
	}
	sess.CreatedAt = t
	return sess, nil
}
```

`CreateSession` needs no change — a new row's label is `''` and the returned struct's zero value matches.

- [ ] **Step 5: Add the setter**

Append to `internal/store/sessions.go`, next to `SetSessionStatus`:

```go
// SetSessionLabel sets a session's display label; "" clears it. The label is
// cosmetic — tmux_name is untouched, so nothing that keys off it changes.
func (s *Store) SetSessionLabel(id int64, label string) error {
	res, err := s.db.Exec(`UPDATE sessions SET label = ? WHERE id = ?`, label, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 6: Run the store tests**

Run: `go test ./internal/store/ -v`
Expected: PASS, including the pre-existing `TestSessionLifecycle`.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/sessions.go internal/store/sessions_test.go
git commit -m "feat(store): add session label column and setter"
```

---

### Task 2: API — `PUT /api/sessions/{id}/label`

**Files:**
- Modify: `internal/server/sessions.go` (new handler after `handleDismissSession`, around `:235`)
- Modify: `internal/server/server.go:89` (register the route)
- Test: `internal/server/sessions_test.go`

**Interfaces:**
- Consumes: `store.Session.Label`, `(*store.Store).SetSessionLabel(id int64, label string) error` from Task 1.
- Produces:
  - Route `PUT /api/sessions/{id}/label`, body `{"label":"..."}`, 200 with the updated `store.Session` JSON.
  - Broadcast event type `session_renamed` carrying that session.
  - `const maxSessionLabel = 64` in package `server`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/server/sessions_test.go`:

```go
func TestSessionRename(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	dir, _ := st.CreateDir("tmp", t.TempDir())
	buf := captureLogs(t)

	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	if w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	var sess store.Session
	json.Unmarshal(w.Body.Bytes(), &sess)
	path := fmt.Sprintf("/api/sessions/%d/label", sess.ID)

	// Rename → 200 with the updated session, and the list agrees.
	w = do(t, s, "PUT", path, token, `{"label":"  api refactor  "}`)
	if w.Code != 200 {
		t.Fatalf("rename = %d: %s", w.Code, w.Body.String())
	}
	var got store.Session
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Label != "api refactor" {
		t.Fatalf("label = %q, want trimmed %q", got.Label, "api refactor")
	}
	w = do(t, s, "GET", "/api/sessions", token)
	if !strings.Contains(w.Body.String(), `"label":"api refactor"`) {
		t.Fatalf("list missing label: %s", w.Body.String())
	}

	// Empty clears.
	if w = do(t, s, "PUT", path, token, `{"label":""}`); w.Code != 200 {
		t.Fatalf("clear = %d: %s", w.Code, w.Body.String())
	}
	if stored, _ := st.GetSession(sess.ID); stored.Label != "" {
		t.Fatalf("label after clear = %q", stored.Label)
	}

	// The label is user text: it must not reach the logs.
	if w = do(t, s, "PUT", path, token, `{"label":"secret-label-must-not-leak"}`); w.Code != 200 {
		t.Fatalf("rename = %d", w.Code)
	}
	logged := buf.String()
	if !strings.Contains(logged, `"msg":"session renamed"`) {
		t.Fatalf("rename not logged: %s", logged)
	}
	if strings.Contains(logged, "secret-label-must-not-leak") {
		t.Fatalf("rename log exposed the label: %s", logged)
	}
}

func TestSessionRenameValidation(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	dir, _ := st.CreateDir("tmp", t.TempDir())
	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	var sess store.Session
	json.Unmarshal(w.Body.Bytes(), &sess)
	path := fmt.Sprintf("/api/sessions/%d/label", sess.ID)

	// 64 runes of a multi-byte character: the cap counts runes, not bytes.
	ok := strings.Repeat("é", 64)
	if w = do(t, s, "PUT", path, token, fmt.Sprintf(`{"label":%q}`, ok)); w.Code != 200 {
		t.Fatalf("64 runes = %d: %s", w.Code, w.Body.String())
	}
	tooLong := strings.Repeat("é", 65)
	if w = do(t, s, "PUT", path, token, fmt.Sprintf(`{"label":%q}`, tooLong)); w.Code != 400 {
		t.Fatalf("65 runes = %d, want 400", w.Code)
	}
	// \u0007 is BEL: valid JSON, invalid label.
	if w = do(t, s, "PUT", path, token, `{"label":"bad\u0007bell"}`); w.Code != 400 {
		t.Fatalf("control char = %d, want 400", w.Code)
	}
	if w = do(t, s, "PUT", path, token, `not json`); w.Code != 400 {
		t.Fatalf("bad body = %d, want 400", w.Code)
	}

	// Unknown id → 404, and the earlier valid label is untouched.
	if w = do(t, s, "PUT", "/api/sessions/9999/label", token, `{"label":"x"}`); w.Code != 404 {
		t.Fatalf("unknown id = %d, want 404", w.Code)
	}
	if stored, _ := st.GetSession(sess.ID); stored.Label != ok {
		t.Fatalf("label = %q, want the 64-rune value", stored.Label)
	}

	// A dead session can still be renamed — its tile stays on screen.
	if w = do(t, s, "DELETE", fmt.Sprintf("/api/sessions/%d", sess.ID), token); w.Code != 204 {
		t.Fatalf("kill = %d", w.Code)
	}
	if w = do(t, s, "PUT", path, token, `{"label":"post mortem"}`); w.Code != 200 {
		t.Fatalf("rename dead = %d: %s", w.Code, w.Body.String())
	}
}

func TestSessionRenameBroadcasts(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	dir, _ := st.CreateDir("tmp", t.TempDir())
	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	var sess store.Session
	json.Unmarshal(w.Body.Bytes(), &sess)

	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)
	if w = do(t, s, "PUT", fmt.Sprintf("/api/sessions/%d/label", sess.ID), token, `{"label":"watched"}`); w.Code != 200 {
		t.Fatalf("rename = %d", w.Code)
	}
	select {
	case raw := <-ch:
		var ev struct {
			Type    string        `json:"type"`
			Payload store.Session `json:"payload"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("event %s: %v", raw, err)
		}
		if ev.Type != "session_renamed" || ev.Payload.Label != "watched" {
			t.Fatalf("event = %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("no session_renamed event")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/server/ -run TestSessionRename -v`
Expected: FAIL — the PUT returns 404 (no such route) before the handler exists.

- [ ] **Step 3: Write the handler**

In `internal/server/sessions.go`, add after `handleDismissSession`:

```go
// maxSessionLabel caps a session's display label. Tile headers are narrow;
// past this the label crowds out the directory and branch.
const maxSessionLabel = 64

// handleRenameSession sets a session's display label ("" clears it). The label
// is cosmetic: tmux_name stays mm-{id}, so attach, Reconcile, and the
// orphan-replace path in handleCreateSession are all unaffected. Dead sessions
// are renameable too — their tiles stay on screen until dismissed.
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	var in struct{ Label string }
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	label := strings.TrimSpace(in.Label)
	for _, c := range label {
		if unicode.IsControl(c) {
			writeJSON(w, 400, map[string]string{"error": "label must not contain control characters"})
			return
		}
	}
	if utf8.RuneCountInString(label) > maxSessionLabel {
		writeJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("label must be %d characters or fewer", maxSessionLabel),
		})
		return
	}
	err = s.cfg.Store.SetSessionLabel(id, label)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	sess, err := s.cfg.Store.GetSession(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// The label is user text, like a directory path: log that it changed, not
	// what it says.
	slog.Info("session renamed", "session_id", sess.ID, "labelled", label != "")
	s.broadcast("session_renamed", sess)
	writeJSON(w, 200, sess)
}
```

Add `"fmt"`, `"unicode"`, and `"unicode/utf8"` to the import block in `internal/server/sessions.go`; `errors`, `log/slog`, `net/http`, `strings`, and the `store` import are already there.

- [ ] **Step 4: Register the route**

In `internal/server/server.go`, directly below the dismiss route (`:89`):

```go
	s.mux.HandleFunc("PUT /api/sessions/{id}/label", s.handleRenameSession)
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/server/ -run TestSession -v`
Expected: PASS, including the pre-existing `TestSessionCreateKillDismiss`.

- [ ] **Step 6: Commit**

```bash
git add internal/server/sessions.go internal/server/server.go internal/server/sessions_test.go
git commit -m "feat(api): add PUT /api/sessions/{id}/label"
```

---

### Task 3: Frontend — inline rename on the tile title

**Files:**
- Modify: `web/src/grid/types.ts:1-13` (Session type)
- Modify: `web/src/grid/GridPage.tsx:85-89` (`toolName` → `sessionTitle`), `:257`, `:327`, `:384-392`, `:519-523` (attach dropdown), plus a new `TileTitle` component and an `editingKey` state
- Modify: `web/src/index.css:523-526` (input styling)
- Test: `web/src/__tests__/grid-page.test.tsx`

**Interfaces:**
- Consumes: `PUT /api/sessions/{id}/label` and the `label` field on the session JSON from Task 2.
- Produces: no exports beyond the page — `TileTitle` and `sessionTitle` stay module-private in `GridPage.tsx`.

- [ ] **Step 1: Write the failing tests**

In `web/src/__tests__/grid-page.test.tsx`, give one of the existing fixtures a label and add the tests. Change the session with `id: 2` to carry a label:

```ts
  { id: 2, tmuxName: "mm-2", toolId: 1, dir: "/b", status: "running", label: "api refactor" },
```

Then extend `mockFetch` so label writes resolve — add this **above** the existing `if (url.includes("/api/sessions") && method === "POST")` line:

```ts
    if (url.includes("/label") && method === "PUT")
      return new Response(JSON.stringify({ id: 1, tmuxName: "mm-1", toolId: 1, dir: "/a", status: "running" }));
```

Add these tests at the end of the file:

```tsx
test("a labelled session shows its label in the tile title and attach dropdown", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 2 }, null] };
  mockFetch(layout);

  render(<GridPage />);

  expect(await screen.findByText("#2 · api refactor")).toBeTruthy();
  const attach = screen.getAllByRole("combobox").find((b) => b.textContent?.includes("attach session on local"))!;
  const options = Array.from(attach.querySelectorAll("option")).map((o) => o.textContent);
  expect(options).toContain("api refactor");
  expect(options).not.toContain("mm-2");
});

test("double-clicking the tile title renames the session", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  const title = await screen.findByText("#1 · claude");

  await userEvent.dblClick(title);
  const input = await screen.findByLabelText<HTMLInputElement>("rename session 1");
  expect(input.value).toBe("");

  await userEvent.type(input, "api refactor");
  fireEvent.keyDown(input, { key: "Enter" });

  await waitFor(() => {
    const put = fetchMock.mock.calls.find(
      ([url, init]) => String(url).includes("/api/sessions/1/label") && init?.method === "PUT",
    );
    expect(put).toBeTruthy();
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ label: "api refactor" });
  });
});

test("Escape cancels a rename without writing", async () => {
  const layout = { shape: { rows: 1, cols: 1 }, tiles: [{ serverId: "local", sessionId: 1 }] };
  const fetchMock = mockFetch(layout);

  render(<GridPage />);
  const title = await screen.findByText("#1 · claude");

  await userEvent.dblClick(title);
  const input = await screen.findByLabelText<HTMLInputElement>("rename session 1");
  await userEvent.type(input, "discard me");
  fireEvent.keyDown(input, { key: "Escape" });

  expect(await screen.findByText("#1 · claude")).toBeTruthy();
  expect(
    fetchMock.mock.calls.some(([url, init]) => String(url).includes("/label") && init?.method === "PUT"),
  ).toBe(false);
});

test("double-clicking the tile title does not maximize the tile", async () => {
  const layout = { shape: { rows: 1, cols: 2 }, tiles: [{ serverId: "local", sessionId: 1 }, null] };
  mockFetch(layout);

  const { container } = render(<GridPage />);
  const title = await screen.findByText("#1 · claude");

  await userEvent.dblClick(title);
  expect(container.querySelector(".tile-maximized")).toBeNull();
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && pnpm test src/__tests__/grid-page.test.tsx`
Expected: FAIL — no element labelled "rename session 1", and the label tests can't find `#2 · api refactor`.

- [ ] **Step 3: Add the type field**

In `web/src/grid/types.ts`:

```ts
export type Session = {
  id: number;
  tmuxName: string;
  toolId: number;
  dir: string;
  status: string;
  /** User-chosen display name; absent or empty means "use the tool name". */
  label?: string;
  repoUrl?: string;
  branch?: string;
  gitState?: "untracked" | "modified" | "clean";
  ahead?: number;
  behind?: number;
  noUpstream?: boolean;
};
```

- [ ] **Step 4: Replace `toolName` with `sessionTitle`**

In `web/src/grid/GridPage.tsx`, replace the function at `:85-89`:

```tsx
// Display name for a session: the user's label when set, else the tool name,
// falling back to the tmux session name while tools load.
function sessionTitle(tools: Tool[] | undefined, session: Session | undefined): string {
  if (!session) return "…";
  if (session.label) return session.label;
  return tools?.find((t) => t.id === session.toolId)?.name ?? session.tmuxName;
}
```

Update both call sites — line 257 becomes:

```tsx
              + #{sess.id} {sessionTitle(toolsByServer[server.id], sess)}
```

(the tile-header call site is replaced wholesale in Step 6).

- [ ] **Step 5: Add the `TileTitle` component**

Add near `sessionTitle` in `web/src/grid/GridPage.tsx`:

```tsx
// The tile title, double-click-to-rename. The tile header's own double-click
// maximizes the tile, so the handlers here stop propagation; the input also
// tells the page to drop `draggable` on the tile, or the browser's drag
// intercepts text selection inside it.
function TileTitle({
  sessionId,
  text,
  label,
  onEditingChange,
  onRename,
}: {
  sessionId: number;
  text: string;
  label: string;
  onEditingChange: (editing: boolean) => void;
  onRename: (label: string) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);

  const stop = (draftValue: string | null) => {
    setDraft(null);
    onEditingChange(false);
    if (draftValue !== null && draftValue.trim() !== label) onRename(draftValue.trim());
  };

  if (draft === null) {
    return (
      <span
        className="tile-title"
        title="double-click to rename"
        onDoubleClick={(e) => {
          e.stopPropagation();
          setDraft(label);
          onEditingChange(true);
        }}
      >
        #{sessionId} · {text}
      </span>
    );
  }
  return (
    <input
      className="tile-title tile-title-input"
      aria-label={`rename session ${sessionId}`}
      autoFocus
      maxLength={64}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onDoubleClick={(e) => e.stopPropagation()}
      onBlur={() => stop(draft)}
      onKeyDown={(e) => {
        if (e.key === "Enter") stop(draft);
        else if (e.key === "Escape") stop(null);
      }}
    />
  );
}
```

- [ ] **Step 6: Wire it into the tile header**

In `GridPage`, add the state next to `moveFrom` (`:111`):

```tsx
  // Tile key whose title is being renamed; the tile drops `draggable` while it
  // is, so the drag doesn't eat text selection in the input.
  const [editingKey, setEditingKey] = useState<string | null>(null);
```

Add the writer next to `refreshSessions` (`:157-163`):

```tsx
  const renameSession = useCallback(
    (server: Server, sessionId: number, label: string) => {
      // The response and the session_renamed broadcast both land as a refresh;
      // a failure just leaves the old title in place.
      putJSON(server, `/api/sessions/${sessionId}/label`, { label }).then(refreshSessions, refreshSessions);
    },
    [refreshSessions],
  );
```

Make the tile non-draggable while editing (`:327`):

```tsx
            draggable={tile !== null && tileKey(tile) !== editingKey}
```

Replace the `<span className="tile-title">` block at `:390-392`:

```tsx
                      <TileTitle
                        sessionId={tile.sessionId}
                        text={sessionTitle(toolsByServer[tile.serverId], session)}
                        label={session?.label ?? ""}
                        onEditingChange={(editing) => setEditingKey(editing ? tileKey(tile) : null)}
                        onRename={(label) => renameSession(server, tile.sessionId, label)}
                      />
```

`server` is already in scope here (`:359`) and is non-null on this branch.

- [ ] **Step 7: Show labels in the attach dropdown**

In `EmptyTile` (`:519-523`):

```tsx
          {sessions.map((sess) => (
            <option key={sess.id} value={sess.id}>
              {sess.label || sess.tmuxName}
            </option>
          ))}
```

- [ ] **Step 8: Style the input**

In `web/src/index.css`, after the `.tile-title` rule (`:523-526`):

```css
.tile-title-input {
  min-width: 8rem;
  max-width: 16rem;
  padding: 0 2px;
  font: inherit;
  color: inherit;
  background: var(--bg);
  border: 1px solid var(--muted);
  border-radius: 3px;
}
```

`--bg`, `--muted`, and `--ink` are all defined in the `:root` block at the top of the file.

- [ ] **Step 9: Run the web tests and lint**

Run: `cd web && pnpm test src/__tests__/grid-page.test.tsx && pnpm lint`
Expected: PASS, including the pre-existing attach-dropdown test (it asserts on `mm-2`, which now renders as `api refactor` — the Step 1 edit to that fixture means the old assertion must be updated to the label; do that if it fails).

- [ ] **Step 10: Commit**

```bash
git add web/src/grid/types.ts web/src/grid/GridPage.tsx web/src/index.css web/src/__tests__/grid-page.test.tsx
git commit -m "feat(web): rename a session by double-clicking its tile title"
```

---

### Task 4: Document and verify end to end

**Files:**
- Modify: `README.md` (the section describing the session grid / tiles)
- Test: `./verify.sh`

**Interfaces:**
- Consumes: everything from Tasks 1-3.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Document the feature**

Find the README section that describes the tile header (search for "tile" or "grid"). Add one sentence in its voice, e.g.:

```markdown
Double-click a tile's title to rename the session. The name is a display label
only — the tmux session keeps its `mm-{id}` name — and clearing it restores the
tool name.
```

- [ ] **Step 2: Run the full verification**

Run: `./verify.sh`
Expected: PASS — gofmt, `go vet`, `go test ./...`, `pnpm lint`, `pnpm test`, `pnpm build`, `go build`, `scripts/smoke.sh`.

Fix anything it reports before continuing. Do not skip the frontend build: `main.go` embeds `web/dist`.

- [ ] **Step 3: Drive it in a real daemon**

```bash
export MULTIMUX_DATA_DIR="/tmp/multimux-rename-$(date +%s)"
go run . serve --dev --port 8787
```

Register a throwaway passkey at the printed `/setup?code=…` URL, launch a session, double-click its title, type a name, press Enter. Confirm: the title updates, a second browser tab showing the same session updates without a reload, and clearing the name restores the tool name.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document session rename"
```

---

## Notes for the implementer

- The tile header already has an `onDoubleClick` that maximizes the tile. Rename deliberately steals the double-click **on the title text only**; double-clicking anywhere else in the header still maximizes. The Task 3 test `"double-clicking the tile title does not maximize the tile"` pins this.
- Rename has no touch path (double-click only). This is a known, accepted limitation — do not add a button for it without asking.
- Do not touch `tmux_name`, `Reconcile`, or the orphan-replace branch in `handleCreateSession`. If a change seems to require it, the design is being misread.
