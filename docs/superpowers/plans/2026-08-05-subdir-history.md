# Subdir Focus Overlay and Per-Directory History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the launcher's subdir field readable while typing without taking more header space, and let each configured directory remember the subdirs that were actually launched into.

**Architecture:** The daemon gains a `dir_subdirs` table (cascading off `dirs`), three store functions, and two routes under `/api/dirs/{id}/subdirs`. An entry is written only after a launch has genuinely succeeded. The React launcher keeps the field 4rem wide at rest and absolutely positions it over the header while focused, with a filtered history dropdown beneath it.

**Tech Stack:** Go 1.x + `modernc.org/sqlite` (pure Go, no cgo), `net/http` `ServeMux` pattern routing, React 19 + TypeScript + Vite, Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-08-05-subdir-history-design.md`

## Global Constraints

- `store.migrations` is **append only**. Add a new entry at the end of the slice; never edit a shipped one.
- All store timestamps are RFC3339 UTC text.
- New routes go in `internal/server/server.go:routes` and sit behind the normal auth middleware. Nothing is added to the auth-bypass list.
- Run `cd web && pnpm build` before `go build` — `main.go` embeds `web/dist`.
- `./verify.sh` runs everything CI runs (gofmt, `go vet`, `go test ./...`, `pnpm lint`, `pnpm test`, `pnpm build`, `go build`, `scripts/smoke.sh`). Tests that touch tmux need tmux on PATH.
- Fix every compiler, lint, and test warning as it appears. Do not defer them.
- Go tests live beside their package; web tests live in `web/src/__tests__/`.
- Commit after each task.

---

### Task 1: Store — `dir_subdirs` table and its three functions

**Files:**
- Modify: `internal/store/store.go:21-71` (append one migration entry)
- Modify: `internal/store/tools.go` (append the new functions at the end of the file)
- Test: `internal/store/tools_test.go` (append)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `func (s *Store) ListSubdirs(dirID int64) ([]string, error)` — newest first, at most 10, never nil
  - `func (s *Store) RecordSubdir(dirID int64, subdir string) error` — upsert + trim; a blank subdir is a no-op returning nil
  - `func (s *Store) DeleteSubdir(dirID int64, subdir string) error` — exact match; deleting an absent row is not an error

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/tools_test.go`:

```go
func TestSubdirHistoryRecordsMostRecentFirst(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")

	for _, sub := range []string{"web", "cmd", "internal/server"} {
		if err := s.RecordSubdir(d.ID, sub); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListSubdirs(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "internal/server,cmd,web" {
		t.Fatalf("history = %v, want newest first", got)
	}

	// Re-using an entry bumps it to the front instead of duplicating it.
	if err := s.RecordSubdir(d.ID, "web"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.ListSubdirs(d.ID); strings.Join(got, ",") != "web,internal/server,cmd" {
		t.Fatalf("history after re-use = %v", got)
	}
}

func TestSubdirHistoryIgnoresBlank(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	if err := s.RecordSubdir(d.ID, "   "); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListSubdirs(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("history = %v, want empty", got)
	}
	// A directory with no history must still list as an empty slice, not nil:
	// the handler writes it straight to JSON, and nil marshals as null.
	if got == nil {
		t.Fatal("ListSubdirs returned nil, want empty slice")
	}
}

func TestSubdirHistoryIsCappedAtTen(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	for i := 0; i < 11; i++ {
		if err := s.RecordSubdir(d.ID, fmt.Sprintf("dir%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.ListSubdirs(d.ID)
	if len(got) != 10 {
		t.Fatalf("history length = %d, want 10", len(got))
	}
	if got[0] != "dir10" || got[9] != "dir01" {
		t.Fatalf("history = %v, want dir10..dir01 (dir00 evicted)", got)
	}
}

func TestSubdirHistoryIsPerDirectory(t *testing.T) {
	s := openTestStore(t)
	a, _ := s.CreateDir("repos", "/repos")
	b, _ := s.CreateDir("home", "/home")
	if err := s.RecordSubdir(a.ID, "web/src"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSubdirs(b.ID); len(got) != 0 {
		t.Fatalf("other directory's history = %v, want empty", got)
	}
}

func TestDeleteSubdir(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	_ = s.RecordSubdir(d.ID, "web")
	_ = s.RecordSubdir(d.ID, "cmd")

	if err := s.DeleteSubdir(d.ID, "web"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSubdirs(d.ID); strings.Join(got, ",") != "cmd" {
		t.Fatalf("history after delete = %v", got)
	}
	// Deleting something that is already gone is the client repeating itself,
	// not an error.
	if err := s.DeleteSubdir(d.ID, "web"); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
}

// The history hangs off the directory row; removing the directory must not
// leave rows nothing can reach.
func TestSubdirHistoryDiesWithItsDirectory(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.CreateDir("repos", "/repos")
	if err := s.RecordSubdir(d.ID, "web"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDir(d.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSubdirs(d.ID); len(got) != 0 {
		t.Fatalf("history survived its directory: %v", got)
	}
}
```

`tools_test.go` already imports `strings`; add `fmt` to its import block.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/store/ -run TestSubdir`
Expected: FAIL — `s.RecordSubdir undefined`, `s.ListSubdirs undefined`, `s.DeleteSubdir undefined`.

- [ ] **Step 3: Append the migration**

In `internal/store/store.go`, add as the **last** entry of the `migrations` slice (after the `sessions ADD COLUMN label` entry):

```go
	// Per-directory memory of subdirs that were actually launched into. It
	// cascades off dirs: a deleted directory's history is meaningless, and
	// store.Open sets foreign_keys(1), so SQLite does the cleanup.
	`CREATE TABLE dir_subdirs (
		dir_id  INTEGER NOT NULL REFERENCES dirs(id) ON DELETE CASCADE,
		subdir  TEXT NOT NULL,
		used_at TEXT NOT NULL,
		PRIMARY KEY (dir_id, subdir)
	);`,
```

- [ ] **Step 4: Write the store functions**

Append to `internal/store/tools.go`:

```go
// subdirHistoryLimit caps the remembered subdirs per directory. Ten covers the
// handful a person actually cycles through and keeps the dropdown short enough
// that it never needs its own scrollbar.
const subdirHistoryLimit = 10

// Recency ordering is a plain lexicographic ORDER BY on used_at, so the format
// must be fixed width: two launches inside the same second are ordinary, and
// time.RFC3339's second precision would tie them. This is RFC3339 with a
// constant nine-digit fraction, so time.Parse(time.RFC3339, …) still reads it.
const subdirTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// ListSubdirs returns the subdirs last launched into under dirID, newest
// first, at most subdirHistoryLimit of them. The result is never nil: it is
// written straight to JSON, where nil would marshal as null.
func (s *Store) ListSubdirs(dirID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT subdir FROM dir_subdirs WHERE dir_id = ? ORDER BY used_at DESC LIMIT ?`,
		dirID, subdirHistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var sub string
		if err := rows.Scan(&sub); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// RecordSubdir remembers subdir under dirID, or bumps it to the front if it is
// already remembered. A blank subdir is not history, so it is dropped.
func (s *Store) RecordSubdir(dirID int64, subdir string) error {
	subdir = strings.TrimSpace(subdir)
	if subdir == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO dir_subdirs (dir_id, subdir, used_at) VALUES (?, ?, ?)
		 ON CONFLICT(dir_id, subdir) DO UPDATE SET used_at = excluded.used_at`,
		dirID, subdir, time.Now().UTC().Format(subdirTimeFormat)); err != nil {
		return err
	}
	// Trimming inside the insert's transaction is what bounds the table: two
	// tabs launching at once cannot interleave into a list above the limit.
	if _, err := tx.Exec(
		`DELETE FROM dir_subdirs WHERE dir_id = ? AND subdir NOT IN (
			SELECT subdir FROM dir_subdirs WHERE dir_id = ? ORDER BY used_at DESC LIMIT ?)`,
		dirID, dirID, subdirHistoryLimit); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSubdir forgets one remembered subdir. Deleting an entry that is
// already gone succeeds — the caller is repeating itself, not misbehaving.
func (s *Store) DeleteSubdir(dirID int64, subdir string) error {
	_, err := s.db.Exec(`DELETE FROM dir_subdirs WHERE dir_id = ? AND subdir = ?`, dirID, subdir)
	return err
}
```

Add `strings` and `time` to the import block at the top of `tools.go` (it currently imports `database/sql` and `fmt`).

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/store/`
Expected: PASS, including the pre-existing tests (the new migration must not disturb them).

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/tools.go internal/store/tools_test.go
git commit -m "feat: remember launched subdirs per directory in the store"
```

---

### Task 2: Record a subdir when a launch actually succeeds

**Files:**
- Modify: `internal/server/sessions.go:131-146` (after `Tmux.CreateSession` succeeds)
- Test: `internal/server/sessions_test.go` (append)

**Interfaces:**
- Consumes: `Store.RecordSubdir(dirID int64, subdir string) error`, `Store.ListSubdirs(dirID int64) ([]string, error)` from Task 1.
- Produces: nothing new; behaviour only.

- [ ] **Step 1: Write the failing tests**

Append to `internal/server/sessions_test.go`:

```go
func TestCreateSessionRecordsSubdirHistory(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "web", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, _ := st.CreateDir("tmp", base)

	body := fmt.Sprintf(`{"toolId":%d,"dirId":%d,"subdir":"web/src"}`, tool.ID, dir.ID)
	if w := do(t, s, "POST", "/api/sessions", token, body); w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	got, err := st.ListSubdirs(dir.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "web/src" {
		t.Fatalf("history = %v, want [web/src]", got)
	}
}

// History is a list of subdirs that worked. A rejected subdir is a typo, and
// offering a typo back as a suggestion is worse than forgetting it.
func TestRejectedAndEmptySubdirsAreNotRecorded(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	base := t.TempDir()
	dir, _ := st.CreateDir("tmp", base)

	for _, subdir := range []string{"..", "missing", "/etc"} {
		body := fmt.Sprintf(`{"toolId":%d,"dirId":%d,"subdir":%q}`, tool.ID, dir.ID, subdir)
		if w := do(t, s, "POST", "/api/sessions", token, body); w.Code != 400 {
			t.Fatalf("subdir %q = %d, want 400", subdir, w.Code)
		}
	}
	body := fmt.Sprintf(`{"toolId":%d,"dirId":%d,"subdir":""}`, tool.ID, dir.ID)
	if w := do(t, s, "POST", "/api/sessions", token, body); w.Code != 201 {
		t.Fatalf("create with no subdir = %d: %s", w.Code, w.Body.String())
	}
	if got, _ := st.ListSubdirs(dir.ID); len(got) != 0 {
		t.Fatalf("history = %v, want empty", got)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/server/ -run "SubdirHistory|NotRecorded"`
Expected: FAIL — `history = [], want [web/src]`.

- [ ] **Step 3: Record after the launch succeeds**

In `internal/server/sessions.go`, inside `handleCreateSession`, insert between the `replacedOrphan` log block and the `slog.Info("session created", …)` call:

```go
	// Recorded only here, once tmux has really started: resolveSubdir rejects
	// bad subdirs above and a tmux failure rolls the row back, so anything that
	// reaches this line is a subdir worth suggesting again. A failed history
	// write is logged and dropped — the session exists and the response is
	// already a success.
	if err := s.cfg.Store.RecordSubdir(dir.ID, in.Subdir); err != nil {
		slog.Warn("subdir history not recorded", "directory_id", dir.ID, "error", err)
	}
```

`RecordSubdir` trims and ignores a blank subdir itself, so no guard is needed here.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/server/ -run "Session"`
Expected: PASS (needs tmux on PATH).

- [ ] **Step 5: Commit**

```bash
git add internal/server/sessions.go internal/server/sessions_test.go
git commit -m "feat: record a subdir once its launch succeeds"
```

---

### Task 3: The two history routes

**Files:**
- Modify: `internal/server/server.go:85-88` (route table, in the `/api/dirs` group)
- Modify: `internal/server/api.go` (add handlers after `handleDeleteDir`, around line 161)
- Test: `internal/server/api_test.go` (append)

**Interfaces:**
- Consumes: `Store.ListSubdirs`, `Store.RecordSubdir`, `Store.DeleteSubdir` from Task 1; `pathID(r *http.Request) (int64, error)` from `internal/server/api.go:19`.
- Produces:
  - `GET /api/dirs/{id}/subdirs` → `200 ["web/src","cmd"]`
  - `DELETE /api/dirs/{id}/subdirs?subdir=web%2Fsrc` → `204`

- [ ] **Step 1: Write the failing tests**

Append to `internal/server/api_test.go`:

```go
func TestSubdirHistoryRoutes(t *testing.T) {
	s, st, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	dir, _ := st.CreateDir("repos", t.TempDir())
	_ = st.RecordSubdir(dir.ID, "web")
	_ = st.RecordSubdir(dir.ID, "internal/server")

	w := do(t, s, "GET", fmt.Sprintf("/api/dirs/%d/subdirs", dir.ID), token)
	if w.Code != 200 {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
	var got []string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "internal/server,web" {
		t.Fatalf("list = %v, want newest first", got)
	}

	// The subdir travels as a query parameter: it contains slashes, which a
	// {wildcard} path segment does not match.
	path := fmt.Sprintf("/api/dirs/%d/subdirs?subdir=%s", dir.ID, url.QueryEscape("internal/server"))
	if w := do(t, s, "DELETE", path, token); w.Code != 204 {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}
	if got, _ := st.ListSubdirs(dir.ID); strings.Join(got, ",") != "web" {
		t.Fatalf("history after delete = %v", got)
	}
	// Idempotent: repeating the delete is still a 204.
	if w := do(t, s, "DELETE", path, token); w.Code != 204 {
		t.Fatalf("repeat delete = %d", w.Code)
	}
}

// A tab's directory list can race a directory deletion, so an unknown id is an
// empty history, not a 404. A non-numeric id is a malformed request.
func TestSubdirHistoryBadRequests(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	w := do(t, s, "GET", "/api/dirs/9999/subdirs", token)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("unknown dir = %d %q, want 200 []", w.Code, w.Body.String())
	}
	if w := do(t, s, "GET", "/api/dirs/abc/subdirs", token); w.Code != 400 {
		t.Fatalf("non-numeric id = %d, want 400", w.Code)
	}
	if w := do(t, s, "DELETE", "/api/dirs/1/subdirs", token); w.Code != 400 {
		t.Fatalf("delete without subdir = %d, want 400", w.Code)
	}
}

func TestSubdirHistoryRequiresAuth(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	dir, _ := st.CreateDir("repos", t.TempDir())

	if w := do(t, s, "GET", fmt.Sprintf("/api/dirs/%d/subdirs", dir.ID), ""); w.Code != 401 && w.Code != 403 {
		t.Fatalf("unauthenticated list = %d, want 401/403", w.Code)
	}
	if w := do(t, s, "DELETE", fmt.Sprintf("/api/dirs/%d/subdirs?subdir=web", dir.ID), ""); w.Code != 401 && w.Code != 403 {
		t.Fatalf("unauthenticated delete = %d, want 401/403", w.Code)
	}
}
```

Add `net/url` to the `api_test.go` import block (it already imports `encoding/json`, `fmt`, `net/http`, `strings`, `testing`).

`newTestServer(t, true)` (`internal/server/server_test.go:24`) returns `(*Server, *store.Store, *auth.Manager)`; `true` means a credential exists, so auth is enforced.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/server/ -run TestSubdirHistory`
Expected: FAIL — the routes 404, so `list = 404`.

- [ ] **Step 3: Add the handlers**

In `internal/server/api.go`, after `handleDeleteDir`:

```go
// A directory's remembered subdirs. An unknown id answers with an empty list
// rather than 404: a tab's directory list can legitimately race a deletion, and
// "no history" is the right answer either way.
func (s *Server) handleListSubdirs(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	subdirs, err := s.cfg.Store.ListSubdirs(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, subdirs)
}

// The subdir is a query parameter, not a path segment: subdirs contain
// slashes, which ServeMux's {id} wildcard does not match.
func (s *Server) handleDeleteSubdir(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	subdir := r.URL.Query().Get("subdir")
	if subdir == "" {
		writeJSON(w, 400, map[string]string{"error": "subdir required"})
		return
	}
	if err := s.cfg.Store.DeleteSubdir(id, subdir); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(204)
}
```

No `slog` line here: the subdir is user path text, and the other read/delete handlers of this size do not log either.

- [ ] **Step 4: Register the routes**

In `internal/server/server.go`, in the `/api/dirs` group (after the `DELETE /api/dirs/{id}` line):

```go
	s.mux.HandleFunc("GET /api/dirs/{id}/subdirs", s.handleListSubdirs)
	s.mux.HandleFunc("DELETE /api/dirs/{id}/subdirs", s.handleDeleteSubdir)
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/server/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/api.go internal/server/server.go internal/server/api_test.go
git commit -m "feat: serve per-directory subdir history"
```

---

### Task 4: Launcher — clear on directory change and load that directory's history

**Files:**
- Modify: `web/src/grid/HeaderLauncher.tsx`
- Test: `web/src/__tests__/header-launcher.test.tsx` (append)

**Interfaces:**
- Consumes: `GET /api/dirs/{id}/subdirs` from Task 3; `getJSON<T>(server, path)` from `web/src/api.ts:75`.
- Produces: component state `history: string[]`, and `selectDir(id: number)`, used by Tasks 5–7.

- [ ] **Step 1: Write the failing tests**

Append to `web/src/__tests__/header-launcher.test.tsx`:

```tsx
const twoDirs = [
  { id: 7, name: "multimux", path: "/repos/multimux" },
  { id: 8, name: "home", path: "/home/jon" },
];

// Mocks a daemon with two dirs and a per-dir history. The subdirs check must
// come before the /api/dirs one — the history path contains it.
function mockDaemonWithHistory(history: Record<number, string[]>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const hist = url.match(/\/api\/dirs\/(\d+)\/subdirs/);
    if (hist) {
      if ((init?.method ?? "GET") === "DELETE") return new Response(null, { status: 204 });
      return new Response(JSON.stringify(history[Number(hist[1])] ?? []));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(twoDirs));
    if (url.includes("/api/sessions") && (init?.method ?? "GET") === "POST")
      return new Response(JSON.stringify({ id: 3, tmuxName: "mm-3", toolId: 1, dir: "/a", status: "running" }), {
        status: 201,
      });
    return new Response("[]");
  });
}

// A subdir is relative to the selected directory, so it means nothing once the
// directory changes.
test("changing the directory clears the subdir and loads that directory's history", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src"], 8: ["Downloads"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  await waitFor(() => expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/api/dirs/7/subdirs"))).toBe(true));
  fireEvent.change(subdir, { target: { value: "web" } });

  fireEvent.change(screen.getByLabelText("dir"), { target: { value: "8" } });

  expect(subdir.value).toBe("");
  await waitFor(() =>
    expect(fetchMock.mock.calls.some(([u]) => String(u).includes("/api/dirs/8/subdirs"))).toBe(true),
  );
  fireEvent.focus(subdir);
  expect(await screen.findByText("Downloads")).toBeInTheDocument();
  expect(screen.queryByText("web/src")).toBeNull();
});
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: FAIL — no `/api/dirs/7/subdirs` request is ever made.

- [ ] **Step 3: Add the state, the fetch, and the clearing**

In `web/src/grid/HeaderLauncher.tsx`:

Import `del` alongside the existing helpers (it is used in Task 6; adding it now would be an unused-import lint error, so import it in Task 6 instead):

```tsx
import { getJSON, postJSON } from "../api";
```

Add state beside the existing hooks:

```tsx
  const [history, setHistory] = useState<string[]>([]);
```

Clear it in `selectServer`, next to the existing `setSubdir("")`:

```tsx
    setHistory([]);
```

Add a directory-change handler above the `return`, and use it from the dir `<select>`:

```tsx
  // A subdir names a path under the selected directory. Changing the directory
  // makes it meaningless, so it is dropped rather than silently re-pointed.
  function selectDir(id: number) {
    setDirId(id);
    setSubdir("");
    setHistory([]);
    setError("");
  }
```

```tsx
          <select
            aria-label="dir"
            value={dirId}
            onChange={(e) => selectDir(Number(e.target.value))}
          >
```

Add the history effect below the existing tools/dirs effect:

```tsx
  // Same `stale` guard as the tools/dirs fetch: a slow answer for a directory
  // the user has already moved on from must not land over the current one.
  useEffect(() => {
    if (!server || dirId <= 0) return;
    let stale = false;
    getJSON<string[]>(server, `/api/dirs/${dirId}/subdirs`)
      .then((h) => {
        if (!stale) setHistory(h);
      })
      .catch(() => {
        // History is a convenience; failing to load it must not break launching.
        if (!stale) setHistory([]);
      });
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId, dirId]);
```

The test's assertion on visible history rows only passes once Task 5 renders them; if the run order puts this task first, that assertion fails until Task 5 lands. Implement Task 5 immediately after, and treat Step 4 below as the checkpoint for the two clearing/fetch assertions.

- [ ] **Step 4: Run the test and confirm the fetch/clear assertions pass**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: the `/api/dirs/7/subdirs` and `/api/dirs/8/subdirs` and cleared-value assertions pass; the `findByText("Downloads")` assertion still fails (no dropdown yet).

- [ ] **Step 5: Commit**

```bash
git add web/src/grid/HeaderLauncher.tsx web/src/__tests__/header-launcher.test.tsx
git commit -m "feat: load per-directory subdir history in the launcher"
```

---

### Task 5: Launcher — focus overlay and the history dropdown

**Files:**
- Modify: `web/src/grid/HeaderLauncher.tsx`
- Modify: `web/src/index.css:396-413` (the `input.subdir` rules)
- Test: `web/src/__tests__/header-launcher.test.tsx` (append)

**Interfaces:**
- Consumes: `history`, `selectDir` from Task 4.
- Produces: `open: boolean`, `filtered: string[]`, and the `.subdir-wrap` / `.subdir-history` markup used by Tasks 6–7.

- [ ] **Step 1: Write the failing tests**

Append to `web/src/__tests__/header-launcher.test.tsx`:

```tsx
test("the history appears on focus, filters as you type, and hides when nothing matches", async () => {
  mockDaemonWithHistory({ 7: ["web/src", "cmd", "internal/server"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  // Nothing is shown until the field is focused: the header stays quiet.
  await waitFor(() => expect(screen.queryByText("web/src")).toBeNull());

  fireEvent.focus(subdir);
  expect(await screen.findByText("web/src")).toBeInTheDocument();
  expect(screen.getByText("cmd")).toBeInTheDocument();

  // Substring match, so a deep path is reachable without typing its prefix.
  fireEvent.change(subdir, { target: { value: "serv" } });
  expect(screen.getByText("internal/server")).toBeInTheDocument();
  expect(screen.queryByText("cmd")).toBeNull();

  fireEvent.change(subdir, { target: { value: "zzz" } });
  expect(screen.queryByText("internal/server")).toBeNull();

  fireEvent.blur(subdir);
  fireEvent.change(subdir, { target: { value: "" } });
  expect(screen.queryByText("web/src")).toBeNull();
});

test("clicking a remembered subdir fills the field and launches with it", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.focus(subdir);
  fireEvent.click(await screen.findByText("web/src"));

  expect(subdir.value).toBe("web/src");
  fireEvent.click(screen.getByText("+ New"));
  await waitFor(() => {
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(post && JSON.parse(String(post[1]?.body)).subdir).toBe("web/src");
  });
});
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: FAIL — `Unable to find an element with the text: web/src`.

- [ ] **Step 3: Render the overlay and dropdown**

In `web/src/grid/HeaderLauncher.tsx`, add state:

```tsx
  const [open, setOpen] = useState(false);
```

Set `setOpen(false)` in `selectServer` and `selectDir` alongside the other resets.

Derive the filtered list just above the `return`, after `canLaunch`:

```tsx
  // Substring, case-insensitive: a remembered "internal/server" should be
  // reachable by typing "serv", not only by typing its prefix.
  const needle = subdir.trim().toLowerCase();
  const filtered = history.filter((h) => h.toLowerCase().includes(needle));
  const showHistory = open && filtered.length > 0;
```

Replace the existing `<input className="subdir" … />` element with:

```tsx
          <div className="subdir-wrap">
            <input
              className="subdir"
              aria-label="subdirectory"
              placeholder="subdir"
              value={subdir}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              onFocus={() => setOpen(true)}
              onBlur={() => setOpen(false)}
              onChange={(e) => {
                setSubdir(e.target.value);
                setOpen(true);
                setError("");
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") launch();
              }}
            />
            {showHistory && (
              // preventDefault on mousedown keeps the input's blur from firing
              // first: without it the panel unmounts before any click lands.
              <div className="subdir-history" onMouseDown={(e) => e.preventDefault()}>
                {filtered.map((h) => (
                  <div key={h} className="subdir-history-row">
                    <button
                      type="button"
                      className="subdir-pick"
                      onClick={() => {
                        setSubdir(h);
                        setOpen(false);
                      }}
                    >
                      {h}
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
```

- [ ] **Step 4: Style the overlay**

In `web/src/index.css`, replace the `.header-launcher input.subdir` rule with:

```css
/* The wrapper holds the resting width in the header's flex row. The input
   leaves the flow when focused, so the field can be readable without any other
   header control moving. */
.subdir-wrap {
  position: relative;
  flex: 0 0 auto;
  width: 4rem;
  height: 1.9rem;
}

.header-launcher input.subdir {
  font-family: var(--mono);
  font-size: 0.8rem;
  color: var(--ink);
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.3rem 0.4rem;
  width: 100%;
  height: 100%;
  min-width: 0;
}

.subdir-wrap:focus-within input.subdir {
  position: absolute;
  left: 0;
  top: 0;
  width: min(20rem, calc(100vw - 3rem));
  z-index: 20;
}

.subdir-history {
  position: absolute;
  left: 0;
  top: calc(100% + 0.3rem);
  z-index: 20;
  width: min(20rem, calc(100vw - 3rem));
  max-height: 14rem;
  overflow-y: auto;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 6px 18px rgb(0 0 0 / 35%);
}

.subdir-history-row {
  display: flex;
  align-items: center;
}

.subdir-history-row button {
  font-family: var(--mono);
  font-size: 0.8rem;
  color: var(--ink);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0.35rem 0.5rem;
}

.subdir-pick {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: left;
}

.subdir-history-row:hover,
.subdir-history-row.on {
  background: color-mix(in srgb, var(--accent) 18%, transparent);
}

/* Anchored right on a narrow viewport, so the overlay grows towards the middle
   of the header instead of off the edge. */
@media (max-width: 40rem) {
  .subdir-wrap:focus-within input.subdir,
  .subdir-history {
    left: auto;
    right: 0;
  }
}
```

Leave the existing `.header-launcher select:focus-visible, .header-launcher input.subdir:focus-visible, …` outline rule untouched.

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: PASS — including Task 4's `findByText("Downloads")` assertion.

- [ ] **Step 6: Commit**

```bash
git add web/src/grid/HeaderLauncher.tsx web/src/index.css web/src/__tests__/header-launcher.test.tsx
git commit -m "feat: expand the subdir field on focus with a history dropdown"
```

---

### Task 6: Launcher — delete a remembered subdir, and remember a fresh one

**Files:**
- Modify: `web/src/grid/HeaderLauncher.tsx`
- Test: `web/src/__tests__/header-launcher.test.tsx` (append)

**Interfaces:**
- Consumes: `DELETE /api/dirs/{id}/subdirs?subdir=…` from Task 3; `del(server, path)` from `web/src/api.ts:103`; `history`, `filtered`, `showHistory` from Tasks 4–5.
- Produces: `forget(value: string)`.

- [ ] **Step 1: Write the failing tests**

Append to `web/src/__tests__/header-launcher.test.tsx`:

```tsx
test("the x forgets a remembered subdir", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src", "cmd"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  fireEvent.focus(await screen.findByLabelText("subdirectory"));
  await screen.findByText("web/src");
  fireEvent.click(screen.getByLabelText("forget web/src"));

  await waitFor(() => expect(screen.queryByText("web/src")).toBeNull());
  expect(screen.getByText("cmd")).toBeInTheDocument();
  const sent = fetchMock.mock.calls.find(([u, init]) => init?.method === "DELETE");
  expect(String(sent?.[0])).toContain(`/api/dirs/7/subdirs?subdir=${encodeURIComponent("web/src")}`);
});

// A failed delete must put the entry back rather than lie about forgetting it.
test("a failed forget restores the entry and reports the error", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    if (/\/api\/dirs\/\d+\/subdirs/.test(url)) {
      if ((init?.method ?? "GET") === "DELETE") return new Response("nope", { status: 500 });
      return new Response(JSON.stringify(["web/src"]));
    }
    if (url.includes("/api/tools")) return new Response(JSON.stringify(localTools));
    if (url.includes("/api/dirs")) return new Response(JSON.stringify(localDirs));
    return new Response("[]");
  });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  fireEvent.focus(await screen.findByLabelText("subdirectory"));
  fireEvent.click(await screen.findByLabelText("forget web/src"));

  expect(await screen.findByText(/couldn't forget/i)).toBeInTheDocument();
  expect(screen.getByText("web/src")).toBeInTheDocument();
});

// The just-launched subdir is the most likely next one, so it goes to the
// front without waiting for a refetch.
test("a successful launch adds its subdir to the history", async () => {
  mockDaemonWithHistory({ 7: ["cmd"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.change(subdir, { target: { value: "web/src" } });
  fireEvent.click(screen.getByText("+ New"));

  await waitFor(() => expect(subdir.value).toBe("web/src"));
  fireEvent.focus(subdir);
  fireEvent.change(subdir, { target: { value: "" } });
  fireEvent.focus(subdir);
  const rows = await screen.findAllByRole("button", { name: /^(web\/src|cmd)$/ });
  expect(rows.map((r) => r.textContent)).toEqual(["web/src", "cmd"]);
});
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: FAIL — `Unable to find a label with the text of: forget web/src`.

- [ ] **Step 3: Implement forget and the post-launch bump**

In `web/src/grid/HeaderLauncher.tsx`, extend the import:

```tsx
import { del, getJSON, postJSON } from "../api";
```

Add `forget` beside `launch`:

```tsx
  // Optimistic: the row disappears at once, and a failed DELETE puts it back
  // rather than leaving the UI claiming something was forgotten.
  async function forget(value: string) {
    if (!server) return;
    const previous = history;
    setHistory((h) => h.filter((x) => x !== value));
    try {
      await del(server, `/api/dirs/${dirId}/subdirs?subdir=${encodeURIComponent(value)}`);
    } catch (e) {
      setHistory(previous);
      setError(`couldn't forget ${value}: ${e instanceof Error ? e.message : e}`);
    }
  }
```

In `launch`, after `onLaunched(server, sess)`, bump the subdir to the front of the local list:

```tsx
      // The daemon has recorded this too; updating locally keeps the dropdown
      // right without a second round trip.
      const used = subdir.trim();
      if (used) setHistory((h) => [used, ...h.filter((x) => x !== used)]);
```

Add the delete button inside `.subdir-history-row`, after the `.subdir-pick` button:

```tsx
                    <button
                      type="button"
                      className="subdir-forget"
                      aria-label={`forget ${h}`}
                      onClick={() => forget(h)}
                    >
                      ×
                    </button>
```

- [ ] **Step 4: Style the forget button**

Append to `web/src/index.css`, after the `.subdir-pick` rule:

```css
.subdir-forget {
  flex: 0 0 auto;
  color: var(--muted);
  line-height: 1;
}

.subdir-forget:hover {
  color: var(--ink);
}
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/grid/HeaderLauncher.tsx web/src/index.css web/src/__tests__/header-launcher.test.tsx
git commit -m "feat: forget a remembered subdir from the launcher"
```

---

### Task 7: Launcher — keyboard navigation

**Files:**
- Modify: `web/src/grid/HeaderLauncher.tsx`
- Test: `web/src/__tests__/header-launcher.test.tsx` (append)

**Interfaces:**
- Consumes: `filtered`, `showHistory`, `open` from Task 5.
- Produces: `highlight: number` state; final `onKeyDown` behaviour.

- [ ] **Step 1: Write the failing tests**

Append to `web/src/__tests__/header-launcher.test.tsx`:

```tsx
test("arrow keys pick a remembered subdir and Enter fills it", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src", "cmd"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.focus(subdir);
  await screen.findByText("web/src");

  fireEvent.keyDown(subdir, { key: "ArrowDown" });
  fireEvent.keyDown(subdir, { key: "ArrowDown" });
  fireEvent.keyDown(subdir, { key: "Enter" });

  expect(subdir.value).toBe("cmd");
  // Enter selected rather than launched.
  expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);
});

// Typing must not arm a suggestion: type-and-Enter has always launched what was
// typed, and it still does.
test("Enter after typing launches instead of selecting", async () => {
  const fetchMock = mockDaemonWithHistory({ 7: ["web/src"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.focus(subdir);
  fireEvent.keyDown(subdir, { key: "ArrowDown" });
  fireEvent.change(subdir, { target: { value: "web" } });
  fireEvent.keyDown(subdir, { key: "Enter" });

  await waitFor(() => {
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(post && JSON.parse(String(post[1]?.body)).subdir).toBe("web");
  });
});

test("Escape closes the history without clearing the field", async () => {
  mockDaemonWithHistory({ 7: ["web/src"] });
  render(<HeaderLauncher servers={[servers[0]]} onLaunched={vi.fn()} />);

  const subdir = await screen.findByLabelText<HTMLInputElement>("subdirectory");
  fireEvent.change(subdir, { target: { value: "web" } });
  fireEvent.focus(subdir);
  await screen.findByText("web/src");

  fireEvent.keyDown(subdir, { key: "Escape" });
  expect(screen.queryByText("web/src")).toBeNull();
  expect(subdir.value).toBe("web");
});
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: FAIL — `expect(subdir.value).toBe("cmd")` gets `""`.

- [ ] **Step 3: Implement the key handling**

In `web/src/grid/HeaderLauncher.tsx`, add state:

```tsx
  const [highlight, setHighlight] = useState(-1);
```

Reset it to `-1` in `selectServer`, in `selectDir`, and in the input's `onChange` (typing disarms any highlighted suggestion, so Enter still launches what was typed).

Replace the input's `onKeyDown` with:

```tsx
              onKeyDown={(e) => {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  if (filtered.length === 0) return;
                  e.preventDefault();
                  setOpen(true);
                  const step = e.key === "ArrowDown" ? 1 : -1;
                  setHighlight((h) =>
                    h < 0
                      ? step > 0
                        ? 0
                        : filtered.length - 1
                      : (h + step + filtered.length) % filtered.length,
                  );
                } else if (e.key === "Enter") {
                  if (showHistory && highlight >= 0 && highlight < filtered.length) {
                    e.preventDefault();
                    setSubdir(filtered[highlight]);
                    setHighlight(-1);
                    setOpen(false);
                  } else {
                    launch();
                  }
                } else if (e.key === "Escape") {
                  if (open) {
                    setOpen(false);
                    setHighlight(-1);
                  } else {
                    e.currentTarget.blur();
                  }
                }
              }}
```

Mark the highlighted row in the map so the CSS `.on` rule applies, and reset the highlight when the mouse takes over:

```tsx
                {filtered.map((h, i) => (
                  <div
                    key={h}
                    className={`subdir-history-row${i === highlight ? " on" : ""}`}
                    onMouseEnter={() => setHighlight(-1)}
                  >
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd web && pnpm test src/__tests__/header-launcher.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/grid/HeaderLauncher.tsx web/src/__tests__/header-launcher.test.tsx
git commit -m "feat: keyboard-navigate the subdir history"
```

---

### Task 8: Full verification and a real-daemon check

**Files:**
- No source changes expected. Fix whatever the run turns up, in the file that owns it.

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: a verified build.

- [ ] **Step 1: Run the whole CI suite**

Run: `./verify.sh`
Expected: PASS — gofmt, `go vet`, `go test ./...`, `pnpm lint`, `pnpm test`, `pnpm build`, `go build`, `scripts/smoke.sh`. Requires tmux on PATH. Fix any failure or warning before continuing.

- [ ] **Step 2: Drive it against a throwaway daemon**

```bash
export MULTIMUX_DATA_DIR="/tmp/multimux-dev-$(date +%s)"
go run . serve --dev --port 8787
```

In another shell:

```bash
cd web && MULTIMUX_DEV_TARGET=https://localhost:8787 pnpm dev
```

Register a throwaway passkey at the `/setup?code=…` URL the daemon prints (Chrome or Firefox — Safari will not send `Secure` cookies over `http://localhost`). Then confirm by hand:

1. The resting subdir field is narrower than before and no header control moves when it is focused.
2. Launching into a subdir, then reopening the field, shows that subdir at the top of the list.
3. Changing the directory empties the field and swaps the list.
4. The `x` removes an entry, and it stays gone after a page reload.
5. Arrow keys and Enter fill the field; typing then Enter launches what was typed.

- [ ] **Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: <what the verification turned up>"
```

Skip this step if nothing needed fixing.

---

## Notes for the implementer

- `store.Open` already sets `_pragma=foreign_keys(1)`; the `ON DELETE CASCADE` needs no extra code, but it also means `RecordSubdir` against a deleted directory returns a foreign-key error. That is why Task 2 logs and swallows it.
- `writeJSON` marshals a nil slice as `null`. `ListSubdirs` returns `[]string{}` for exactly this reason — do not "simplify" it to `var out []string`.
- The overlay is driven by CSS `:focus-within`, not by the `open` state. `open` controls only the dropdown, so Escape can hide the list while leaving the field wide and focused.
- jsdom does not apply `:focus-within`, so the overlay sizing is not covered by tests. Step 2 of Task 8 is where that gets checked.
