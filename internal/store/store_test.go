package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSettingsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	if v, err := s.GetSetting("hostname"); err != nil || v != "" {
		t.Fatalf("unset setting = %q, %v; want empty, nil", v, err)
	}
	if err := s.SetSetting("hostname", "mybox"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("hostname", "mybox2"); err != nil { // upsert
		t.Fatal(err)
	}
	if v, _ := s.GetSetting("hostname"); v != "mybox2" {
		t.Fatalf("got %q, want mybox2", v)
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open with missing parent: %v", err)
	}
	s.Close()
}
