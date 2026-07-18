package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
}

// A freshly-inserted row whose tmux session does not exist yet (creation in
// flight) must survive a reconcile tick.
func TestReconcileSparesFreshSessions(t *testing.T) {
	s, st, _ := newTmuxTestServer(t)
	tool, _ := st.CreateTool("sh", "sleep 60")
	sess, _ := st.CreateSession(tool.ID, "/tmp")
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
}

func TestLayoutAPI(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "GET", "/api/layout", token); w.Code != 200 || w.Body.String() != "{}\n" && w.Body.String() != "{}" {
		t.Fatalf("empty layout = %d %q", w.Code, w.Body.String())
	}
	doc := `{"shape":{"rows":2,"cols":2},"tiles":[null,null,null,null]}`
	if w := do(t, s, "PUT", "/api/layout", token, doc); w.Code != 204 {
		t.Fatalf("put layout = %d", w.Code)
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
