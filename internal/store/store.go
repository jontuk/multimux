// Package store persists all multimux state in a single SQLite database.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database. All timestamps are stored as RFC3339 UTC
// text so values round-trip identically regardless of driver scan behaviour.
type Store struct {
	db *sql.DB
}

// migrations run in order; PRAGMA user_version tracks progress. Append only —
// never edit an entry that has shipped.
var migrations = []string{
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,
	`CREATE TABLE tools (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		name    TEXT NOT NULL,
		command TEXT NOT NULL
	);
	CREATE TABLE dirs (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		path TEXT NOT NULL
	);`,
	`CREATE TABLE sessions (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		tmux_name  TEXT NOT NULL DEFAULT '',
		tool_id    INTEGER NOT NULL,
		dir        TEXT NOT NULL,
		status     TEXT NOT NULL DEFAULT 'running',
		created_at TEXT NOT NULL
	);
	CREATE TABLE layout (
		id   INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	);`,
	`CREATE TABLE credentials (
		id           TEXT PRIMARY KEY,
		name         TEXT NOT NULL,
		data         BLOB NOT NULL,
		created_at   TEXT NOT NULL,
		last_used_at TEXT NOT NULL
	);
	CREATE TABLE auth_sessions (
		token_hash TEXT PRIMARY KEY,
		user_agent TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL
	);`,
}

// Open opens (creating if needed) the database at path, enables WAL, and
// applies pending migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// A single connection sidesteps table-lock contention between the API
	// handlers and background tickers; multimux is a single-user daemon.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf(
			"database schema version %d is newer than this binary understands (%d): "+
				"it was written by a newer multimux — upgrade multimux to open it",
			v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		if err := applyMigration(db, i); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}

// applyMigration runs migrations[i] and the matching user_version bump in one
// transaction, so a failure or crash leaves the database at the previous
// version with none of the new schema. SQLite treats PRAGMA user_version as
// ordinary page-one data, so it rolls back with the enclosing transaction.
func applyMigration(db *sql.DB, i int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Each entry may hold several statements; the driver executes them all.
	if _, err := tx.Exec(migrations[i]); err != nil {
		return err
	}
	// PRAGMA rejects bind parameters ("near \"?\": syntax error"), so the
	// version is interpolated. It is a loop index, never user input.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// GetSetting returns the value for key, or "" when unset.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetSetting upserts a settings key.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetSettings upserts several settings keys in one transaction, so related
// keys (e.g. the daemon identity trio) never land partially.
func (s *Store) SetSettings(kv map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range kv {
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}
