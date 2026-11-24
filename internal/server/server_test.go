package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jontuk/multimux/internal/auth"
	"github.com/jontuk/multimux/internal/pki"
	"github.com/jontuk/multimux/internal/store"
	"github.com/jontuk/multimux/internal/tmuxmgr"
)

// newTestServerCfg builds the Config newTestServer uses, so tests that need a
// non-default field (NoAuth) can tweak it before calling New.
// registered=true seeds one credential so the daemon is past setup-pending.
func newTestServerCfg(t *testing.T, registered bool) (Config, *store.Store, *auth.Manager) {
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
	testPKI := pki.New(t.TempDir())
	if _, err := testPKI.Ensure([]string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(testPKI.CACertPath())
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		Store: st, Auth: am, Tmux: tmuxmgr.New("mm", "test-none"),
		Arbiter: tmuxmgr.NewArbiter(), WebFS: webFS,
		Origins: []string{"https://localhost:8686"}, Version: "test",
		ReadCA: func() ([]byte, error) { return append([]byte(nil), caPEM...), nil },
	}, st, am
}

// newTestServer builds a Server on a temp store with a fake web FS.
// registered=true seeds one credential so the daemon is past setup-pending.
func newTestServer(t *testing.T, registered bool) (*Server, *store.Store, *auth.Manager) {
	t.Helper()
	cfg, st, am := newTestServerCfg(t, registered)
	s := New(cfg)
	// Git pacing is off unless a test asks for it. On a loaded machine even a
	// tiny repo's git status can cost enough to earn a wait, and a test that
	// calls CheckGitInfo back to back would then find its dir skipped.
	s.gitCostFactor = 1
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

// setupGateCases pins exactly which paths the setup gate guards. The gate
// sees the raw request path, before the mux cleans or matches it.
var setupGateCases = []struct {
	path      string
	protected bool
}{
	{"/", false},
	{"/settings", false},
	{"/assets/app.js", false},
	{"/icon.svg", false},
	{"/manifest.webmanifest", false},
	{"/healthz", false},
	{"/ca.crt", false},
	{"/ca/info", false},
	{"/api", false}, // no trailing slash: not under /api/, falls to static
	{"/ws", false},
	{"/API/tools", false}, // the mux is case-sensitive too: this is a static path
	{"//api/tools", false},
	{"/api/auth/setup/begin", false},
	{"/api/auth/setup/finish", false},
	{"/api/auth/setup/", false},
	{"/api/auth/setup/../tools", false}, // the mux redirects it to /api/tools, which is gated
	{"/api/auth/login/begin", false},
	{"/api/auth/login/finish", false},
	{"/api/", true},
	{"/api/auth/setup", true},
	{"/api/auth/setupx/begin", true},
	{"/api/auth/login", true},
	{"/api/auth/me", true},
	{"/api/auth/token", true},
	{"/api/auth/register/begin", true},
	{"/api/auth/credentials", true},
	{"/api/tools", true},
	{"/api/sessions", true},
	{"/api/sessions/1/text", true},
	{"/api/settings", true},
	{"/api/layout", true},
	{"/api/nope", true},
	{"/ws/", true},
	{"/ws/events", true},
	{"/ws/pty/1", true},
}

// gateResult runs one request through setupGate alone and reports whether it
// reached the next handler, plus what the gate itself answered if not.
func gateResult(s *Server, method, path string) (passed bool, code int, body string) {
	h := s.setupGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		passed = true
		w.WriteHeader(http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return passed, w.Code, strings.TrimSpace(w.Body.String())
}

func TestSetupGateTable(t *testing.T) {
	pendingSrv, _, _ := newTestServer(t, false)
	readySrv, _, _ := newTestServer(t, true)
	for _, tc := range setupGateCases {
		if got := isProtected(tc.path); got != tc.protected {
			t.Errorf("isProtected(%q) = %v, want %v", tc.path, got, tc.protected)
		}
		for _, method := range []string{"GET", "POST", "DELETE"} {
			// Setup pending: protected paths are refused, everything else passes.
			passed, code, body := gateResult(pendingSrv, method, tc.path)
			if tc.protected {
				if passed || code != http.StatusForbidden || body != `{"error":"setup pending"}` {
					t.Errorf("pending %s %s: passed=%v %d %s; want 403 setup pending", method, tc.path, passed, code, body)
				}
			} else if !passed {
				t.Errorf("pending %s %s: blocked with %d %s; want it passed through", method, tc.path, code, body)
			}
			// Set up: the gate refuses nothing.
			if passed, code, body := gateResult(readySrv, method, tc.path); !passed {
				t.Errorf("set up %s %s: blocked with %d %s; want it passed through", method, tc.path, code, body)
			}
		}
	}
}

// The same table through the whole middleware stack: while setup is pending a
// protected path answers 403 whether or not the caller holds a session token,
// and no unprotected path does.
func TestSetupGateTableThroughHandler(t *testing.T) {
	s, _, am := newTestServer(t, false)
	token, _ := am.CreateSession("UA")
	for _, tc := range setupGateCases {
		for _, tok := range []string{"", token} {
			w := do(t, s, "GET", tc.path, tok)
			blocked := w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), "setup pending")
			if blocked != tc.protected {
				t.Errorf("pending GET %s (token=%v) = %d %s; want blocked=%v",
					tc.path, tok != "", w.Code, strings.TrimSpace(w.Body.String()), tc.protected)
			}
		}
	}
	ready, _, _ := newTestServer(t, true)
	for _, tc := range setupGateCases {
		if w := do(t, ready, "GET", tc.path, ""); strings.Contains(w.Body.String(), "setup pending") {
			t.Errorf("set up GET %s = %d %s; the gate must not answer", tc.path, w.Code, w.Body.String())
		} else if tc.protected && w.Code != http.StatusUnauthorized {
			t.Errorf("set up GET %s without a token = %d, want 401", tc.path, w.Code)
		}
	}
}

// The gate reads the answer afresh for every protected request: it follows a
// passkey being added and removed again, including by another handle on the
// same database (the CLI is a separate process).
func TestSetupGateFollowsCredentialChanges(t *testing.T) {
	cfg, _, _ := newTestServerCfg(t, false)
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	var handles [2]*store.Store
	for i := range handles {
		st, err := store.Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		handles[i] = st
	}
	am, err := auth.New(handles[0], "localhost", []string{"https://localhost:8686"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Store, cfg.Auth = handles[0], am
	s := New(cfg)
	other := handles[1]
	blocked := func() bool {
		passed, _, _ := gateResult(s, "GET", "/api/tools")
		return !passed
	}
	if !blocked() {
		t.Fatal("no passkey: /api/tools passed the gate")
	}
	now := time.Now().UTC()
	if err := other.AddCredential(store.Credential{ID: "c1", Name: "k", Data: []byte("{}"), CreatedAt: now, LastUsedAt: now}); err != nil {
		t.Fatal(err)
	}
	if blocked() {
		t.Fatal("passkey added by another process: /api/tools still blocked")
	}
	if err := other.DeleteCredential("c1"); err != nil {
		t.Fatal(err)
	}
	if !blocked() {
		t.Fatal("passkey removed by another process: /api/tools still passes the gate")
	}
}

// With the store unusable the gate cannot tell whether setup is pending, so
// it fails closed on every protected path. Unprotected paths pass in either
// state, so they are served without asking.
func TestSetupGateFailsClosedWhenStoreIsUnavailable(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	st.Close()
	for _, tc := range setupGateCases {
		passed, code, body := gateResult(s, "GET", tc.path)
		if tc.protected {
			if passed || code != http.StatusInternalServerError || body != `{"error":"internal"}` {
				t.Errorf("GET %s: passed=%v %d %s; want 500 internal", tc.path, passed, code, body)
			}
		} else if !passed {
			t.Errorf("GET %s: blocked with %d %s; want it passed through", tc.path, code, body)
		}
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

// 500s must be visible in the daemon log, not just the response body.
func TestServerErrorsAreLogged(t *testing.T) {
	s, st, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	st.Close() // every store call now errors
	if w := do(t, s, "GET", "/api/sessions", token); w.Code != 500 {
		t.Fatalf("code = %d, want 500", w.Code)
	}
	log := buf.String()
	if !strings.Contains(log, "/api/sessions") || !strings.Contains(log, "500") {
		t.Fatalf("500 response not logged: %q", log)
	}
}

// Browsers lowercase the host when they serialize an Origin, so a daemon
// configured with a hostname that kept its case (os.Hostname() on a Mac) would
// otherwise serve the page and then 403 every mutation and cookie-auth
// WebSocket. Hostnames are canonicalized on write now; folding here covers an
// origin list built before that.
func TestOriginCheckIsCaseInsensitive(t *testing.T) {
	cfg, _, am := newTestServerCfg(t, true)
	cfg.Origins = []string{"https://MacBook-Pro.local:8686"}
	s := New(cfg)
	cookie, _ := am.CreateSession("UA")
	const browserOrigin = "https://macbook-pro.local:8686"

	r := httptest.NewRequest("POST", "/api/tools", stringsReader(`{"name":"x","command":"y"}`))
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	r.Header.Set("Origin", browserOrigin)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("POST from lowercased own origin = %d, want 201 (body %s)", w.Code, w.Body.String())
	}

	ws := httptest.NewRequest("GET", "/ws/events", nil)
	ws.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	ws.Header.Set("Origin", browserOrigin)
	if !s.checkWSOrigin(ws) {
		t.Fatal("cookie-auth WS from lowercased own origin rejected")
	}

	foreign := httptest.NewRequest("GET", "/ws/events", nil)
	foreign.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	foreign.Header.Set("Origin", "https://evil.example")
	if s.checkWSOrigin(foreign) {
		t.Fatal("folding origins let a foreign origin through")
	}
}
