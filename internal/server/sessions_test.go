package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jontuk/multimux/internal/panetext"
	"github.com/jontuk/multimux/internal/store"
	"github.com/jontuk/multimux/internal/tmuxmgr"
)

type stubPaneTextCapturer struct {
	mu    sync.Mutex
	text  []byte
	err   error
	name  string
	calls int
}

func (s *stubPaneTextCapturer) CapturePaneText(name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
	s.calls++
	return append([]byte(nil), s.text...), s.err
}

func (s *stubPaneTextCapturer) observation() (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name, s.calls
}

type stubPaneTextCleaner struct {
	mu                 sync.Mutex
	result             panetext.Result
	inputs             [][]byte
	started            chan struct{}
	startedOnce        sync.Once
	blockUntilCanceled bool
	contextErr         error
}

func (s *stubPaneTextCleaner) Clean(ctx context.Context, raw []byte) panetext.Result {
	s.mu.Lock()
	s.inputs = append(s.inputs, append([]byte(nil), raw...))
	s.mu.Unlock()
	if s.started != nil {
		s.startedOnce.Do(func() { close(s.started) })
	}
	if s.blockUntilCanceled {
		<-ctx.Done()
		s.mu.Lock()
		s.contextErr = ctx.Err()
		s.mu.Unlock()
	}
	return s.result
}

func (s *stubPaneTextCleaner) observation() ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inputs := make([][]byte, len(s.inputs))
	for i := range s.inputs {
		inputs[i] = append([]byte(nil), s.inputs[i]...)
	}
	return inputs, s.contextErr
}

func newPaneTextCleanTestServer(t *testing.T, capture *stubPaneTextCapturer, cleaner *stubPaneTextCleaner) (*Server, *store.Store, string) {
	t.Helper()
	cfg, st, am := newTestServerCfg(t, true)
	cfg.PaneText = capture
	cfg.PaneTextCleaner = cleaner
	token, err := am.CreateSession("UA")
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg), st, token
}

func newPaneTextTestServer(t *testing.T, text []byte, captureErr error) (*Server, *store.Store, *stubPaneTextCapturer, string) {
	t.Helper()
	cfg, st, am := newTestServerCfg(t, true)
	capture := &stubPaneTextCapturer{text: text, err: captureErr}
	cfg.PaneText = capture
	token, err := am.CreateSession("UA")
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg), st, capture, token
}

func runningSession(t *testing.T, st *store.Store) store.Session {
	t.Helper()
	tool, err := st.CreateTool("shell", "sh")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.CreateSession(tool.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestSessionTextSuccessUsesStoredTmuxName(t *testing.T) {
	s, st, capture, token := newPaneTextTestServer(t, []byte("oldest\nβ newest\n"), nil)
	sess := runningSession(t, st)
	w := do(t, s, "GET", fmt.Sprintf("/api/sessions/%d/text?tmuxName=attacker", sess.ID), token)
	if w.Code != http.StatusOK || w.Body.String() != "oldest\nβ newest\n" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if capture.name != sess.TmuxName {
		t.Fatalf("capture name = %q, want stored %q", capture.name, sess.TmuxName)
	}
}

func TestSessionTextAllowsEmptySnapshot(t *testing.T) {
	s, st, _, token := newPaneTextTestServer(t, nil, nil)
	sess := runningSession(t, st)
	w := do(t, s, "GET", fmt.Sprintf("/api/sessions/%d/text", sess.ID), token)
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("empty capture = %d %q", w.Code, w.Body.String())
	}
}

func TestSessionTextRequiresAuth(t *testing.T) {
	s, st, capture, _ := newPaneTextTestServer(t, []byte("secret"), nil)
	sess := runningSession(t, st)
	w := do(t, s, "GET", fmt.Sprintf("/api/sessions/%d/text", sess.ID), "")
	if w.Code != http.StatusUnauthorized || capture.name != "" {
		t.Fatalf("unauthenticated capture = %d, name %q", w.Code, capture.name)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unauthenticated Cache-Control = %q", got)
	}
}

func TestSessionTextHeadAuthFailureIsNotCached(t *testing.T) {
	s, st, capture, _ := newPaneTextTestServer(t, []byte("secret"), nil)
	sess := runningSession(t, st)
	w := do(t, s, "HEAD", fmt.Sprintf("/api/sessions/%d/text", sess.ID), "")
	if w.Code != http.StatusUnauthorized || capture.name != "" {
		t.Fatalf("unauthenticated HEAD capture = %d, name %q", w.Code, capture.name)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unauthenticated HEAD Cache-Control = %q", got)
	}
}

func TestSessionTextRejectsBadMissingAndEndedRows(t *testing.T) {
	s, st, capture, token := newPaneTextTestServer(t, []byte("unused"), nil)
	if w := do(t, s, "GET", "/api/sessions/not-a-number/text", token); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", w.Code)
	}
	if w := do(t, s, "GET", "/api/sessions/999/text", token); w.Code != http.StatusNotFound {
		t.Fatalf("missing id = %d", w.Code)
	}
	sess := runningSession(t, st)
	if err := st.SetSessionStatus(sess.ID, "dead"); err != nil {
		t.Fatal(err)
	}
	if w := do(t, s, "GET", fmt.Sprintf("/api/sessions/%d/text", sess.ID), token); w.Code != http.StatusConflict {
		t.Fatalf("ended session = %d", w.Code)
	}
	if capture.name != "" {
		t.Fatalf("capture called for rejected row: %q", capture.name)
	}
}

func TestSessionTextMapsTmuxDisappearanceToConflict(t *testing.T) {
	s, st, _, token := newPaneTextTestServer(t, nil, tmuxmgr.ErrSessionUnavailable)
	sess := runningSession(t, st)
	w := do(t, s, "GET", fmt.Sprintf("/api/sessions/%d/text", sess.ID), token)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "no longer available") {
		t.Fatalf("disappeared capture = %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("conflict Cache-Control = %q", got)
	}
}

func TestSessionTextInternalFailureIsContentSafe(t *testing.T) {
	s, st, _, token := newPaneTextTestServer(t, nil, errors.New("capture command failed"))
	sess := runningSession(t, st)
	logs := captureLogs(t)
	w := do(t, s, "GET", fmt.Sprintf("/api/sessions/%d/text", sess.ID), token)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "command failed") {
		t.Fatalf("internal capture = %d %q", w.Code, w.Body.String())
	}
	logged := logs.String()
	if !strings.Contains(logged, `"msg":"pane text capture failed"`) || !strings.Contains(logged, `"session_id":`) {
		t.Fatalf("diagnostic context missing: %s", logged)
	}
}

func TestSessionCleanTextSuccessUsesStoredTmuxNameAndReturnsMetadata(t *testing.T) {
	raw := []byte("wrapped pane\ntext\n")
	capture := &stubPaneTextCapturer{text: raw}
	cleaner := &stubPaneTextCleaner{result: panetext.Result{
		Text:      "wrapped pane text\n",
		Processor: "codex",
		Model:     "gpt-5.6-luna",
		Warning:   "cleanup note",
	}}
	s, st, token := newPaneTextCleanTestServer(t, capture, cleaner)
	sess := runningSession(t, st)

	w := do(t, s, http.MethodPost, fmt.Sprintf("/api/sessions/%d/text/clean?tmuxName=attacker", sess.ID), token)

	if w.Code != http.StatusOK {
		t.Fatalf("response status = %d, body %q", w.Code, w.Body.String())
	}
	if got, want := w.Body.String(), `{"text":"wrapped pane text\n","processor":"codex","model":"gpt-5.6-luna","warning":"cleanup note"}`+"\n"; got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	name, calls := capture.observation()
	if name != sess.TmuxName || calls != 1 {
		t.Fatalf("capture = (%q, %d calls), want (%q, 1 call)", name, calls, sess.TmuxName)
	}
	inputs, _ := cleaner.observation()
	if len(inputs) != 1 || !bytes.Equal(inputs[0], raw) {
		t.Fatalf("cleaner inputs = %q, want one exact input %q", inputs, raw)
	}
}

func TestSessionCleanTextRawFallbackIsStillSuccessfulJSON(t *testing.T) {
	raw := []byte("unaltered\npane\n")
	capture := &stubPaneTextCapturer{text: raw}
	cleaner := &stubPaneTextCleaner{result: panetext.Result{
		Text:      string(raw),
		Processor: "raw",
		Warning:   "Automatic cleanup unavailable. Showing raw pane text.",
	}}
	s, st, token := newPaneTextCleanTestServer(t, capture, cleaner)
	sess := runningSession(t, st)

	w := do(t, s, http.MethodPost, fmt.Sprintf("/api/sessions/%d/text/clean", sess.ID), token)

	if w.Code != http.StatusOK {
		t.Fatalf("response status = %d, body %q", w.Code, w.Body.String())
	}
	if got, want := w.Body.String(), `{"text":"unaltered\npane\n","processor":"raw","model":"","warning":"Automatic cleanup unavailable. Showing raw pane text."}`+"\n"; got != want {
		t.Fatalf("response body = %q, want %q", got, want)
	}
}

func TestSessionCleanTextRequiresAuthWithoutDoingWork(t *testing.T) {
	capture := &stubPaneTextCapturer{text: []byte("secret")}
	cleaner := &stubPaneTextCleaner{result: panetext.Result{Text: "must not run"}}
	s, st, _ := newPaneTextCleanTestServer(t, capture, cleaner)
	sess := runningSession(t, st)

	w := do(t, s, http.MethodPost, fmt.Sprintf("/api/sessions/%d/text/clean", sess.ID), "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unauthenticated Cache-Control = %q", got)
	}
	if _, calls := capture.observation(); calls != 0 {
		t.Fatalf("unauthenticated capture calls = %d", calls)
	}
	if inputs, _ := cleaner.observation(); len(inputs) != 0 {
		t.Fatalf("unauthenticated cleaner inputs = %q", inputs)
	}
}

func TestSessionCleanTextRejectsInvalidOrUnavailableSessionsBeforeCleaning(t *testing.T) {
	capture := &stubPaneTextCapturer{text: []byte("unused")}
	cleaner := &stubPaneTextCleaner{result: panetext.Result{Text: "must not run"}}
	s, st, token := newPaneTextCleanTestServer(t, capture, cleaner)

	for _, tc := range []struct {
		name string
		path string
		code int
		body string
	}{
		{name: "malformed id", path: "/api/sessions/not-a-number/text/clean", code: http.StatusBadRequest, body: `{"error":"bad id"}` + "\n"},
		{name: "missing session", path: "/api/sessions/999/text/clean", code: http.StatusNotFound, body: `{"error":"not found"}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, s, http.MethodPost, tc.path, token)
			if w.Code != tc.code || w.Body.String() != tc.body {
				t.Fatalf("response = %d %q, want %d %q", w.Code, w.Body.String(), tc.code, tc.body)
			}
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
		})
	}

	sess := runningSession(t, st)
	if err := st.SetSessionStatus(sess.ID, "dead"); err != nil {
		t.Fatal(err)
	}
	w := do(t, s, http.MethodPost, fmt.Sprintf("/api/sessions/%d/text/clean", sess.ID), token)
	if got, want := w.Body.String(), `{"error":"session is no longer available"}`+"\n"; w.Code != http.StatusConflict || got != want {
		t.Fatalf("ended response = %d %q, want 409 %q", w.Code, got, want)
	}
	if _, calls := capture.observation(); calls != 0 {
		t.Fatalf("capture calls for rejected rows = %d", calls)
	}
	if inputs, _ := cleaner.observation(); len(inputs) != 0 {
		t.Fatalf("cleaner inputs for rejected rows = %q", inputs)
	}
}

func TestSessionCleanTextMapsStoreFailureWithoutDoingWork(t *testing.T) {
	capture := &stubPaneTextCapturer{text: []byte("unused")}
	cleaner := &stubPaneTextCleaner{result: panetext.Result{Text: "must not run"}}
	s, st, _ := newPaneTextCleanTestServer(t, capture, cleaner)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/sessions/1/text/clean", nil)
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.handleSessionCleanText(w, r)

	if got, want := w.Body.String(), `{"error":"could not load session"}`+"\n"; w.Code != http.StatusInternalServerError || got != want {
		t.Fatalf("response = %d %q, want 500 %q", w.Code, got, want)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if _, calls := capture.observation(); calls != 0 {
		t.Fatalf("capture calls after store failure = %d", calls)
	}
	if inputs, _ := cleaner.observation(); len(inputs) != 0 {
		t.Fatalf("cleaner inputs after store failure = %q", inputs)
	}
}

func TestSessionCleanTextMapsCaptureFailuresWithoutCleaning(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
		body string
	}{
		{name: "tmux disappeared", err: tmuxmgr.ErrSessionUnavailable, code: http.StatusConflict, body: `{"error":"session is no longer available"}` + "\n"},
		{name: "capture failed", err: errors.New("capture command failed"), code: http.StatusInternalServerError, body: `{"error":"could not capture pane text"}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &stubPaneTextCapturer{err: tc.err}
			cleaner := &stubPaneTextCleaner{result: panetext.Result{Text: "must not run"}}
			s, st, token := newPaneTextCleanTestServer(t, capture, cleaner)
			sess := runningSession(t, st)

			w := do(t, s, http.MethodPost, fmt.Sprintf("/api/sessions/%d/text/clean", sess.ID), token)

			if w.Code != tc.code || w.Body.String() != tc.body {
				t.Fatalf("response = %d %q, want %d %q", w.Code, w.Body.String(), tc.code, tc.body)
			}
			if _, calls := capture.observation(); calls != 1 {
				t.Fatalf("capture calls = %d, want 1", calls)
			}
			if inputs, _ := cleaner.observation(); len(inputs) != 0 {
				t.Fatalf("cleaner inputs after capture failure = %q", inputs)
			}
		})
	}
}

func TestSessionCleanTextPropagatesCancellationAndReturnsPromptly(t *testing.T) {
	capture := &stubPaneTextCapturer{text: []byte("pane text")}
	cleaner := &stubPaneTextCleaner{
		result:             panetext.Result{Text: "fallback", Processor: "raw"},
		started:            make(chan struct{}),
		blockUntilCanceled: true,
	}
	s, st, token := newPaneTextCleanTestServer(t, capture, cleaner)
	sess := runningSession(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/sessions/%d/text/clean", sess.ID), nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.Handler().ServeHTTP(w, r)
		close(done)
	}()

	select {
	case <-cleaner.started:
	case <-time.After(time.Second):
		t.Fatal("cleaner did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after request cancellation")
	}
	inputs, contextErr := cleaner.observation()
	if len(inputs) != 1 || !bytes.Equal(inputs[0], []byte("pane text")) {
		t.Fatalf("cleaner inputs = %q", inputs)
	}
	if !errors.Is(contextErr, context.Canceled) {
		t.Fatalf("cleaner context error = %v, want context.Canceled", contextErr)
	}
}

// newTmuxTestServer swaps in a Manager on an isolated tmux socket.
func newTmuxTestServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	s, st, am := newTestServer(t, true)
	socket := fmt.Sprintf("mmtest-%d", time.Now().UnixNano())
	s.cfg.Tmux = tmuxmgr.New("mm", socket)
	t.Cleanup(func() { exec.Command("tmux", "-L", socket, "kill-server").Run() })
	token, _ := am.CreateSession("UA")
	return s, st, token
}

func TestSessionCreateKillDismiss(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	dir, _ := st.CreateDir("tmp", t.TempDir())
	buf := captureLogs(t)

	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	if w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	sess := onlySession(t, w)
	if !s.cfg.Tmux.IsAlive(sess.TmuxName) {
		t.Fatal("tmux session not created")
	}

	// Dismiss while running → 409.
	if w = do(t, s, "POST", fmt.Sprintf("/api/sessions/%d/dismiss", sess.ID), token); w.Code != 409 {
		t.Fatalf("dismiss running = %d, want 409", w.Code)
	}

	// Kill → dead row kept.
	if w = do(t, s, "DELETE", fmt.Sprintf("/api/sessions/%d", sess.ID), token); w.Code != 204 {
		t.Fatalf("kill = %d", w.Code)
	}
	got, _ := st.GetSession(sess.ID)
	if got.Status != "dead" {
		t.Fatalf("status = %s, want dead", got.Status)
	}
	if s.cfg.Tmux.IsAlive(sess.TmuxName) {
		t.Fatal("tmux session survived kill")
	}

	// Dismiss now works.
	if w = do(t, s, "POST", fmt.Sprintf("/api/sessions/%d/dismiss", sess.ID), token); w.Code != 204 {
		t.Fatalf("dismiss dead = %d", w.Code)
	}

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
}

func TestCreateSessionSubdir(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "web", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, _ := st.CreateDir("tmp", base)

	w := do(t, s, "POST", "/api/sessions", token,
		fmt.Sprintf(`{"toolId":%d,"dirId":%d,"subdir":"web/src"}`, tool.ID, dir.ID))
	if w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	sess := onlySession(t, w)
	// EvalSymlinks resolves the temp dir (/var → /private/var on macOS), so
	// compare against the resolved base rather than the raw one.
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(realBase, "web", "src"); sess.Dir != want {
		t.Fatalf("session dir = %q, want %q", sess.Dir, want)
	}
	if !s.cfg.Tmux.IsAlive(sess.TmuxName) {
		t.Fatal("tmux session not created")
	}
}

// The configured dirs are the whole allow-list of launch locations: a subdir
// must not reach outside one, and must not create anything.
func TestCreateSessionSubdirRejected(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Fatal(err)
	}
	dir, _ := st.CreateDir("tmp", base)

	for _, subdir := range []string{"..", "../..", "web/../..", "escape", "/etc", "missing"} {
		body := fmt.Sprintf(`{"toolId":%d,"dirId":%d,"subdir":%q}`, tool.ID, dir.ID, subdir)
		if w := do(t, s, "POST", "/api/sessions", token, body); w.Code != 400 {
			t.Fatalf("subdir %q = %d, want 400: %s", subdir, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(base, "missing")); !os.IsNotExist(err) {
		t.Fatalf("a rejected launch created the subdirectory: %v", err)
	}
	sessions, _ := st.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("rejected launches left rows: %+v", sessions)
	}
}

// A tmux session bearing the name the next row will get, but with no DB row
// (left over from a wiped DB or a failed kill), is unreachable from the UI.
// Create must replace it instead of failing with "duplicate session".
func TestCreateSessionReplacesOrphanTmuxSession(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	dir, _ := st.CreateDir("tmp", t.TempDir())

	orphan := s.cfg.Tmux.SessionName(1) // fresh DB: the next session row gets ID 1
	if err := s.cfg.Tmux.CreateSession(orphan, t.TempDir(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	buf := captureLogs(t)

	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	if w.Code != 201 {
		t.Fatalf("create with orphan = %d: %s", w.Code, w.Body.String())
	}
	sess := onlySession(t, w)
	if sess.TmuxName != orphan {
		t.Fatalf("tmux name = %q, want %q", sess.TmuxName, orphan)
	}
	if !s.cfg.Tmux.IsAlive(sess.TmuxName) {
		t.Fatal("tmux session not created")
	}
	logged := buf.String()
	if !strings.Contains(logged, `"msg":"orphan tmux session replaced"`) ||
		!strings.Contains(logged, `"tmux_name":"`+orphan+`"`) {
		t.Fatalf("orphan replacement not logged safely: %s", logged)
	}
}

func TestFailedCreateDoesNotClaimOrphanWasReplaced(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("oversized", strings.Repeat("x", 2<<20))
	dir, _ := st.CreateDir("tmp", t.TempDir())

	orphan := s.cfg.Tmux.SessionName(1)
	if err := s.cfg.Tmux.CreateSession(orphan, t.TempDir(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	buf := captureLogs(t)

	w := do(t, s, "POST", "/api/sessions", token,
		fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("create with oversized command = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(buf.String(), `"msg":"orphan tmux session replaced"`) {
		t.Fatalf("failed creation claimed orphan replacement: %s", buf.String())
	}
}

func TestCreateSessionBadTool(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "POST", "/api/sessions", token, `{"toolId":999,"dirId":999}`); w.Code != 400 {
		t.Fatalf("bad refs = %d, want 400", w.Code)
	}
}

func TestReconcileMarksDead(t *testing.T) {
	s, st, _ := newTmuxTestServer(t)
	s.reconcileGrace = 0 // the row below is brand new; skip the create-race grace
	tool, _ := st.CreateTool("sh", "sleep 60")
	// DB row without a live tmux session (simulates daemon restart after reboot).
	sess, _ := st.CreateSession(tool.ID, "/tmp")
	buf := captureLogs(t)
	dead, err := s.Reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 1 || dead[0].ID != sess.ID {
		t.Fatalf("dead = %+v", dead)
	}
	got, _ := st.GetSession(sess.ID)
	if got.Status != "dead" {
		t.Fatalf("status = %s", got.Status)
	}
	logged := buf.String()
	if !strings.Contains(logged, `"msg":"session died"`) ||
		!strings.Contains(logged, fmt.Sprintf(`"session_id":%d`, sess.ID)) {
		t.Fatalf("reconciled death not logged: %s", logged)
	}
}

// A freshly-inserted row whose tmux session does not exist yet (creation in
// flight) must survive a reconcile tick.
func TestReconcileSparesFreshSessions(t *testing.T) {
	s, st, _ := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	sess, _ := st.CreateSession(tool.ID, "/tmp")
	buf := captureLogs(t)
	dead, err := s.Reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 0 {
		t.Fatalf("dead = %+v, want none within the grace period", dead)
	}
	got, _ := st.GetSession(sess.ID)
	if got.Status != "running" {
		t.Fatalf("status = %s, want running", got.Status)
	}
	if strings.Contains(buf.String(), `"msg":"session died"`) {
		t.Fatalf("no-op reconcile logged a death: %s", buf.String())
	}
}

// fakeTmux puts a shell script named tmux first on PATH and returns the file
// its invocations are logged to (one line of args per call).
func fakeTmux(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	log := dir + "/calls.log"
	body := "#!/bin/sh\necho \"$@\" >> \"" + log + "\"\n" + script
	if err := os.WriteFile(dir+"/tmux", []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return log
}

// One reconcile pass must issue a single tmux listing — not one has-session
// per row — and only sessions confirmed absent from it may be marked dead.
func TestReconcileUsesOneListing(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	s.cfg.Tmux = tmuxmgr.New("mm", "unused")
	s.reconcileGrace = 0
	tool, _ := st.CreateTool("sh", "sleep 60")
	a, _ := st.CreateSession(tool.ID, "/tmp") // mm-1: absent → dead
	b, _ := st.CreateSession(tool.ID, "/tmp") // mm-2: listed → stays running
	c, _ := st.CreateSession(tool.ID, "/tmp") // mm-3: absent → dead

	log := fakeTmux(t, "echo mm-2\n")
	dead, err := s.Reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 2 || dead[0].ID != a.ID || dead[1].ID != c.ID {
		t.Fatalf("dead = %+v, want sessions %d and %d", dead, a.ID, c.ID)
	}
	if got, _ := st.GetSession(b.ID); got.Status != "running" {
		t.Fatalf("listed session status = %s, want running", got.Status)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "list-sessions") {
		t.Fatalf("tmux calls = %q, want exactly one list-sessions", lines)
	}
}

// A tmux command error (as opposed to "no server running") confirms nothing:
// live rows must survive the pass untouched.
func TestReconcileSparesLiveRowsOnTmuxError(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	s.cfg.Tmux = tmuxmgr.New("mm", "unused")
	s.reconcileGrace = 0
	tool, _ := st.CreateTool("sh", "sleep 60")
	sess, _ := st.CreateSession(tool.ID, "/tmp")

	fakeTmux(t, "echo 'lost server' >&2\nexit 2\n")
	dead, err := s.Reconcile()
	if err == nil {
		t.Fatal("Reconcile() = nil error, want the tmux failure surfaced")
	}
	if len(dead) != 0 {
		t.Fatalf("dead = %+v, want none on a failed listing", dead)
	}
	if got, _ := st.GetSession(sess.ID); got.Status != "running" {
		t.Fatalf("status = %s, want running after transient tmux error", got.Status)
	}
}

// A kill that fails for a real reason (not "already gone") must preserve the
// running row and surface the error — marking it dead would orphan a live
// tmux session with no UI handle to it.
func TestKillFailurePreservesRunningRow(t *testing.T) {
	s, st, token := func() (*Server, *store.Store, string) {
		s, st, am := newTestServer(t, true)
		s.cfg.Tmux = tmuxmgr.New("mm", "unused")
		token, _ := am.CreateSession("UA")
		return s, st, token
	}()
	tool, _ := st.CreateTool("sh", "sleep 60")
	sess, _ := st.CreateSession(tool.ID, "/tmp")

	fakeTmux(t, "echo 'lost server' >&2\nexit 2\n")
	if w := do(t, s, "DELETE", fmt.Sprintf("/api/sessions/%d", sess.ID), token); w.Code != 500 {
		t.Fatalf("kill with failing tmux = %d, want 500", w.Code)
	}
	if got, _ := st.GetSession(sess.ID); got.Status != "running" {
		t.Fatalf("status = %s, want running preserved on kill failure", got.Status)
	}
}

// Killing a session whose tmux server is gone (reboot) still succeeds: the
// session is already absent, so the row is marked dead and 204 returned.
func TestKillAfterRebootMarksDead(t *testing.T) {
	s, st, am := newTestServer(t, true)
	s.cfg.Tmux = tmuxmgr.New("mm", "unused")
	token, _ := am.CreateSession("UA")
	tool, _ := st.CreateTool("sh", "sleep 60")
	sess, _ := st.CreateSession(tool.ID, "/tmp")

	fakeTmux(t, "echo 'no server running on /tmp/x' >&2\nexit 1\n")
	if w := do(t, s, "DELETE", fmt.Sprintf("/api/sessions/%d", sess.ID), token); w.Code != 204 {
		t.Fatalf("kill after reboot = %d, want 204", w.Code)
	}
	if got, _ := st.GetSession(sess.ID); got.Status != "dead" {
		t.Fatalf("status = %s, want dead", got.Status)
	}
}

func TestLayoutAPI(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "GET", "/api/layout", token); w.Code != 200 || w.Body.String() != "{}\n" && w.Body.String() != "{}" {
		t.Fatalf("empty layout = %d %q", w.Code, w.Body.String())
	}
	doc := `{"shape":{"rows":2,"cols":2},"tiles":[null,null,null,null]}`
	buf := captureLogs(t)
	if w := do(t, s, "PUT", "/api/layout", token, doc); w.Code != 204 {
		t.Fatalf("put layout = %d", w.Code)
	}
	if logged := buf.String(); !strings.Contains(logged, `"msg":"layout changed"`) {
		t.Fatalf("layout change not logged: %s", logged)
	}
	w := do(t, s, "GET", "/api/layout", token)
	var got, want map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	json.Unmarshal([]byte(doc), &want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("layout = %v", got)
	}
}

func TestLayoutRejectsNonJSON(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "PUT", "/api/layout", token, "{not json"); w.Code != 400 {
		t.Fatalf("put garbage layout = %d, want 400", w.Code)
	}
}

func TestListSessionsIncludesRepoURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	s, st, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	repo := t.TempDir()
	for _, args := range [][]string{
		{"-C", repo, "init"},
		{"-C", repo, "remote", "add", "origin", "git@github.com:org/repo.git"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	tool, _ := st.CreateTool("sh", "sleep 60")
	st.CreateSession(tool.ID, repo)
	st.CreateSession(tool.ID, t.TempDir()) // no repo → no repoUrl

	w := do(t, s, "GET", "/api/sessions", token)
	if w.Code != 200 {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
	var got []struct {
		Dir     string `json:"dir"`
		RepoURL string `json:"repoUrl"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].RepoURL != "https://github.com/org/repo" {
		t.Errorf("repo session repoUrl = %q", got[0].RepoURL)
	}
	if got[1].RepoURL != "" {
		t.Errorf("non-repo session repoUrl = %q", got[1].RepoURL)
	}
}

func TestListSessionsIncludesBranchAndGitState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	s, st, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	repo := t.TempDir()
	for _, args := range [][]string{
		{"-C", repo, "init"},
		{"-C", repo, "checkout", "-b", "feat"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// A commit with no remote makes the branch never-pushed; a.txt stays
	// untracked on top of it.
	for _, args := range [][]string{
		{"-C", repo, "-c", "user.email=t@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(repo+"/a.txt", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool, _ := st.CreateTool("sh", "sleep 60")
	st.CreateSession(tool.ID, repo)
	st.CreateSession(tool.ID, t.TempDir()) // no repo → no branch/state

	w := do(t, s, "GET", "/api/sessions", token)
	if w.Code != 200 {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
	var got []struct {
		Branch     string `json:"branch"`
		GitState   string `json:"gitState"`
		NoUpstream bool   `json:"noUpstream"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Branch != "feat" || got[0].GitState != "untracked" {
		t.Errorf("repo session = (%q, %q), want (feat, untracked)", got[0].Branch, got[0].GitState)
	}
	if !got[0].NoUpstream {
		t.Error("repo session noUpstream = false, want true (committed, no remote)")
	}
	if got[1].Branch != "" || got[1].GitState != "" || got[1].NoUpstream {
		t.Errorf("non-repo session = (%q, %q, %v), want empty", got[1].Branch, got[1].GitState, got[1].NoUpstream)
	}
}

func TestCheckGitInfoBroadcastsOnChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	s, st, _ := newTestServer(t, true)

	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	tool, _ := st.CreateTool("sh", "sleep 60")
	st.CreateSession(tool.ID, repo)

	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)
	drain := func() []string {
		var events []string
		for {
			select {
			case raw := <-ch:
				var ev struct {
					Type string `json:"type"`
				}
				json.Unmarshal(raw, &ev)
				events = append(events, ev.Type)
			default:
				return events
			}
		}
	}

	// Baseline tick: establishes state, must not broadcast.
	if err := s.CheckGitInfo(); err != nil {
		t.Fatal(err)
	}
	if evs := drain(); len(evs) != 0 {
		t.Fatalf("baseline tick broadcast %v, want none", evs)
	}

	// No change → still no broadcast.
	if err := s.CheckGitInfo(); err != nil {
		t.Fatal(err)
	}
	if evs := drain(); len(evs) != 0 {
		t.Fatalf("unchanged tick broadcast %v, want none", evs)
	}

	// New untracked file → one git_changed.
	if err := os.WriteFile(repo+"/a.txt", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckGitInfo(); err != nil {
		t.Fatal(err)
	}
	if evs := drain(); len(evs) != 1 || evs[0] != "git_changed" {
		t.Fatalf("changed tick broadcast %v, want [git_changed]", evs)
	}
}

func TestSessionRename(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	dir, _ := st.CreateDir("tmp", t.TempDir())
	buf := captureLogs(t)

	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	if w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	sess := onlySession(t, w)
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
	sess := onlySession(t, w)
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
	//  is BEL: valid JSON, invalid label.
	if w = do(t, s, "PUT", path, token, `{"label":"badbell"}`); w.Code != 400 {
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
	sess := onlySession(t, w)

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

// A tool whose command carries the group separator launches one session per
// command, each labelled with the program it runs — sessions record a tool,
// not a command, so without the label every tile in the group reads the same.
func TestSessionGroupLaunch(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("dev", "sleep 60 ;; cat")
	dir, _ := st.CreateDir("tmp", t.TempDir())

	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	if w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	var sessions []store.Session
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2: %+v", len(sessions), sessions)
	}
	wantLabels := []string{"sleep", "cat"}
	for i, sess := range sessions {
		if sess.Label != wantLabels[i] {
			t.Fatalf("session %d label = %q, want %q", i, sess.Label, wantLabels[i])
		}
		if !s.cfg.Tmux.IsAlive(sess.TmuxName) {
			t.Fatalf("tmux session %s not created", sess.TmuxName)
		}
	}
	if sessions[0].ID == sessions[1].ID {
		t.Fatal("group returned the same session twice")
	}
	rows, _ := st.ListSessions()
	if len(rows) != 2 {
		t.Fatalf("stored %d rows, want 2", len(rows))
	}
}

// An ordinary tool is still one session, and it keeps an empty label so the
// user's own name for it is the only one a tile ever shows.
func TestSessionSingleLaunchIsUnlabelled(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	dir, _ := st.CreateDir("tmp", t.TempDir())

	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	if w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	var sessions []store.Session
	json.Unmarshal(w.Body.Bytes(), &sessions)
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].Label != "" {
		t.Fatalf("label = %q, want empty for a single-command tool", sessions[0].Label)
	}
}

// Each session of a group must run its own command, not the group's text.
func TestSessionGroupRunsEachCommand(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	workdir := t.TempDir()
	tool, _ := st.CreateTool("dev", "touch one; sleep 60 ;; touch two; sleep 60")
	dir, _ := st.CreateDir("tmp", workdir)

	if w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID)); w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, name := range []string{"one", "two"} {
		for {
			if _, err := os.Stat(filepath.Join(workdir, name)); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s never created: command did not run in its own session", name)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// A group is all-or-nothing: a session that fails halfway through takes the
// ones already started with it, so the user never has to clean up half a
// group by hand.
func TestSessionGroupRollsBackOnFailure(t *testing.T) {
	s, st, am := newTestServer(t, true)
	s.cfg.Tmux = tmuxmgr.New("mm", "unused")
	token, _ := am.CreateSession("UA")
	tool, _ := st.CreateTool("dev", "sleep 60 ;; sleep 61")
	dir, _ := st.CreateDir("tmp", t.TempDir())

	log := fakeTmux(t, `case "$*" in
  *"new-session -d -s mm-2"*) echo 'boom' >&2; exit 2 ;;
  *has-session*) exit 1 ;;
  *) exit 0 ;;
esac
`)
	if w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID)); w.Code != 500 {
		t.Fatalf("create with failing tmux = %d, want 500", w.Code)
	}
	rows, _ := st.ListSessions()
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none after a rolled-back group", rows)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "kill-session") {
		t.Fatalf("the session that did start was not killed; tmux calls:\n%s", calls)
	}
}

// onlySession decodes a launch that was expected to start exactly one session.
// Every launch answers with a list, because a tool may be a group.
func onlySession(t *testing.T, w *httptest.ResponseRecorder) store.Session {
	t.Helper()
	var sessions []store.Session
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode sessions: %v (body %s)", err, w.Body.String())
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1: %+v", len(sessions), sessions)
	}
	return sessions[0]
}
