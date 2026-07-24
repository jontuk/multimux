package store

import (
	"database/sql"
	"fmt"
)

// Tool is a launchable command template (e.g. zsh, claude, codex).
type Tool struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Dir is an allowed working directory for new sessions.
type Dir struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// Rows carry a position column, but it never reaches clients: list order is
// the order, so a client cannot half-apply an ordering it did not ask for.

func (s *Store) CreateTool(name, command string) (Tool, error) {
	id, err := s.insertOrdered(`INSERT INTO tools (name, command, position) VALUES (?, ?, ?)`,
		`tools`, name, command)
	if err != nil {
		return Tool{}, err
	}
	return Tool{ID: id, Name: name, Command: command}, nil
}

func (s *Store) ListTools() ([]Tool, error) {
	rows, err := s.db.Query(`SELECT id, name, command FROM tools ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tool
	for rows.Next() {
		var t Tool
		if err := rows.Scan(&t.ID, &t.Name, &t.Command); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTool(t Tool) error {
	_, err := s.db.Exec(`UPDATE tools SET name = ?, command = ? WHERE id = ?`, t.Name, t.Command, t.ID)
	return err
}

func (s *Store) DeleteTool(id int64) error {
	_, err := s.db.Exec(`DELETE FROM tools WHERE id = ?`, id)
	return err
}

// ReorderTools stores ids as the new tool order. ids must name every existing
// tool exactly once; anything else is rejected without touching a row, so a
// client working from a stale list cannot drop or duplicate entries.
func (s *Store) ReorderTools(ids []int64) error { return s.reorder("tools", ids) }

func (s *Store) CreateDir(name, path string) (Dir, error) {
	id, err := s.insertOrdered(`INSERT INTO dirs (name, path, position) VALUES (?, ?, ?)`,
		`dirs`, name, path)
	if err != nil {
		return Dir{}, err
	}
	return Dir{ID: id, Name: name, Path: path}, nil
}

func (s *Store) ListDirs() ([]Dir, error) {
	rows, err := s.db.Query(`SELECT id, name, path FROM dirs ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dir
	for rows.Next() {
		var d Dir
		if err := rows.Scan(&d.ID, &d.Name, &d.Path); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDir(id int64) error {
	_, err := s.db.Exec(`DELETE FROM dirs WHERE id = ?`, id)
	return err
}

// ReorderDirs stores ids as the new directory order, with the same all-or-
// nothing rules as ReorderTools.
func (s *Store) ReorderDirs(ids []int64) error { return s.reorder("dirs", ids) }

// insertOrdered appends a row at the end of table's order. Reading the current
// maximum and inserting in one transaction keeps two concurrent adds from
// landing on the same position.
func (s *Store) insertOrdered(insert, table string, a, b string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var next int64
	// Table names cannot be bound; they are package constants, never user input.
	if err := tx.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM ` + table).Scan(&next); err != nil {
		return 0, err
	}
	res, err := tx.Exec(insert, a, b, next)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// reorder rewrites table's positions to 1..len(ids), in one transaction.
func (s *Store) reorder(table string, ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := checkIDSet(tx, table, ids); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE `+table+` SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ErrOrderMismatch reports a reorder whose ids are not exactly the rows that
// exist. Callers turn this into a 400, not a 500: the client is out of date.
var ErrOrderMismatch = fmt.Errorf("order must list every id exactly once")

func checkIDSet(tx *sql.Tx, table string, ids []int64) error {
	rows, err := tx.Query(`SELECT id FROM ` + table)
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		existing[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) != len(existing) {
		return fmt.Errorf("%w: got %d ids for %d rows", ErrOrderMismatch, len(ids), len(existing))
	}
	for _, id := range ids {
		if !existing[id] {
			return fmt.Errorf("%w: id %d does not exist", ErrOrderMismatch, id)
		}
		delete(existing, id) // a repeat of the same id fails the next lookup
	}
	return nil
}

// SeedDefaults inserts the default shell tool on an empty tools table:
// zsh on macOS, bash on Linux.
func (s *Store) SeedDefaults(goos string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tools`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	shell := "bash"
	if goos == "darwin" {
		shell = "zsh"
	}
	_, err := s.CreateTool(shell, shell)
	return err
}
