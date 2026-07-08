package store

import (
	"database/sql"
	"errors"
	"time"
)

// Credential is a registered WebAuthn credential. Data is the JSON-marshalled
// webauthn.Credential — opaque to this package by design.
type Credential struct {
	ID         string // base64url credential ID
	Name       string
	Data       []byte
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// AuthSession is a server-side login session (cookie- or bearer-token-backed).
// Only the SHA-256 hex hash of the token is stored.
type AuthSession struct {
	TokenHash string
	UserAgent string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (s *Store) AddCredential(c Credential) error {
	_, err := s.db.Exec(
		`INSERT INTO credentials (id, name, data, created_at, last_used_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Data, c.CreatedAt.UTC().Format(time.RFC3339), c.LastUsedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) ListCredentials() ([]Credential, error) {
	rows, err := s.db.Query(`SELECT id, name, data, created_at, last_used_at FROM credentials ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		var c Credential
		var created, used string
		if err := rows.Scan(&c.ID, &c.Name, &c.Data, &created, &used); err != nil {
			return nil, err
		}
		if c.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, err
		}
		if c.LastUsedAt, err = time.Parse(time.RFC3339, used); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCredentialData replaces the opaque credential blob (e.g. new sign
// count after login) and bumps last_used_at.
func (s *Store) UpdateCredentialData(id string, data []byte) error {
	_, err := s.db.Exec(`UPDATE credentials SET data = ?, last_used_at = ? WHERE id = ?`,
		data, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *Store) DeleteCredential(id string) error {
	_, err := s.db.Exec(`DELETE FROM credentials WHERE id = ?`, id)
	return err
}

func (s *Store) CountCredentials() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM credentials`).Scan(&n)
	return n, err
}

func (s *Store) DeleteAllCredentials() error {
	_, err := s.db.Exec(`DELETE FROM credentials`)
	return err
}

func (s *Store) CreateAuthSession(a AuthSession) error {
	_, err := s.db.Exec(
		`INSERT INTO auth_sessions (token_hash, user_agent, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		a.TokenHash, a.UserAgent, a.CreatedAt.UTC().Format(time.RFC3339), a.ExpiresAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetAuthSession(tokenHash string) (AuthSession, bool, error) {
	var a AuthSession
	var created, expires string
	err := s.db.QueryRow(
		`SELECT token_hash, user_agent, created_at, expires_at FROM auth_sessions WHERE token_hash = ?`,
		tokenHash).Scan(&a.TokenHash, &a.UserAgent, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthSession{}, false, nil
	}
	if err != nil {
		return AuthSession{}, false, err
	}
	if a.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return AuthSession{}, false, err
	}
	if a.ExpiresAt, err = time.Parse(time.RFC3339, expires); err != nil {
		return AuthSession{}, false, err
	}
	return a, true, nil
}

func (s *Store) TouchAuthSession(tokenHash string, expires time.Time) error {
	_, err := s.db.Exec(`UPDATE auth_sessions SET expires_at = ? WHERE token_hash = ?`,
		expires.UTC().Format(time.RFC3339), tokenHash)
	return err
}

func (s *Store) ListAuthSessions() ([]AuthSession, error) {
	rows, err := s.db.Query(`SELECT token_hash, user_agent, created_at, expires_at FROM auth_sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthSession
	for rows.Next() {
		var a AuthSession
		var created, expires string
		if err := rows.Scan(&a.TokenHash, &a.UserAgent, &created, &expires); err != nil {
			return nil, err
		}
		if a.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, err
		}
		if a.ExpiresAt, err = time.Parse(time.RFC3339, expires); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAuthSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteAllAuthSessions() error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions`)
	return err
}

func (s *Store) DeleteExpiredAuthSessions(now time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM auth_sessions WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
