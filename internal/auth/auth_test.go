package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jontuk/multimux/internal/store"
)

func testManager(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m, err := New(st, "localhost", []string{"https://localhost:8686"})
	if err != nil {
		t.Fatal(err)
	}
	return m, st
}

func TestSetupPending(t *testing.T) {
	m, st := testManager(t)
	pending, err := m.SetupPending()
	if err != nil || !pending {
		t.Fatalf("fresh store: pending=%v err=%v; want true", pending, err)
	}
	now := time.Now().UTC()
	st.AddCredential(store.Credential{ID: "c", Name: "k", Data: []byte("{}"), CreatedAt: now, LastUsedAt: now})
	if pending, _ = m.SetupPending(); pending {
		t.Fatal("with credential: want not pending")
	}
}

func TestSetupCode(t *testing.T) {
	m, _ := testManager(t)
	code := m.NewSetupCode()
	if len(code) != 6 {
		t.Fatalf("code = %q, want 6 chars", code)
	}
	if m.ConsumeSetupCode("WRONG1") {
		t.Fatal("wrong code accepted")
	}
	if !m.ConsumeSetupCode(code) {
		t.Fatal("right code rejected")
	}
	old := code
	code2 := m.NewSetupCode()
	if m.ConsumeSetupCode(old) {
		t.Fatal("regeneration must invalidate old code")
	}
	if !m.ConsumeSetupCode(code2) {
		t.Fatal("new code rejected")
	}
}

func TestSessionTokenLifecycle(t *testing.T) {
	m, st := testManager(t)
	token, err := m.CreateSession("TestUA")
	if err != nil || token == "" {
		t.Fatalf("CreateSession: %q, %v", token, err)
	}
	ok, err := m.ValidateToken(token)
	if err != nil || !ok {
		t.Fatalf("ValidateToken: %v, %v", ok, err)
	}
	if ok, _ := m.ValidateToken("garbage"); ok {
		t.Fatal("garbage token validated")
	}
	// Raw token never stored.
	sessions, _ := st.ListAuthSessions()
	if len(sessions) != 1 || sessions[0].TokenHash == token {
		t.Fatalf("token stored raw or missing: %+v", sessions)
	}
	if err := m.Logout(token); err != nil {
		t.Fatal(err)
	}
	if ok, _ := m.ValidateToken(token); ok {
		t.Fatal("token valid after logout")
	}
}

func TestMiddleware(t *testing.T) {
	m, _ := testManager(t)
	token, _ := m.CreateSession("UA")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := m.Middleware(next)

	cases := []struct {
		name string
		set  func(r *http.Request)
		want int
	}{
		{"no auth", func(r *http.Request) {}, 401},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: CookieName, Value: token}) }, 200},
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }, 200},
		{"query", func(r *http.Request) { q := r.URL.Query(); q.Set("token", token); r.URL.RawQuery = q.Encode() }, 200},
		{"bad cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: CookieName, Value: "nope"}) }, 401},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/api/tools", nil)
		c.set(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: code = %d, want %d", c.name, w.Code, c.want)
		}
	}
}
