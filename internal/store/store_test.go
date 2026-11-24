package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// Upgrading an install that predates the position column must preserve the
// order its owner already sees, which is id order.
func TestPositionBackfillPreservesExistingOrder(t *testing.T) {
	const preOrdering = 4 // migrations up to and including auth tables

	path := filepath.Join(t.TempDir(), "test.db")
	db := rawOpen(t, path)
	for i := range preOrdering {
		if err := applyMigration(db, i); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	for _, name := range []string{"zsh", "claude", "codex"} {
		if _, err := db.Exec(`INSERT INTO tools (name, command) VALUES (?, ?)`, name, name); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO dirs (name, path) VALUES ('repos', '/repos')`); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after upgrade: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if got := toolNames(t, s); got != "zsh,claude,codex" {
		t.Fatalf("order after upgrade = %s, want zsh,claude,codex", got)
	}
	// Backfilled rows must not collide with the position given to new ones.
	if _, err := s.CreateTool("bash", "bash"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(t, s); got != "zsh,claude,codex,bash" {
		t.Fatalf("order after adding = %s, want bash last", got)
	}
}

// The seeded home directory used to be labelled "Home"; upgrading relabels it
// to "~", but only where it really is the seeded home row.
func TestHomeDirRelabel(t *testing.T) {
	const preRelabel = 7 // migrations up to and including dir_subdirs

	path := filepath.Join(t.TempDir(), "test.db")
	db := rawOpen(t, path)
	for i := range preRelabel {
		if err := applyMigration(db, i); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	rows := []struct{ name, path string }{
		{"Home", "/Users/jon"},
		{"Home", "/home/jon"},
		{"Home", "/Users/jon/projects"}, // hand-made, deeper: leave alone
		{"Home", "/srv/shared"},         // hand-made, elsewhere: leave alone
		{"repos", "/Users/jon"},         // already renamed by its owner
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO dirs (name, path) VALUES (?, ?)`, r.name, r.path); err != nil {
			t.Fatalf("seed dir %s: %v", r.path, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after upgrade: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	dirs, err := s.ListDirs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"~", "~", "Home", "Home", "repos"}
	if len(dirs) != len(want) {
		t.Fatalf("got %d dirs, want %d", len(dirs), len(want))
	}
	for i, d := range dirs {
		if d.Name != want[i] {
			t.Errorf("dir %s named %q, want %q", d.Path, d.Name, want[i])
		}
	}
}

// Sessions that were already dead when ended_at arrived keep their rows and
// get no ended_at, so the sweep ages them from created_at.
func TestEndedAtMigrationKeepsExistingSessions(t *testing.T) {
	const preEndedAt = 8 // migrations up to and including the home relabel

	path := filepath.Join(t.TempDir(), "test.db")
	db := rawOpen(t, path)
	for i := range preEndedAt {
		if err := applyMigration(db, i); err != nil {
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-time.Hour).Format(time.RFC3339)
	for _, r := range []struct{ name, status, created string }{
		{"mm-1", "dead", old},
		{"mm-2", "dead", recent},
		{"mm-3", "running", old},
	} {
		if _, err := db.Exec(
			`INSERT INTO sessions (tmux_name, tool_id, dir, status, created_at) VALUES (?, 1, '/tmp', ?, ?)`,
			r.name, r.status, r.created); err != nil {
			t.Fatalf("seed %s: %v", r.name, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after upgrade: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if all, err := s.ListSessions(); err != nil || len(all) != 3 {
		t.Fatalf("sessions after upgrade = %+v, %v, want all 3", all, err)
	}
	var stamped int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE ended_at IS NOT NULL`).Scan(&stamped); err != nil {
		t.Fatal(err)
	}
	if stamped != 0 {
		t.Fatalf("%d upgraded rows got an ended_at, want none", stamped)
	}

	deleted, err := s.DeleteDeadSessionsBefore(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].TmuxName != "mm-1" {
		t.Fatalf("swept %+v, want only the old dead mm-1", deleted)
	}
	// A session that dies after the upgrade is stamped like any other.
	if err := s.SetSessionStatus(3, "dead"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE ended_at IS NOT NULL`).Scan(&stamped); err != nil {
		t.Fatal(err)
	}
	if stamped != 1 {
		t.Fatalf("%d rows stamped after a post-upgrade death, want 1", stamped)
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
