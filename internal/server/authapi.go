package server

import (
	"net/http"
	"time"

	"github.com/jontuk/multimux/internal/auth"
)

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: token, Path: "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) handleSetupBegin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code, UserName, KeyName string }
	if err := readJSON(r, &in); err != nil || in.UserName == "" || in.KeyName == "" {
		writeJSON(w, 400, map[string]string{"error": "code, userName, keyName required"})
		return
	}
	pending, err := s.cfg.Auth.SetupPending()
	if err != nil || !pending {
		writeJSON(w, 403, map[string]string{"error": "setup already complete"})
		return
	}
	if !s.cfg.Auth.ConsumeSetupCode(in.Code) {
		writeJSON(w, 403, map[string]string{"error": "invalid or expired setup code"})
		return
	}
	if err := s.cfg.Auth.SetUserName(in.UserName); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	creation, err := s.cfg.Auth.BeginRegistration(in.KeyName)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, creation)
}

func (s *Server) handleSetupFinish(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	keyName := r.URL.Query().Get("keyName")
	if pending, err := s.cfg.Auth.SetupPending(); err != nil || !pending {
		writeJSON(w, 403, map[string]string{"error": "setup already complete"})
		return
	}
	if !s.cfg.Auth.ConsumeSetupCode(code) {
		writeJSON(w, 403, map[string]string{"error": "invalid or expired setup code"})
		return
	}
	if err := s.cfg.Auth.FinishRegistration(keyName, r); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	s.cfg.Auth.ClearSetupCode()
	token, err := s.cfg.Auth.CreateSession(r.UserAgent())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, 200, map[string]string{"status": "registered"})
}

func (s *Server) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	assertion, err := s.cfg.Auth.BeginLogin()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, assertion)
}

func (s *Server) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Auth.FinishLogin(r); err != nil {
		writeJSON(w, 401, map[string]string{"error": "login failed"})
		return
	}
	token, err := s.cfg.Auth.CreateSession(r.UserAgent())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	name, err := s.cfg.Auth.UserName()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"name": name})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = s.cfg.Auth.Logout(auth.TokenFromRequest(r))
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(204)
}

func (s *Server) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var in struct{ KeyName string }
	if err := readJSON(r, &in); err != nil || in.KeyName == "" {
		writeJSON(w, 400, map[string]string{"error": "keyName required"})
		return
	}
	creation, err := s.cfg.Auth.BeginRegistration(in.KeyName)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, creation)
}

func (s *Server) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Auth.FinishRegistration(r.URL.Query().Get("keyName"), r); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "registered"})
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	creds, err := s.cfg.Store.ListCredentials()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := []map[string]any{}
	for _, c := range creds {
		out = append(out, map[string]any{
			"id": c.ID, "name": c.Name,
			"createdAt": c.CreatedAt, "lastUsedAt": c.LastUsedAt,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	n, err := s.cfg.Store.CountCredentials()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if n <= 1 {
		writeJSON(w, 400, map[string]string{"error": "cannot delete the last passkey — use `multimux auth reset` on the machine"})
		return
	}
	if err := s.cfg.Store.DeleteCredential(r.PathValue("id")); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleListAuthSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.cfg.Store.ListAuthSessions()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := []map[string]any{}
	for _, a := range sessions {
		out = append(out, map[string]any{
			"tokenHash": a.TokenHash, "userAgent": a.UserAgent,
			"createdAt": a.CreatedAt, "expiresAt": a.ExpiresAt,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleDeleteAuthSession(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Store.DeleteAuthSession(r.PathValue("hash")); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(204)
}

// handleMintToken issues a bearer token for cross-daemon use (the /connect
// popup handoff). The caller is already authenticated (cookie).
func (s *Server) handleMintToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.cfg.Auth.CreateSession("cross-origin: " + r.UserAgent())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"token": token})
}
