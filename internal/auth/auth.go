// Package auth implements passkey (WebAuthn) authentication, first-run setup
// codes, and server-side session tokens.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/jontuk/multimux/internal/store"
)

const (
	// CookieName is the session cookie set on successful login.
	CookieName = "mm_session"

	sessionTTL     = 30 * 24 * time.Hour
	slideThreshold = 29 * 24 * time.Hour // renew when remaining life dips below this
	setupCodeTTL   = 15 * time.Minute
)

type Manager struct {
	store   *store.Store
	rpID    string
	origins []string

	mu          sync.Mutex
	setupCode   string
	setupExpiry time.Time
	// webauthn state added in the next task
}

func New(st *store.Store, rpID string, origins []string) (*Manager, error) {
	m := &Manager{store: st, rpID: rpID, origins: origins}
	return m, m.initWebAuthn() // no-op stub until Task 11
}

// initWebAuthn is completed in the WebAuthn task; stub keeps this task green.
func (m *Manager) initWebAuthn() error { return nil }

// SetupPending reports whether no passkey is registered yet.
func (m *Manager) SetupPending() (bool, error) {
	n, err := m.store.CountCredentials()
	return n == 0, err
}

// NewSetupCode mints the single active setup code (15-minute TTL).
func (m *Manager) NewSetupCode() string {
	var b [4]byte
	rand.Read(b[:])
	code := base32.StdEncoding.EncodeToString(b[:])[:6]
	m.mu.Lock()
	m.setupCode, m.setupExpiry = code, time.Now().Add(setupCodeTTL)
	m.mu.Unlock()
	return code
}

// ConsumeSetupCode reports whether code matches the active, unexpired setup
// code. The code stays valid until registration completes (the finish handler
// re-checks) so begin/finish can both present it.
func (m *Manager) ConsumeSetupCode(code string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setupCode == "" || time.Now().After(m.setupExpiry) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(code), []byte(m.setupCode)) == 1
}

// ClearSetupCode invalidates the active code (call after first registration).
func (m *Manager) ClearSetupCode() {
	m.mu.Lock()
	m.setupCode = ""
	m.mu.Unlock()
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession mints a new session token (returned raw exactly once; only
// its SHA-256 hash is stored).
func (m *Manager) CreateSession(userAgent string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UTC()
	err := m.store.CreateAuthSession(store.AuthSession{
		TokenHash: hashToken(token),
		UserAgent: userAgent,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionTTL),
	})
	return token, err
}

// ValidateToken checks a raw token and applies the 30-day sliding expiry.
func (m *Manager) ValidateToken(token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	h := hashToken(token)
	sess, ok, err := m.store.GetAuthSession(h)
	if err != nil || !ok {
		return false, err
	}
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		_ = m.store.DeleteAuthSession(h)
		return false, nil
	}
	if sess.ExpiresAt.Sub(now) < slideThreshold {
		if err := m.store.TouchAuthSession(h, now.Add(sessionTTL)); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (m *Manager) Logout(token string) error {
	return m.store.DeleteAuthSession(hashToken(token))
}

// TokenFromRequest extracts the session token from cookie, Authorization
// header, or ?token= query param (in that order).
func TokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return r.URL.Query().Get("token")
}

// Middleware rejects requests without a valid session.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := m.ValidateToken(TokenFromRequest(r))
		if err != nil {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
