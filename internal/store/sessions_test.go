package store

import (
	"errors"
	"path/filepath"
	"testing"
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
