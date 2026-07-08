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

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
