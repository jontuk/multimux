package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jontuk/multimux/internal/gitinfo"
	"github.com/jontuk/multimux/internal/store"
)

// sessionJSON is a store.Session enriched with data derived from the session's
// directory at read time.
type sessionJSON struct {
	store.Session
	RepoURL  string `json:"repoUrl,omitempty"`
	Branch   string `json:"branch,omitempty"`
	GitState string `json:"gitState,omitempty"`
}

// dirGitInfo is the per-directory git data resolved while listing sessions.
type dirGitInfo struct {
	url, branch, state string
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.cfg.Store.ListSessions()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := make([]sessionJSON, 0, len(sessions))
	// The same dir often backs several sessions; resolve each dir once.
	infos := map[string]dirGitInfo{}
	for _, sess := range sessions {
		info, ok := infos[sess.Dir]
		if !ok {
			info.url = gitinfo.RepoWebURL(sess.Dir)
			info.branch, info.state = gitinfo.BranchStatus(sess.Dir)
			infos[sess.Dir] = info
		}
		out = append(out, sessionJSON{Session: sess, RepoURL: info.url, Branch: info.branch, GitState: info.state})
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
	// A tmux session may already hold this name without a backing DB row —
	// left over from a wiped DB or a failed kill. No row means it is
	// unreachable from the UI, so replace it rather than fail on the name.
	replacedOrphan := false
	if s.cfg.Tmux.IsAlive(sess.TmuxName) {
		if err := s.cfg.Tmux.KillSession(sess.TmuxName); err != nil {
			_ = s.cfg.Store.DeleteSession(sess.ID)
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		replacedOrphan = true
	}
	if err := s.cfg.Tmux.CreateSession(sess.TmuxName, dir.Path, tool.Command); err != nil {
		// No orphan rows: roll the DB back when tmux fails.
		_ = s.cfg.Store.DeleteSession(sess.ID)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if replacedOrphan {
		slog.Info("orphan tmux session replaced", "tmux_name", sess.TmuxName)
	}
	slog.Info("session created",
		"session_id", sess.ID,
		"tmux_name", sess.TmuxName,
		"tool_id", tool.ID,
		"directory_id", dir.ID)
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
	slog.Info("session killed", "session_id", sess.ID, "tmux_name", sess.TmuxName)
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
	slog.Info("session dismissed", "session_id", sess.ID, "tmux_name", sess.TmuxName)
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
	slog.Info("layout changed")
	s.broadcast("layout_changed", nil)
	w.WriteHeader(204)
}

// Reconcile marks DB-running sessions whose tmux session no longer exists as
// dead. Called at startup and periodically (Task 17). Each pass takes one
// tmux listing and checks the DB rows against it — foreign sessions in that
// listing are never touched, only membership of multimux-owned names is
// consulted. A listing error (unlike "no server running", which ListSessions
// maps to an empty list) confirms nothing, so the pass aborts before marking
// anything dead: one transient tmux failure must not kill live rows.
func (s *Server) Reconcile() ([]store.Session, error) {
	sessions, err := s.cfg.Store.ListSessions()
	if err != nil {
		return nil, err
	}
	names, err := s.cfg.Tmux.ListSessions()
	if err != nil {
		return nil, err
	}
	alive := make(map[string]bool, len(names))
	for _, name := range names {
		alive[name] = true
	}
	var newlyDead []store.Session
	now := time.Now()
	for _, sess := range sessions {
		if sess.Status != "running" || alive[sess.TmuxName] {
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
		slog.Info("session died", "session_id", sess.ID, "tmux_name", sess.TmuxName)
		s.broadcast("session_died", sess)
	}
	return newlyDead, nil
}

// CheckGitInfo recomputes branch and working-tree state for every running
// session's dir and broadcasts git_changed when any of it differs from the
// previous check, prompting clients to refetch the session list. The first
// check only records a baseline. Called from the maintenance ticker goroutine
// only, so gitSeen needs no locking.
func (s *Server) CheckGitInfo() error {
	sessions, err := s.cfg.Store.ListSessions()
	if err != nil {
		return err
	}
	seen := map[string]dirGitInfo{}
	for _, sess := range sessions {
		if sess.Status != "running" {
			continue
		}
		if _, ok := seen[sess.Dir]; ok {
			continue
		}
		var info dirGitInfo
		info.branch, info.state = gitinfo.BranchStatus(sess.Dir)
		seen[sess.Dir] = info
	}
	changed := false
	if s.gitSeen != nil {
		for dir, info := range seen {
			if prev, ok := s.gitSeen[dir]; !ok || prev != info {
				changed = true
				break
			}
		}
	}
	s.gitSeen = seen
	if changed {
		s.broadcast("git_changed", nil)
	}
	return nil
}

// broadcast fans a session/layout event out to every connected /ws/events
// subscriber via the hub.
func (s *Server) broadcast(eventType string, payload any) {
	s.hub.Broadcast(eventType, payload)
}
