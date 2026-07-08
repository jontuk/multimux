package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned by point lookups when no row matches.
var ErrNotFound = errors.New("store: not found")

// Session is one managed tmux session.
type Session struct {
	ID        int64     `json:"id"`
	TmuxName  string    `json:"tmuxName"`
	ToolID    int64     `json:"toolId"`
	Dir       string    `json:"dir"`
	Status    string    `json:"status"` // "running" | "dead"
	CreatedAt time.Time `json:"createdAt"`
}

// CreateSession inserts a running session and derives its tmux name from the
// assigned row ID (mm-{id}).
func (s *Store) CreateSession(toolID int64, dir string) (Session, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO sessions (tool_id, dir, status, created_at) VALUES (?, ?, 'running', ?)`,
		toolID, dir, now.Format(time.RFC3339))
	if err != nil {
		return Session{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Session{}, err
	}
	name := fmt.Sprintf("mm-%d", id)
	if _, err := s.db.Exec(`UPDATE sessions SET tmux_name = ? WHERE id = ?`, name, id); err != nil {
		return Session{}, err
	}
	return Session{ID: id, TmuxName: name, ToolID: toolID, Dir: dir, Status: "running", CreatedAt: now}, nil
}

func scanSession(scan func(...any) error) (Session, error) {
	var sess Session
	var created string
	if err := scan(&sess.ID, &sess.TmuxName, &sess.ToolID, &sess.Dir, &sess.Status, &created); err != nil {
		return Session{}, err
	}
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return Session{}, err
	}
	sess.CreatedAt = t
	return sess, nil
}

const sessionCols = `id, tmux_name, tool_id, dir, status, created_at`

func (s *Store) GetSession(id int64) (Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *Store) ListSessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT ` + sessionCols + ` FROM sessions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) SetSessionStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE sessions SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) DeleteSession(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// GetLayout returns the layout JSON document, or "" when none saved.
func (s *Store) GetLayout() (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT data FROM layout WHERE id = 1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetLayout upserts the singleton layout row.
func (s *Store) SetLayout(data string) error {
	_, err := s.db.Exec(
		`INSERT INTO layout (id, data) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET data = excluded.data`, data)
	return err
}
