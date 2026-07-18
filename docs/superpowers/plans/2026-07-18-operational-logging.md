# Operational Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add useful request completion and safe domain-event entries to the normal multimux service log.

**Architecture:** Move the HTTP response recorder and request logger into a focused `logging.go` unit. The middleware owns route filtering, status, duration, and available 5xx error text; handlers emit explicit structured events only after successful state changes.

**Tech Stack:** Go 1.24, `net/http`, `log/slog`, Go’s `testing` and `httptest` packages, Gorilla WebSocket, SQLite-backed stores.

---

## File Map

- Create `internal/server/logging.go`: response metadata recording, WebSocket
  hijack forwarding, request filtering, and structured completion logging.
- Create `internal/server/logging_test.go`: shared log capture helper and focused
  request middleware tests.
- Modify `internal/server/server.go`: remove the moved middleware and let
  `writeJSON` attach safe 5xx error metadata.
- Modify `internal/server/api.go` and `internal/server/api_test.go`: resource
  and configuration events plus privacy assertions.
- Modify `internal/server/sessions.go` and
  `internal/server/sessions_test.go`: session, orphan, layout, and
  reconciliation events.
- Modify `internal/server/authapi.go`,
  `internal/server/authapi_test.go`, and `internal/server/events.go`:
  authentication and maintenance events.

### Task 1: Request Completion Logging

**Files:**

- Create: `internal/server/logging.go`
- Create: `internal/server/logging_test.go`
- Modify: `internal/server/server.go:6-14,218-247,286-290`
- Modify: `internal/server/server_test.go:3-12,234-253`

- [ ] **Step 1: Write failing request-log tests**

Create a JSON `slog` capture helper and tests which exercise `logRequests`
directly:

```go
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestRequestLoggingIncludesApplicationRequestDetails(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"token": "response-secret"})
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tools?token=query-secret", nil))

	logged := buf.String()
	for _, want := range []string{`"level":"INFO"`, `"msg":"http"`, `"method":"GET"`, `"path":"/api/tools"`, `"status":200`, `"duration":`} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{"query-secret", "response-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log exposed %q: %s", secret, logged)
		}
	}
}

func TestRequestLoggingSuppressesSuccessfulNonApplicationPaths(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if buf.Len() != 0 {
		t.Fatalf("routine non-application requests logged: %s", buf.String())
	}
}

func TestRequestLoggingIncludesErrorsOnAnyPath(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database closed"})
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz?token=secret", nil))
	logged := buf.String()
	for _, want := range []string{`"level":"ERROR"`, `"path":"/healthz"`, `"status":500`, `"error":"database closed"`} {
		if !strings.Contains(logged, want) {
			t.Fatalf("error log missing %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, "secret") {
		t.Fatalf("query string leaked: %s", logged)
	}
}

func TestRequestLoggingIncludesClientErrorsOnAnyPath(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	if logged := buf.String(); !strings.Contains(logged, `"status":404`) || !strings.Contains(logged, `"level":"INFO"`) {
		t.Fatalf("client error not logged at info: %s", logged)
	}
}
```

Add a recorder test with a minimal `http.Hijacker` fake. It must assert that
`Hijack` changes the recorded status to 101 and returns the underlying
connection without an error.

- [ ] **Step 2: Run request-log tests and verify failure**

Run:

```bash
go test ./internal/server -run 'TestRequestLogging|TestStatusRecorderHijack' -count=1
```

Expected: failures because successful API requests are still `Debug`, health
requests are logged, no duration or error attribute is present, and hijacking
does not record 101.

- [ ] **Step 3: Implement focused request logging**

Move `statusRecorder` and `logRequests` from `server.go` to `logging.go`.
Implement these semantics:

```go
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	err         string
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(p)
}

func (r *statusRecorder) recordError(err string) {
	r.err = err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err == nil && !r.wroteHeader {
		r.status = http.StatusSwitchingProtocols
		r.wroteHeader = true
	}
	return conn, rw, err
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		if !strings.HasPrefix(r.URL.Path, "/api/") &&
			!strings.HasPrefix(r.URL.Path, "/ws/") &&
			rec.status < http.StatusBadRequest {
			return
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
		}
		level := slog.LevelInfo
		if rec.status >= http.StatusInternalServerError {
			level = slog.LevelError
			if rec.err != "" {
				attrs = append(attrs, slog.String("error", rec.err))
			}
		}
		slog.LogAttrs(r.Context(), level, "http", attrs...)
	})
}
```

Before `writeJSON` writes its header, attach the safe error string:

```go
if status >= http.StatusInternalServerError {
	if body, ok := v.(map[string]string); ok {
		if recorder, ok := w.(interface{ recordError(string) }); ok {
			recorder.recordError(body["error"])
		}
	}
}
```

Remove the now-unused `bufio`, `log/slog`, and `net` imports from
`server.go`. Replace `TestServerErrorsAreLogged` with the more specific tests
in `logging_test.go`.

- [ ] **Step 4: Run request-log tests**

Run:

```bash
gofmt -w internal/server/logging.go internal/server/logging_test.go internal/server/server.go internal/server/server_test.go
go test ./internal/server -run 'TestRequestLogging|TestStatusRecorderHijack' -count=1
```

Expected: all selected tests pass.

- [ ] **Step 5: Commit request logging**

```bash
git add internal/server/logging.go internal/server/logging_test.go internal/server/server.go internal/server/server_test.go
git commit -m "feat: add structured request completion logs"
```

### Task 2: Resource and Configuration Events

**Files:**

- Modify: `internal/server/api.go:3-11,31-75,89-121,131-150,167-189`
- Modify: `internal/server/api_test.go:3-9,11-78`

- [ ] **Step 1: Write failing resource-event tests**

Add tests that perform successful mutations and assert safe structured fields:

```go
func TestResourceMutationsAreLoggedWithoutSensitiveValues(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	buf := captureLogs(t)

	secretCommand := "command-value-must-not-leak"
	w := do(t, s, "POST", "/api/tools", token,
		`{"name":"codex","command":"`+secretCommand+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tool = %d: %s", w.Code, w.Body.String())
	}
	secretPath := t.TempDir()
	w = do(t, s, "POST", "/api/dirs", token,
		fmt.Sprintf(`{"name":"workspace","path":%q}`, secretPath))
	if w.Code != http.StatusCreated {
		t.Fatalf("create dir = %d: %s", w.Code, w.Body.String())
	}
	secretHostname := "private-host.example"
	w = do(t, s, "PUT", "/api/settings", token,
		`{"hostname":"`+secretHostname+`","extraSans":"","port":"8686"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put settings = %d: %s", w.Code, w.Body.String())
	}

	logged := buf.String()
	for _, want := range []string{
		`"msg":"tool created"`, `"tool_id":`, `"name":"codex"`,
		`"msg":"directory created"`, `"name":"workspace"`,
		`"msg":"settings changed"`, `"keys":`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("mutation log missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{secretCommand, secretPath, secretHostname} {
		if strings.Contains(logged, secret) {
			t.Fatalf("mutation log exposed %q: %s", secret, logged)
		}
	}
}
```

- [ ] **Step 2: Run the resource-event test and verify failure**

Run:

```bash
go test ./internal/server -run TestResourceMutationsAreLoggedWithoutSensitiveValues -count=1
```

Expected: failure because no resource or configuration domain events exist.

- [ ] **Step 3: Add explicit resource and configuration events**

Import `log/slog` in `api.go`. Emit after successful store operations:

```go
slog.Info("tool created", "tool_id", tool.ID, "name", tool.Name)
slog.Info("tool updated", "tool_id", tool.ID, "name", tool.Name)
slog.Info("tool deleted", "tool_id", id)
slog.Info("directory created", "directory_id", d.ID, "name", d.Name)
slog.Info("directory deleted", "directory_id", id)
slog.Info("settings changed", "keys", []string{"hostname", "extra_sans", "port"})
slog.Info("appearance changed", "keys", []string{"host_label", "accent_color"})
```

Do not pass tool commands, directory paths, or setting values to `slog`.

- [ ] **Step 4: Run API tests**

Run:

```bash
gofmt -w internal/server/api.go internal/server/api_test.go
go test ./internal/server -run 'TestResourceMutations|TestToolsCRUD|TestDirsValidation|TestSettingsRoundTrip|TestAppearance' -count=1
```

Expected: all selected tests pass.

- [ ] **Step 5: Commit resource events**

```bash
git add internal/server/api.go internal/server/api_test.go
git commit -m "feat: log resource and configuration changes"
```

### Task 3: Session and Reconciliation Events

**Files:**

- Modify: `internal/server/sessions.go:3-12,49-157,172-191,199-227`
- Modify: `internal/server/sessions_test.go:3-12,29-121,140-157`

- [ ] **Step 1: Write failing session-event tests**

Capture logs around the existing tmux-backed lifecycle test and assert these
messages and safe IDs:

```go
buf := captureLogs(t)
// Run the existing create, kill, and dismiss requests.
logged := buf.String()
for _, want := range []string{
	`"msg":"session created"`,
	`"msg":"session killed"`,
	`"msg":"session dismissed"`,
	`"session_id":`,
	`"tmux_name":`,
	`"tool_id":`,
	`"directory_id":`,
} {
	if !strings.Contains(logged, want) {
		t.Fatalf("session log missing %q: %s", want, logged)
	}
}
if strings.Contains(logged, dir.Path) {
	t.Fatalf("session log exposed directory path: %s", logged)
}
```

Capture logs in the orphan replacement test and require
`"msg":"orphan tmux session replaced"` with only `tmux_name`.

Capture logs in `TestReconcileMarksDead` and require `"msg":"session died"`.
Add this no-op assertion to `TestReconcileSparesFreshSessions`:

```go
buf := captureLogs(t)
// Run Reconcile as the existing test does.
if strings.Contains(buf.String(), `"msg":"session died"`) {
	t.Fatalf("no-op reconcile logged a death: %s", buf.String())
}
```

Capture the successful layout PUT and require `"msg":"layout changed"`.

- [ ] **Step 2: Run session-event tests and verify failure**

Run:

```bash
go test ./internal/server -run 'TestSessionCreateKillDismiss|TestCreateSessionReplacesOrphan|TestReconcile|TestLayoutAPI' -count=1
```

Expected: failure in environments with tmux because session events do not
exist; `TestLayoutAPI` fails everywhere for the same reason.

- [ ] **Step 3: Emit session events after successful transitions**

Import `log/slog` in `sessions.go` and add:

```go
slog.Info("orphan tmux session replaced", "tmux_name", sess.TmuxName)
slog.Info("session created",
	"session_id", sess.ID,
	"tmux_name", sess.TmuxName,
	"tool_id", tool.ID,
	"directory_id", dir.ID)
slog.Info("session killed", "session_id", sess.ID, "tmux_name", sess.TmuxName)
slog.Info("session dismissed", "session_id", sess.ID, "tmux_name", sess.TmuxName)
slog.Info("layout changed")
slog.Info("session died", "session_id", sess.ID, "tmux_name", sess.TmuxName)
```

Place each entry after the state-changing operation succeeds and before its
broadcast or response. The orphan replacement entry follows a successful
`KillSession`. The death entry follows `SetSessionStatus`.

- [ ] **Step 4: Run session tests**

Run:

```bash
gofmt -w internal/server/sessions.go internal/server/sessions_test.go
go test ./internal/server -run 'TestSessionCreateKillDismiss|TestCreateSessionReplacesOrphan|TestReconcile|TestLayoutAPI' -count=1
```

Expected: all selected tests pass, with tmux-backed tests skipped only when
tmux is unavailable.

- [ ] **Step 5: Commit session events**

```bash
git add internal/server/sessions.go internal/server/sessions_test.go
git commit -m "feat: log session lifecycle changes"
```

### Task 4: Authentication and Maintenance Events

**Files:**

- Modify: `internal/server/authapi.go:3-8,45-106,123-129,147-162,180-199`
- Modify: `internal/server/authapi_test.go:3-11,58-116`
- Modify: `internal/server/events.go:97-118`

- [ ] **Step 1: Write failing authentication-event tests**

Capture logs around the existing setup and login flow, then require setup,
passkey, and login events without the username, setup code, cookie, or
credential data:

```go
buf := captureLogs(t)
cookie, rp, authenticator := setupViaHTTP(t, s, code)
// Complete the existing login flow.
logged := buf.String()
for _, want := range []string{
	`"msg":"setup completed"`,
	`"msg":"passkey registered"`,
	`"key_name":"laptop"`,
	`"msg":"login succeeded"`,
} {
	if !strings.Contains(logged, want) {
		t.Fatalf("auth log missing %q: %s", want, logged)
	}
}
for _, secret := range []string{code, cookie, "jon"} {
	if strings.Contains(logged, secret) {
		t.Fatalf("auth log exposed %q: %s", secret, logged)
	}
}
```

Capture logs around `TestMintBearerToken`, retain the decoded minted token,
and require `"msg":"bearer session minted"` while asserting neither the
original nor minted token occurs in the log.

- [ ] **Step 2: Run authentication-event tests and verify failure**

Run:

```bash
go test ./internal/server -run 'TestSetupThenLoginFlow|TestMintBearerToken' -count=1
```

Expected: failure because the authentication domain events do not exist.

- [ ] **Step 3: Add safe authentication and maintenance entries**

Import `log/slog` in `authapi.go`. Emit:

```go
slog.Info("passkey registered", "key_name", keyName)
slog.Info("setup completed")
slog.Info("login succeeded")
if err := s.cfg.Auth.Logout(auth.TokenFromRequest(r)); err == nil {
	slog.Info("logout completed")
}
slog.Info("passkey deleted")
slog.Info("auth session revoked")
slog.Info("bearer session minted")
```

For ordinary passkey registration, assign
`keyName := r.URL.Query().Get("keyName")` once, pass it to
`FinishRegistration`, and log it after success. The logout response keeps its
existing best-effort behavior; only the log depends on successful deletion.
Never pass the token, credential route value, auth-session route value,
username, user agent, or request object to `slog`.

Use the existing row count returned by auth-session cleanup in `events.go`:

```go
if n, err := s.cfg.Store.DeleteExpiredAuthSessions(time.Now()); err != nil {
	slog.Error("session sweep", "err", err)
} else if n > 0 {
	slog.Info("auth sessions expired", "count", n)
}
```

- [ ] **Step 4: Run authentication and event tests**

Run:

```bash
gofmt -w internal/server/authapi.go internal/server/authapi_test.go internal/server/events.go
go test ./internal/server -run 'TestSetupThenLoginFlow|TestMintBearerToken|TestHub|TestUnsubscribe' -count=1
```

Expected: all selected tests pass.

- [ ] **Step 5: Commit authentication and maintenance events**

```bash
git add internal/server/authapi.go internal/server/authapi_test.go internal/server/events.go
git commit -m "feat: log authentication and maintenance events"
```

### Task 5: Full Validation and Design Review

**Files:**

- Review: `docs/superpowers/specs/2026-07-18-operational-logging-design.md`
- Review: all files modified in Tasks 1–4

- [ ] **Step 1: Run the entire server package**

Run:

```bash
go test ./internal/server -count=1
```

Expected: PASS, with environment-dependent tmux tests either passing or
reporting a documented skip.

- [ ] **Step 2: Run repository validation**

Run:

```bash
./verify.sh
```

Expected: formatting, Go tests, vet, frontend checks, and builds all pass with
no warnings or errors.

- [ ] **Step 3: Inspect the complete diff for privacy and scope**

Run:

```bash
git diff 4a8ed2f..HEAD -- internal/server
rg -n 'slog\\.' internal/server
git status --short
```

Expected: every domain attribute is explicitly selected; no request bodies,
commands, paths, setting values, tokens, credential IDs, session hashes,
user-agent strings, or query strings are supplied to a log call; the working
tree is clean.

- [ ] **Step 4: Commit validation-only fixes if required**

If validation required a source correction, stage only the corrected files
and commit them:

```bash
git add internal/server
git commit -m "fix: address operational logging validation"
```

If no correction was required, do not create an empty commit.
