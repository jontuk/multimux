// Package server wires multimux's HTTP surface: static SPA, REST API, and
// WebSockets, behind auth and setup-pending gates.
package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jontuk/multimux/internal/auth"
	"github.com/jontuk/multimux/internal/store"
	"github.com/jontuk/multimux/internal/tmuxmgr"
)

type Config struct {
	Store   *store.Store
	Auth    *auth.Manager
	Tmux    *tmuxmgr.Manager
	Arbiter *tmuxmgr.Arbiter
	WebFS   fs.FS
	Origins []string // this daemon's own origins (cookie-auth WS origin check)
	Version string
}

type Server struct {
	cfg Config
	mux *http.ServeMux
	hub *Hub

	// reconcileGrace is how old a running session row must be before Reconcile
	// may declare it dead — creation inserts the row before the tmux session
	// exists, and a reconcile tick in that window must not race it.
	reconcileGrace time.Duration

	// gitSeen is the per-dir git state as of the last CheckGitInfo tick.
	// Touched only by the maintenance ticker goroutine; nil until the baseline
	// check runs.
	gitSeen map[string]dirGitInfo
}

func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux(), hub: NewHub(), reconcileGrace: 10 * time.Second}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	// Auth ceremonies (open — they ARE the login): Task 15.
	s.mux.HandleFunc("POST /api/auth/setup/begin", s.handleSetupBegin)
	s.mux.HandleFunc("POST /api/auth/setup/finish", s.handleSetupFinish)
	s.mux.HandleFunc("POST /api/auth/login/begin", s.handleLoginBegin)
	s.mux.HandleFunc("POST /api/auth/login/finish", s.handleLoginFinish)
	s.mux.HandleFunc("GET /api/auth/me", s.handleMe)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("POST /api/auth/register/begin", s.handleRegisterBegin)
	s.mux.HandleFunc("POST /api/auth/register/finish", s.handleRegisterFinish)
	s.mux.HandleFunc("GET /api/auth/credentials", s.handleListCredentials)
	s.mux.HandleFunc("DELETE /api/auth/credentials/{id}", s.handleDeleteCredential)
	s.mux.HandleFunc("GET /api/auth/sessions", s.handleListAuthSessions)
	s.mux.HandleFunc("DELETE /api/auth/sessions/{hash}", s.handleDeleteAuthSession)
	s.mux.HandleFunc("POST /api/auth/token", s.handleMintToken)
	// REST API: Tasks 13-14. WS: Tasks 16-17.
	s.mux.HandleFunc("GET /api/tools", s.handleListTools)
	s.mux.HandleFunc("POST /api/tools", s.handleCreateTool)
	s.mux.HandleFunc("PUT /api/tools/{id}", s.handleUpdateTool)
	s.mux.HandleFunc("DELETE /api/tools/{id}", s.handleDeleteTool)
	s.mux.HandleFunc("GET /api/dirs", s.handleListDirs)
	s.mux.HandleFunc("POST /api/dirs", s.handleCreateDir)
	s.mux.HandleFunc("DELETE /api/dirs/{id}", s.handleDeleteDir)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	s.mux.HandleFunc("GET /api/settings/appearance", s.handleGetAppearance)
	s.mux.HandleFunc("PUT /api/settings/appearance", s.handlePutAppearance)
	s.mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.handleKillSession)
	s.mux.HandleFunc("POST /api/sessions/{id}/dismiss", s.handleDismissSession)
	s.mux.HandleFunc("GET /api/layout", s.handleGetLayout)
	s.mux.HandleFunc("PUT /api/layout", s.handlePutLayout)
	s.mux.HandleFunc("GET /ws/pty/{id}", s.handlePTY)
	s.mux.HandleFunc("GET /ws/events", s.handleEvents)
	s.mux.Handle("/", s.staticHandler())
}

// Handler wraps the mux in (outermost first) logging → CORS → setup gate →
// CSRF gate → auth → body cap. Static assets and /healthz and
// /api/auth/{login,setup} bypass auth.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = limitBody(h)
	h = s.authGate(h)
	h = s.csrfGate(h)
	h = s.setupGate(h)
	h = s.cors(h)
	h = logRequests(h)
	return h
}

// csrfGate blocks cross-origin cookie-carrying API mutations. CORS only stops
// the response being read — a same-site sibling origin (a/b.<tailnet>.ts.net
// share a site, so SameSite=Strict still attaches the cookie) can fire a
// no-cors POST whose side effects execute regardless. Two rules for unsafe
// /api/ methods: cookie-authenticated requests must present one of our own
// exact origins (the SPA always sends Origin on non-GET fetches), and any
// request body must be application/json, which a cross-origin caller cannot
// send without a credentialed CORS request that ACAO:* already forbids.
// Explicit bearer tokens skip the origin check — the token is attached
// deliberately, which is how cross-daemon calls work; auth then uses that
// token, never the cookie (TokenFromRequest is explicit-first), mirroring
// checkWSOrigin.
func (s *Server) csrfGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case !strings.HasPrefix(r.URL.Path, "/api/"),
			r.Method == http.MethodGet, r.Method == http.MethodHead, r.Method == http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if auth.ExplicitToken(r) == "" {
			if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
				if !slices.Contains(s.cfg.Origins, r.Header.Get("Origin")) {
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden origin"})
					return
				}
			}
		}
		if r.ContentLength != 0 { // includes -1 (unknown length): body may exist
			ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if ct != "application/json" {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "expected application/json"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request bodies so unauthenticated endpoints (the WebAuthn
// ceremonies) cannot be fed unbounded JSON. Nothing in the API legitimately
// approaches the cap — the largest payloads are attestations of a few KB.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}

func isProtected(path string) bool {
	if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/ws/") {
		return false
	}
	// Login and setup ceremonies must be reachable unauthenticated.
	for _, open := range []string{"/api/auth/login/", "/api/auth/setup/"} {
		if strings.HasPrefix(path, open) {
			return false
		}
	}
	return true
}

func (s *Server) authGate(next http.Handler) http.Handler {
	protected := s.cfg.Auth.Middleware(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isProtected(r.URL.Path) {
			protected.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// setupGate 403s everything except healthz, setup ceremonies, and static
// assets while no passkey is registered yet.
func (s *Server) setupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pending, err := s.cfg.Auth.SetupPending()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		if pending && isProtected(r.URL.Path) && !strings.HasPrefix(r.URL.Path, "/api/auth/setup/") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "setup pending"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cors: /api/ is callable from any origin WITHOUT credentials — cross-origin
// callers authenticate with bearer tokens, never cookies, so reflecting * is
// safe (see design decision on multi-host auth).
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		slog.Debug("http", "method", r.Method, "path", r.URL.Path)
	})
}

// staticHandler serves the embedded SPA with index.html fallback for client
// routes (anything without a file extension).
func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServerFS(s.cfg.WebFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := s.cfg.WebFS.Open(path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback.
		index, err := fs.ReadFile(s.cfg.WebFS, "index.html")
		if err != nil {
			http.Error(w, "web assets missing — build web/ first", http.StatusNotImplemented)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	pending, _ := s.cfg.Auth.SetupPending()
	accent, _ := s.cfg.Store.GetSetting("accent_color")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"version":      s.cfg.Version,
		"tmux":         s.cfg.Tmux.Available() == nil,
		"setupPending": pending,
		"hostLabel":    s.hostLabel(),
		"accentColor":  accent,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer io.Copy(io.Discard, r.Body)
	return json.NewDecoder(r.Body).Decode(v)
}
