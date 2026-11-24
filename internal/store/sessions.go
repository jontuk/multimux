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
	// Label is the user's display name for this session, or "" for none.
	Label string `json:"label,omitempty"`
}

// CreateSession inserts a running session and derives its tmux name from the
// assigned row ID (mm-{id}). Both operations are wrapped in a transaction to
// ensure atomicity.
func (s *Store) CreateSession(toolID int64, dir string) (Session, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
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
	if _, err := tx.Exec(`UPDATE sessions SET tmux_name = ? WHERE id = ?`, name, id); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	return Session{ID: id, TmuxName: name, ToolID: toolID, Dir: dir, Status: "running", CreatedAt: now}, nil
}

func scanSession(scan func(...any) error) (Session, error) {
	var sess Session
	var created string
	if err := scan(&sess.ID, &sess.TmuxName, &sess.ToolID, &sess.Dir, &sess.Status, &created, &sess.Label); err != nil {
		return Session{}, err
	}
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return Session{}, err
	}
	sess.CreatedAt = t
	return sess, nil
}

const sessionCols = `id, tmux_name, tool_id, dir, status, created_at, label`

func (s *Store) GetSession(id int64) (Session, error) {
	row := s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *Store) ListSessions() ([]Session, error) {
	return s.querySessions(`SELECT ` + sessionCols + ` FROM sessions ORDER BY id`)
}

// ListRunningSessions returns only the rows whose status is "running", in ID
// order — what the background loops act on; dead rows awaiting dismissal are
// of no interest to them.
func (s *Store) ListRunningSessions() ([]Session, error) {
	return s.querySessions(`SELECT ` + sessionCols + ` FROM sessions WHERE status = 'running' ORDER BY id`)
}

func (s *Store) querySessions(query string, args ...any) ([]Session, error) {
	rows, err := s.db.Query(query, args...)
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

// SetSessionStatus also keeps ended_at in step: stamped on the way to dead,
// cleared on the way back. A row already in the requested status is left
// alone, so killing a session that is already dead does not restart the
// retention clock DeleteDeadSessionsBefore runs on.
func (s *Store) SetSessionStatus(id int64, status string) error {
	var endedAt any
	if status == "dead" {
		endedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`UPDATE sessions SET status = ?, ended_at = ? WHERE id = ? AND status <> ?`,
		status, endedAt, id, status)
	return err
}

// SetSessionLabel sets a session's display label; "" clears it. The label is
// cosmetic — tmux_name is untouched, so nothing that keys off it changes.
func (s *Store) SetSessionLabel(id int64, label string) error {
	res, err := s.db.Exec(`UPDATE sessions SET label = ? WHERE id = ?`, label, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSession(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteDeadSessionsBefore deletes the dead rows that ended before cutoff and
// returns them. A dead row with no ended_at died before that column existed;
// its age is counted from created_at instead, which can only make it look
// older than it is — the right way to err for rows that predate the column.
// Running rows are never matched, whatever their age.
func (s *Store) DeleteDeadSessionsBefore(cutoff time.Time) ([]Session, error) {
	return s.querySessions(
		`DELETE FROM sessions
		 WHERE status = 'dead' AND COALESCE(ended_at, created_at) < ?
		 RETURNING `+sessionCols, cutoff.UTC().Format(time.RFC3339))
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
