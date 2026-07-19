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
	code, err := m.NewSetupCode()
	if err != nil {
		t.Fatalf("NewSetupCode: %v", err)
	}
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
	code2, err := m.NewSetupCode()
	if err != nil {
		t.Fatalf("NewSetupCode: %v", err)
	}
	if m.ConsumeSetupCode(old) {
		t.Fatal("regeneration must invalidate old code")
	}
	if !m.ConsumeSetupCode(code2) {
		t.Fatal("new code rejected")
	}
}

func TestSetupCodeAttemptLimit(t *testing.T) {
	m, _ := testManager(t)
	code, err := m.NewSetupCode()
	if err != nil {
		t.Fatalf("NewSetupCode: %v", err)
	}
	for i := 0; i < maxSetupTries; i++ {
		if m.ConsumeSetupCode("WRONG1") {
			t.Fatalf("attempt %d: wrong code accepted", i+1)
		}
	}
	// After maxSetupTries failed attempts, the code must be invalidated even
	// though it is presented correctly.
	if m.ConsumeSetupCode(code) {
		t.Fatal("correct code accepted after exceeding attempt limit")
	}
}

func TestSetupCodeSucceedsWithinAttemptLimit(t *testing.T) {
	m, _ := testManager(t)
	code, err := m.NewSetupCode()
	if err != nil {
		t.Fatalf("NewSetupCode: %v", err)
	}
	for i := 0; i < maxSetupTries-1; i++ {
		if m.ConsumeSetupCode("WRONG1") {
			t.Fatalf("attempt %d: wrong code accepted", i+1)
		}
	}
	if !m.ConsumeSetupCode(code) {
		t.Fatal("correct code rejected within attempt limit")
	}
}

func TestSessionTokenLifecycle(t *testing.T) {
	m, st := testManager(t)
	token, err := m.CreateSession("TestUA")
	if err != nil || token == "" {
		t.Fatalf("CreateSession: %q, %v", token, err)
	}
	ok, slid, err := m.ValidateToken(token)
	if err != nil || !ok {
		t.Fatalf("ValidateToken: %v, %v", ok, err)
	}
	if slid {
		t.Fatal("fresh token should not slide")
	}
	if ok, _, _ := m.ValidateToken("garbage"); ok {
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
	if ok, _, _ := m.ValidateToken(token); ok {
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

// The DB expiry slides on use but the browser cookie's MaxAge is fixed at
// login time; without a refreshed Set-Cookie the browser drops the cookie 30
// days after login even though the server-side session is still live.
func TestMiddlewareRefreshesCookieWhenExpirySlides(t *testing.T) {
	m, st := testManager(t)
	token, _ := m.CreateSession("UA")
	// Age the session so remaining life is under the slide threshold.
	if err := st.TouchAuthSession(hashToken(token), time.Now().UTC().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := m.Middleware(next)

	r := httptest.NewRequest("GET", "/api/tools", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CookieName || cookies[0].Value != token {
		t.Fatalf("want refreshed session cookie, got %+v", cookies)
	}
	if cookies[0].MaxAge != int(sessionTTL.Seconds()) {
		t.Fatalf("MaxAge = %d, want %d", cookies[0].MaxAge, int(sessionTTL.Seconds()))
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("refreshed cookie lost attributes: %+v", cookies[0])
	}
}

func TestMiddlewareNoCookieRefreshForBearerOrFreshSessions(t *testing.T) {
	m, st := testManager(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := m.Middleware(next)

	// Fresh session: nothing slid, no Set-Cookie.
	fresh, _ := m.CreateSession("UA")
	r := httptest.NewRequest("GET", "/api/tools", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: fresh})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Result().Cookies(); len(got) != 0 {
		t.Fatalf("fresh session: unexpected Set-Cookie %+v", got)
	}

	// Aged session presented as a bearer token: no cookie to refresh — setting
	// one would bind another origin's token into this browser's cookie jar.
	bearer, _ := m.CreateSession("UA")
	st.TouchAuthSession(hashToken(bearer), time.Now().UTC().Add(24*time.Hour))
	r = httptest.NewRequest("GET", "/api/tools", nil)
	r.Header.Set("Authorization", "Bearer "+bearer)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("bearer code = %d, want 200", w.Code)
	}
	if got := w.Result().Cookies(); len(got) != 0 {
		t.Fatalf("bearer auth: unexpected Set-Cookie %+v", got)
	}
}

// Browsers attach cookies to WebSockets unconditionally, so a request that
// carries an explicit bearer token (Authorization header or ?token=) must be
// authenticated by that token — the cookie may belong to a different login on
// a same-site daemon, and the WS origin check is skipped for token auth.
func TestTokenFromRequestPrefersExplicitToken(t *testing.T) {
	withCookie := func(r *http.Request) *http.Request {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: "cookie-token"})
		return r
	}

	r := withCookie(httptest.NewRequest("GET", "/ws/pty/1", nil))
	r.Header.Set("Authorization", "Bearer header-token")
	if got := TokenFromRequest(r); got != "header-token" {
		t.Errorf("Authorization + cookie: got %q, want header-token", got)
	}

	r = withCookie(httptest.NewRequest("GET", "/ws/pty/1?token=query-token", nil))
	if got := TokenFromRequest(r); got != "query-token" {
		t.Errorf("query + cookie: got %q, want query-token", got)
	}

	r = withCookie(httptest.NewRequest("GET", "/ws/pty/1", nil))
	if got := TokenFromRequest(r); got != "cookie-token" {
		t.Errorf("cookie only: got %q, want cookie-token", got)
	}
}
