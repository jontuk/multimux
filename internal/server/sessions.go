package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jontuk/multimux/internal/gitinfo"
	"github.com/jontuk/multimux/internal/store"
)

// sessionJSON is a store.Session enriched with data derived from the session's
// directory at read time.
type sessionJSON struct {
	store.Session
	RepoURL string `json:"repoUrl,omitempty"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.cfg.Store.ListSessions()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := make([]sessionJSON, 0, len(sessions))
	// The same dir often backs several sessions; resolve each dir once.
	urls := map[string]string{}
	for _, sess := range sessions {
		url, ok := urls[sess.Dir]
		if !ok {
			url = gitinfo.RepoWebURL(sess.Dir)
			urls[sess.Dir] = url
		}
		out = append(out, sessionJSON{Session: sess, RepoURL: url})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var in struct{ ToolID, DirID int64 }
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	tools, err := s.cfg.Store.ListTools()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	dirs, err := s.cfg.Store.ListDirs()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var tool *store.Tool
	for i := range tools {
		if tools[i].ID == in.ToolID {
			tool = &tools[i]
		}
	}
	var dir *store.Dir
	for i := range dirs {
		if dirs[i].ID == in.DirID {
			dir = &dirs[i]
		}
	}
	if tool == nil || dir == nil {
		writeJSON(w, 400, map[string]string{"error": "unknown tool or dir"})
		return
	}
	sess, err := s.cfg.Store.CreateSession(tool.ID, dir.Path)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.Tmux.CreateSession(sess.TmuxName, dir.Path, tool.Command); err != nil {
		// No orphan rows: roll the DB back when tmux fails.
		_ = s.cfg.Store.DeleteSession(sess.ID)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("session_created", sess)
	writeJSON(w, 201, sess)
}

func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	sess, err := s.cfg.Store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Session may already be gone (reboot); killing is best-effort.
	_ = s.cfg.Tmux.KillSession(sess.TmuxName)
	if err := s.cfg.Store.SetSessionStatus(id, "dead"); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	sess.Status = "dead"
	s.broadcast("session_killed", sess)
	w.WriteHeader(204)
}

func (s *Server) handleDismissSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	sess, err := s.cfg.Store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if sess.Status == "running" {
		writeJSON(w, 409, map[string]string{"error": "session is running — kill it first"})
		return
	}
	if err := s.cfg.Store.DeleteSession(id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("session_dismissed", sess)
	w.WriteHeader(204)
}

func (s *Server) handleGetLayout(w http.ResponseWriter, r *http.Request) {
	data, err := s.cfg.Store.GetLayout()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if data == "" {
		data = "{}"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(data))
}

func (s *Server) handlePutLayout(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || len(body) == 0 {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	// The document is opaque to the daemon but is served back with a JSON
	// content type, so reject bodies that aren't JSON (including ones the
	// 64KB limit truncated mid-document).
	if !json.Valid(body) {
		writeJSON(w, 400, map[string]string{"error": "layout must be valid JSON"})
		return
	}
	if err := s.cfg.Store.SetLayout(string(body)); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.broadcast("layout_changed", nil)
	w.WriteHeader(204)
}

// Reconcile marks DB-running sessions whose tmux session no longer exists as
// dead. Called at startup and periodically (Task 17). Only sessions tracked
// by the store are considered — tmuxmgr.ListSessions returns every session on
// the tmux server, including ones outside multimux's control, but Reconcile
// never consults that list directly: it walks the DB and asks IsAlive about
// each known tmux name, so foreign sessions are never touched.
func (s *Server) Reconcile() ([]store.Session, error) {
	sessions, err := s.cfg.Store.ListSessions()
	if err != nil {
		return nil, err
	}
	var newlyDead []store.Session
	now := time.Now()
	for _, sess := range sessions {
		if sess.Status != "running" || s.cfg.Tmux.IsAlive(sess.TmuxName) {
			continue
		}
		// The DB row is inserted before the tmux session exists (the tmux name
		// derives from the row ID), so a tick landing in that window would
		// otherwise declare a session dead while it is still being created.
		if now.Sub(sess.CreatedAt) < s.reconcileGrace {
			continue
		}
		if err := s.cfg.Store.SetSessionStatus(sess.ID, "dead"); err != nil {
			return newlyDead, err
		}
		sess.Status = "dead"
		newlyDead = append(newlyDead, sess)
		s.broadcast("session_died", sess)
	}
	return newlyDead, nil
}

// broadcast fans a session/layout event out to every connected /ws/events
// subscriber via the hub.
func (s *Server) broadcast(eventType string, payload any) {
	s.hub.Broadcast(eventType, payload)
}
