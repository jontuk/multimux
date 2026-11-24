package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	s := openTestStore(t)
	tool, _ := s.CreateTool("zsh", "zsh")
	sess, err := s.CreateSession(tool.ID, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	wantName := "mm-1"
	if sess.TmuxName != wantName || sess.Status != "running" {
		t.Fatalf("session = %+v, want name %s status running", sess, wantName)
	}
	got, err := s.GetSession(sess.ID)
	if err != nil || got.TmuxName != wantName || got.CreatedAt.IsZero() {
		t.Fatalf("GetSession = %+v, %v", got, err)
	}
	if err := s.SetSessionStatus(sess.ID, "dead"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListSessions()
	if len(list) != 1 || list[0].Status != "dead" {
		t.Fatalf("list = %+v", list)
	}
	if err := s.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession after delete = %v, want ErrNotFound", err)
	}
}

func TestLayoutRoundTrip(t *testing.T) {
	s := openTestStore(t)
	if v, err := s.GetLayout(); err != nil || v != "" {
		t.Fatalf("empty layout = %q, %v", v, err)
	}
	doc := `{"shape":{"rows":2,"cols":2},"tiles":[null,null,null,null]}`
	if err := s.SetLayout(doc); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLayout(doc); err != nil { // upsert
		t.Fatal(err)
	}
	if v, _ := s.GetLayout(); v != doc {
		t.Fatalf("layout = %q", v)
	}
}

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

func TestListRunningSessionsReturnsOnlyRunningRows(t *testing.T) {
	s := openTestStore(t)
	tool, _ := s.CreateTool("zsh", "zsh")
	if list, err := s.ListRunningSessions(); err != nil || len(list) != 0 {
		t.Fatalf("empty table = %+v, %v", list, err)
	}
	var ids []int64
	for range 4 {
		sess, err := s.CreateSession(tool.ID, "/tmp")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sess.ID)
	}
	if err := s.SetSessionLabel(ids[2], "kept"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{ids[0], ids[3]} {
		if err := s.SetSessionStatus(id, "dead"); err != nil {
			t.Fatal(err)
		}
	}
	running, err := s.ListRunningSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 2 || running[0].ID != ids[1] || running[1].ID != ids[2] {
		t.Fatalf("running = %+v, want ids %d then %d", running, ids[1], ids[2])
	}
	// Same rows, fully populated, as the unfiltered listing gives.
	all, _ := s.ListSessions()
	if len(all) != 4 || running[0] != all[1] || running[1] != all[2] {
		t.Fatalf("running = %+v, all = %+v", running, all)
	}
	// A row that goes back to running is listed again.
	if err := s.SetSessionStatus(ids[0], "running"); err != nil {
		t.Fatal(err)
	}
	if running, _ = s.ListRunningSessions(); len(running) != 3 || running[0].ID != ids[0] {
		t.Fatalf("running after revive = %+v", running)
	}
}

// endedAt reads a session's raw ended_at, "" for NULL.
func endedAt(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var v *string
	if err := s.db.QueryRow(`SELECT ended_at FROM sessions WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func TestSetSessionStatusTracksEndedAt(t *testing.T) {
	s := openTestStore(t)
	tool, _ := s.CreateTool("zsh", "zsh")
	sess, err := s.CreateSession(tool.ID, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if got := endedAt(t, s, sess.ID); got != "" {
		t.Fatalf("running session ended_at = %q, want NULL", got)
	}
	before := time.Now().UTC().Truncate(time.Second)
	if err := s.SetSessionStatus(sess.ID, "dead"); err != nil {
		t.Fatal(err)
	}
	stamped, err := time.Parse(time.RFC3339, endedAt(t, s, sess.ID))
	if err != nil || stamped.Before(before) || stamped.Location() != time.UTC {
		t.Fatalf("ended_at = %v (%v), want RFC3339 UTC at or after %v", stamped, err, before)
	}

	// Marking a dead row dead again must not restart its retention clock.
	const earlier = "2020-01-01T00:00:00Z"
	if _, err := s.db.Exec(`UPDATE sessions SET ended_at = ? WHERE id = ?`, earlier, sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionStatus(sess.ID, "dead"); err != nil {
		t.Fatal(err)
	}
	if got := endedAt(t, s, sess.ID); got != earlier {
		t.Fatalf("ended_at after a repeated dead = %q, want %q kept", got, earlier)
	}

	if err := s.SetSessionStatus(sess.ID, "running"); err != nil {
		t.Fatal(err)
	}
	if got := endedAt(t, s, sess.ID); got != "" {
		t.Fatalf("revived session ended_at = %q, want NULL", got)
	}
}

func TestDeleteDeadSessionsBefore(t *testing.T) {
	s := openTestStore(t)
	tool, _ := s.CreateTool("zsh", "zsh")
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-time.Hour).Format(time.RFC3339)
	cutoff := now.Add(-7 * 24 * time.Hour)

	// status, created_at, ended_at ("" for NULL), and whether the sweep takes it.
	rows := []struct {
		name, status, created, ended string
		swept                        bool
	}{
		{"dead long ago", "dead", old, old, true},
		{"old session that died recently", "dead", old, recent, false},
		{"dead before ended_at existed, old", "dead", old, "", true},
		{"dead before ended_at existed, recent", "dead", recent, "", false},
		{"running, however old", "running", old, "", false},
		// Not a state the store produces, but running must win regardless.
		{"running with a stale ended_at", "running", old, old, false},
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		sess, err := s.CreateSession(tool.ID, "/tmp")
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = sess.ID
		var ended any
		if r.ended != "" {
			ended = r.ended
		}
		if _, err := s.db.Exec(`UPDATE sessions SET status = ?, created_at = ?, ended_at = ?, label = ? WHERE id = ?`,
			r.status, r.created, ended, r.name, sess.ID); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := s.DeleteDeadSessionsBefore(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	gone := map[int64]Session{}
	for _, sess := range deleted {
		gone[sess.ID] = sess
	}
	left := map[int64]bool{}
	all, _ := s.ListSessions()
	for _, sess := range all {
		left[sess.ID] = true
	}
	for i, r := range rows {
		got, returned := gone[ids[i]]
		if returned != r.swept || left[ids[i]] == r.swept {
			t.Errorf("%s: returned as deleted = %v, still stored = %v, want swept = %v",
				r.name, returned, left[ids[i]], r.swept)
		}
		// The caller broadcasts what it gets back, so it must be the whole row.
		if returned && (got.TmuxName == "" || got.Label != r.name || got.Status != "dead") {
			t.Errorf("%s: deleted row came back as %+v", r.name, got)
		}
	}
	if len(deleted) != 2 {
		t.Errorf("deleted %d rows, want 2", len(deleted))
	}

	if again, err := s.DeleteDeadSessionsBefore(cutoff); err != nil || len(again) != 0 {
		t.Fatalf("second sweep = %+v, %v, want nothing", again, err)
	}
}
