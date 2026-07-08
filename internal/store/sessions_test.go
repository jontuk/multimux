package store

import (
	"errors"
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
