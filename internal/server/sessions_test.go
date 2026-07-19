package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jontuk/multimux/internal/store"
	"github.com/jontuk/multimux/internal/tmuxmgr"
)

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
	var sess store.Session
	json.Unmarshal(w.Body.Bytes(), &sess)
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
	var sess store.Session
	json.Unmarshal(w.Body.Bytes(), &sess)
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
		Branch   string `json:"branch"`
		GitState string `json:"gitState"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Branch != "feat" || got[0].GitState != "untracked" {
		t.Errorf("repo session = (%q, %q), want (feat, untracked)", got[0].Branch, got[0].GitState)
	}
	if got[1].Branch != "" || got[1].GitState != "" {
		t.Errorf("non-repo session = (%q, %q), want empty", got[1].Branch, got[1].GitState)
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
