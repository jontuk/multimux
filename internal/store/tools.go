package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
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

// commandSep separates the commands of a tool group: one launch of
// `zsh ;; claude` opens two sessions. It is deliberately not a comma or a
// pipe — both appear in real command lines (`--allowed-tools Read,Edit`,
// `tail -f log | grep x`), and a separator that collides with ordinary syntax
// turns one session into two without the user asking. `;;` is only valid shell
// inside a case statement, so a tool command is very unlikely to contain one by
// accident; `\;;` escapes it for the cases that do.
const commandSep = ";;"

// SplitCommand splits a tool's command into the commands of its group, one per
// session to launch. A command with no separator yields itself, so an ordinary
// tool still launches exactly one session. Blank segments are dropped — a
// stray separator is a typo, not a session — but a command that is blank all
// the way through stays one empty segment, because an empty command field
// meant "launch with no command" before groups existed and still does.
func SplitCommand(cmd string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(cmd); {
		switch {
		case strings.HasPrefix(cmd[i:], `\`+commandSep):
			cur.WriteString(commandSep)
			i += 1 + len(commandSep)
		case strings.HasPrefix(cmd[i:], commandSep):
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			i += len(commandSep)
		default:
			cur.WriteByte(cmd[i])
			i++
		}
	}
	out = append(out, strings.TrimSpace(cur.String()))

	kept := make([]string, 0, len(out))
	for _, seg := range out {
		if seg != "" {
			kept = append(kept, seg)
		}
	}
	if len(kept) == 0 {
		return []string{""}
	}
	return kept
}

// CommandLabel names one command of a group for display: the program it runs,
// without its arguments or its path. Sessions record a tool, not a command, so
// without this every tile in a group would carry the group's name.
func CommandLabel(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
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

// SeedDefaults inserts the default shell tool on an empty tools table (zsh on
// macOS, bash on Linux) and the user's home directory on an empty dirs table.
// Each table is seeded independently, so clearing one out does not resurrect
// entries the user deleted from the other. home may be empty, in which case no
// directory is seeded.
func (s *Store) SeedDefaults(goos, home string) error {
	if err := s.seedTool(goos); err != nil {
		return err
	}
	return s.seedDir(home)
}

func (s *Store) seedTool(goos string) error {
	empty, err := s.isEmpty("tools")
	if err != nil || !empty {
		return err
	}
	shell := "bash"
	if goos == "darwin" {
		shell = "zsh"
	}
	_, err = s.CreateTool(shell, shell)
	return err
}

func (s *Store) seedDir(home string) error {
	if home == "" {
		return nil
	}
	empty, err := s.isEmpty("dirs")
	if err != nil || !empty {
		return err
	}
	_, err = s.CreateDir("~", home)
	return err
}

func (s *Store) isEmpty(table string) (bool, error) {
	var n int
	// Table names cannot be bound; they are package constants, never user input.
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// subdirHistoryLimit caps the remembered subdirs per directory. Ten covers the
// handful a person actually cycles through and keeps the dropdown short enough
// that it never needs its own scrollbar.
const subdirHistoryLimit = 10

// Recency ordering is a plain lexicographic ORDER BY on used_at, so the format
// must be fixed width: two launches inside the same second are ordinary, and
// time.RFC3339's second precision would tie them. This is RFC3339 with a
// constant nine-digit fraction, so time.Parse(time.RFC3339, …) still reads it.
const subdirTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// ListSubdirs returns the subdirs last launched into under dirID, newest
// first, at most subdirHistoryLimit of them. The result is never nil: it is
// written straight to JSON, where nil would marshal as null.
func (s *Store) ListSubdirs(dirID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT subdir FROM dir_subdirs WHERE dir_id = ? ORDER BY used_at DESC LIMIT ?`,
		dirID, subdirHistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var sub string
		if err := rows.Scan(&sub); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// RecordSubdir remembers subdir under dirID, or bumps it to the front if it is
// already remembered. A blank subdir is not history, so it is dropped.
func (s *Store) RecordSubdir(dirID int64, subdir string) error {
	subdir = strings.TrimSpace(subdir)
	if subdir == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO dir_subdirs (dir_id, subdir, used_at) VALUES (?, ?, ?)
		 ON CONFLICT(dir_id, subdir) DO UPDATE SET used_at = excluded.used_at`,
		dirID, subdir, time.Now().UTC().Format(subdirTimeFormat)); err != nil {
		return err
	}
	// Trimming inside the insert's transaction is what bounds the table: two
	// tabs launching at once cannot interleave into a list above the limit.
	if _, err := tx.Exec(
		`DELETE FROM dir_subdirs WHERE dir_id = ? AND subdir NOT IN (
			SELECT subdir FROM dir_subdirs WHERE dir_id = ? ORDER BY used_at DESC LIMIT ?)`,
		dirID, dirID, subdirHistoryLimit); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSubdir forgets one remembered subdir. Deleting an entry that is
// already gone succeeds — the caller is repeating itself, not misbehaving.
func (s *Store) DeleteSubdir(dirID int64, subdir string) error {
	_, err := s.db.Exec(`DELETE FROM dir_subdirs WHERE dir_id = ? AND subdir = ?`, dirID, subdir)
	return err
}
