package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jontuk/multimux/internal/auth"
	"github.com/jontuk/multimux/internal/store"
	"github.com/jontuk/multimux/internal/tmuxmgr"
)

// newTestServer builds a Server on a temp store with a fake web FS.
// registered=true seeds one credential so the daemon is past setup-pending.
func newTestServer(t *testing.T, registered bool) (*Server, *store.Store, *auth.Manager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if registered {
		now := time.Now().UTC()
		st.AddCredential(store.Credential{ID: "c1", Name: "k", Data: []byte("{}"), CreatedAt: now, LastUsedAt: now})
	}
	am, err := auth.New(st, "localhost", []string{"https://localhost:8686"})
	if err != nil {
		t.Fatal(err)
	}
	webFS := fstest.MapFS{
		"index.html":    {Data: []byte("<html>multimux</html>")},
		"assets/app.js": {Data: []byte("//js")},
	}
	s := New(Config{
		Store: st, Auth: am, Tmux: tmuxmgr.New("mm", "test-none"),
		Arbiter: tmuxmgr.NewArbiter(), WebFS: webFS,
		Origins: []string{"https://localhost:8686"}, Version: "test",
	})
	return s, st, am
}

// authedRequest performs req with a valid bearer token.
func do(t *testing.T, s *Server, method, path, token string, body ...string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if len(body) > 0 {
		r = httptest.NewRequest(method, path, stringsReader(body[0]))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestHealthzOpen(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	w := do(t, s, "GET", "/healthz", "")
	if w.Code != 200 {
		t.Fatalf("healthz = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["version"] != "test" {
		t.Fatalf("body = %v", body)
	}
}

func TestAPIRequiresAuth(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	if w := do(t, s, "GET", "/api/tools", ""); w.Code != 401 {
		t.Fatalf("unauthed /api/tools = %d, want 401", w.Code)
	}
}

func TestSetupGateBlocksAPI(t *testing.T) {
	s, _, am := newTestServer(t, false) // no credential → setup pending
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "GET", "/api/tools", token); w.Code != 403 {
		t.Fatalf("setup-pending /api/tools = %d, want 403", w.Code)
	}
	if w := do(t, s, "GET", "/healthz", ""); w.Code != 200 {
		t.Fatalf("setup-pending healthz = %d, want 200", w.Code)
	}
}

func TestStaticSPAFallback(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	for _, path := range []string{"/", "/settings", "/assets/app.js"} {
		w := do(t, s, "GET", path, "")
		if w.Code != 200 {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
	}
}

func TestCORSPreflight(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	r := httptest.NewRequest("OPTIONS", "/api/tools", nil)
	r.Header.Set("Origin", "https://otherhost:8686")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight = %d, ACAO=%q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("must never allow credentials cross-origin")
	}
}

// TestCSRFGate covers the cross-origin defense for cookie-authenticated
// mutations: CORS only stops the response being read, not the mutation
// executing, so unsafe cookie-carrying requests must present our own Origin
// and a JSON content type. Explicit bearer tokens stay cross-origin capable —
// that is how cross-daemon calls authenticate.
func TestCSRFGate(t *testing.T) {
	const ownOrigin = "https://localhost:8686"
	newReq := func(method, path, body string) *http.Request {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, stringsReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		return r
	}
	run := func(s *Server, r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	toolBody := `{"name":"x","command":"y"}`

	tests := []struct {
		name   string
		setup  func(r *http.Request, cookie, bearer string)
		method string
		path   string
		body   string
		want   int
	}{
		{"cookie POST foreign origin rejected", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", "https://evil.example")
			r.Header.Set("Content-Type", "application/json")
		}, "POST", "/api/tools", toolBody, 403},
		{"cookie POST absent origin rejected", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Content-Type", "application/json")
		}, "POST", "/api/tools", toolBody, 403},
		{"cookie POST own origin allowed", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", ownOrigin)
			r.Header.Set("Content-Type", "application/json")
		}, "POST", "/api/tools", toolBody, 201},
		{"cookie POST own origin text/plain rejected", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", ownOrigin)
			r.Header.Set("Content-Type", "text/plain")
		}, "POST", "/api/tools", toolBody, 415},
		{"cookie POST no content type rejected", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", ownOrigin)
		}, "POST", "/api/tools", toolBody, 415},
		{"cookie bodyless POST foreign origin rejected", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", "https://evil.example")
		}, "POST", "/api/auth/logout", "", 403},
		{"cookie DELETE foreign origin rejected", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", "https://evil.example")
		}, "DELETE", "/api/tools/1", "", 403},
		{"cookie GET foreign origin still allowed", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", "https://evil.example")
		}, "GET", "/api/tools", "", 200},
		{"bearer POST foreign origin allowed", func(r *http.Request, _, bearer string) {
			r.Header.Set("Authorization", "Bearer "+bearer)
			r.Header.Set("Origin", "https://otherhost:8686")
			r.Header.Set("Content-Type", "application/json")
		}, "POST", "/api/tools", toolBody, 201},
		{"garbage bearer cannot ride valid cookie", func(r *http.Request, cookie, _ string) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Authorization", "Bearer garbage")
			r.Header.Set("Origin", "https://evil.example")
			r.Header.Set("Content-Type", "application/json")
		}, "POST", "/api/tools", toolBody, 401},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, st, am := newTestServer(t, true)
			cookie, _ := am.CreateSession("UA")
			bearer, _ := am.CreateSession("UA")
			r := newReq(tc.method, tc.path, tc.body)
			tc.setup(r, cookie, bearer)
			w := run(s, r)
			if w.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d (body %s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
			}
			// A rejected mutation must not have executed.
			if tc.want >= 400 && tc.method == "POST" && tc.path == "/api/tools" {
				tools, _ := st.ListTools()
				if len(tools) != 0 {
					t.Fatalf("rejected request still created tool: %v", tools)
				}
			}
		})
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	huge := `{"name":"x","command":"` + strings.Repeat("a", 2<<20) + `"}`
	if w := do(t, s, "POST", "/api/tools", token, huge); w.Code != 400 {
		t.Fatalf("2MB body = %d, want 400", w.Code)
	}
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
