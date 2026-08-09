package server

import (
	"testing"
)

// newNoAuthServer builds a dev-mode server: no credentials registered, NoAuth set.
func newNoAuthServer(t *testing.T) *Server {
	t.Helper()
	cfg, _, _ := newTestServerCfg(t, false)
	cfg.NoAuth = true
	return New(cfg)
}

// Under --dev there is no passkey and no session token, so both the setup gate
// and the auth gate must stand down or every API call fails.
func TestNoAuthServesProtectedRoutes(t *testing.T) {
	s := newNoAuthServer(t)
	w := do(t, s, "GET", "/api/tools", "")
	if w.Code != 200 {
		t.Fatalf("GET /api/tools = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}

// A mutating call must work too: csrfGate only enforces its origin rule for
// cookie-authenticated requests, and NoAuth requests carry no cookie.
func TestNoAuthServesMutations(t *testing.T) {
	s := newNoAuthServer(t)
	w := do(t, s, "POST", "/api/tools", "", `{"name":"claude","command":"claude"}`)
	if w.Code != 200 && w.Code != 201 {
		t.Fatalf("POST /api/tools = %d, want 2xx (body: %s)", w.Code, w.Body.String())
	}
}

// The default build must be untouched: with no credentials the setup gate
// still 403s, and with credentials the auth gate still 401s.
func TestWithoutNoAuthGatesStillApply(t *testing.T) {
	s, _, _ := newTestServer(t, false)
	if w := do(t, s, "GET", "/api/tools", ""); w.Code != 403 {
		t.Fatalf("setup-pending GET /api/tools = %d, want 403", w.Code)
	}
	registered, _, _ := newTestServer(t, true)
	if w := do(t, registered, "GET", "/api/tools", ""); w.Code != 401 {
		t.Fatalf("unauthenticated GET /api/tools = %d, want 401", w.Code)
	}
}
