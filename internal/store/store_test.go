package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
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

// rawOpen opens path with the same driver/DSN as Open but without migrating,
// so tests can inspect or tamper with schema bookkeeping directly.
func rawOpen(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatalf("look up table %s: %v", name, err)
	}
	return n > 0
}

// A database written by a newer multimux must be refused, not opened blind.
func TestOpenRejectsFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	future := len(migrations) + 3

	db := rawOpen(t, path)
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", future)); err != nil {
		t.Fatalf("stamp future version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("Open accepted a database from a newer multimux; want error")
	}
	msg := err.Error()
	for _, want := range []string{
		"store: migrate:",
		fmt.Sprintf("%d", future),
		fmt.Sprintf("%d", len(migrations)),
		"newer multimux",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

// A migration that fails partway must leave neither its tables nor its version
// bump behind.
func TestMigrationIsAtomic(t *testing.T) {
	orig := migrations
	t.Cleanup(func() { migrations = orig })
	// Valid first statement, then a syntax error: without a transaction the
	// good table would survive the failed migration.
	migrations = append(append([]string{}, orig...),
		`CREATE TABLE atomic_probe (x INTEGER);
		 CREATE TABLE atomic_probe_broken (;`)

	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err == nil {
		s.Close()
		t.Fatal("Open succeeded despite a broken migration; want error")
	}
	if want := fmt.Sprintf("migration %d", len(migrations)); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the failing %s", err, want)
	}

	db := rawOpen(t, path)
	if v := userVersion(t, db); v != len(orig) {
		t.Errorf("user_version = %d after failed migration; want %d", v, len(orig))
	}
	if tableExists(t, db, "atomic_probe") {
		t.Error("partial schema from the failed migration survived")
	}
	// Earlier migrations still committed, so the store stays usable.
	if !tableExists(t, db, "settings") {
		t.Error("earlier migrations were rolled back too")
	}
}
