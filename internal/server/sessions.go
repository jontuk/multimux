package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	// Ahead/Behind count commits against the upstream branch; NoUpstream
	// marks a branch that has never been pushed anywhere.
	Ahead      int  `json:"ahead,omitempty"`
	Behind     int  `json:"behind,omitempty"`
	NoUpstream bool `json:"noUpstream,omitempty"`
}

// dirGitInfo is the per-directory git data resolved while listing sessions. It
// must stay comparable — CheckGitInfo diffs values with != to decide whether
// to broadcast.
type dirGitInfo struct {
	url string
	gitinfo.Status
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
			info.Status = gitinfo.BranchStatus(sess.Dir)
			infos[sess.Dir] = info
		}
		out = append(out, sessionJSON{
			Session:    sess,
			RepoURL:    info.url,
			Branch:     info.Branch,
			GitState:   info.State,
			Ahead:      info.Ahead,
			Behind:     info.Behind,
			NoUpstream: info.NoUpstream,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ToolID, DirID int64
		Subdir        string
	}
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
	workdir, err := resolveSubdir(dir.Path, in.Subdir)
	if err != nil {
		// The message names no path: the client supplied the subdir, and the
		// configured dir's location is not something the response should leak.
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	sess, err := s.cfg.Store.CreateSession(tool.ID, workdir)
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
	if err := s.cfg.Tmux.CreateSession(sess.TmuxName, workdir, tool.Command); err != nil {
		// No orphan rows: roll the DB back when tmux fails.
		_ = s.cfg.Store.DeleteSession(sess.ID)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Recorded only here, once tmux has really started: resolveSubdir rejects
	// bad subdirs above and a tmux failure rolls the row back, so anything that
	// reaches this line is a subdir worth suggesting again. A failed history
	// write is logged and dropped — the session exists and the response is
	// already a success.
	if err := s.cfg.Store.RecordSubdir(dir.ID, in.Subdir); err != nil {
		slog.Warn("subdir history not recorded", "directory_id", dir.ID, "error", err)
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

// resolveSubdir extends a configured directory with a client-supplied relative
// path. The configured dirs are the whole allow-list for where sessions may
// start, so the result must stay inside base: the subdir is checked after
// cleaning and after resolving symlinks, which stops both `../..` and a symlink
// inside base pointing out of it. The directory must already exist — a launch
// never creates one.
func resolveSubdir(base, subdir string) (string, error) {
	subdir = strings.TrimSpace(subdir)
	if subdir == "" {
		return base, nil
	}
	if filepath.IsAbs(subdir) {
		return "", errors.New("subdirectory must be relative")
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", errors.New("directory is unavailable")
	}
	full, err := filepath.EvalSymlinks(filepath.Join(realBase, subdir))
	if err != nil {
		return "", errors.New("subdirectory does not exist")
	}
	if full != realBase && !strings.HasPrefix(full, realBase+string(filepath.Separator)) {
		return "", errors.New("subdirectory must stay inside the selected directory")
	}
	if info, err := os.Stat(full); err != nil || !info.IsDir() {
		return "", errors.New("subdirectory does not exist")
	}
	return full, nil
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
	// An already-gone session (reboot) counts as success inside KillSession;
	// any error here is real, and marking the row dead anyway would orphan a
	// live tmux session with no UI handle to it.
	if err := s.cfg.Tmux.KillSession(sess.TmuxName); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
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

// maxSessionLabel caps a session's display label. Tile headers are narrow;
// past this the label crowds out the directory and branch.
const maxSessionLabel = 64

// handleRenameSession sets a session's display label ("" clears it). The label
// is cosmetic: tmux_name stays mm-{id}, so attach, Reconcile, and the
// orphan-replace path in handleCreateSession are all unaffected. Dead sessions
// are renameable too — their tiles stay on screen until dismissed.
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	var in struct{ Label string }
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	label := strings.TrimSpace(in.Label)
	for _, c := range label {
		if unicode.IsControl(c) {
			writeJSON(w, 400, map[string]string{"error": "label must not contain control characters"})
			return
		}
	}
	if utf8.RuneCountInString(label) > maxSessionLabel {
		writeJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("label must be %d characters or fewer", maxSessionLabel),
		})
		return
	}
	err = s.cfg.Store.SetSessionLabel(id, label)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	sess, err := s.cfg.Store.GetSession(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// The label is user text, like a directory path: log that it changed, not
	// what it says.
	slog.Info("session renamed", "session_id", sess.ID, "labelled", label != "")
	s.broadcast("session_renamed", sess)
	writeJSON(w, 200, sess)
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
		seen[sess.Dir] = dirGitInfo{Status: gitinfo.BranchStatus(sess.Dir)}
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
