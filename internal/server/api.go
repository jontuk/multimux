package server

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/jontuk/multimux/internal/config"
	"github.com/jontuk/multimux/internal/identity"
	"github.com/jontuk/multimux/internal/store"
)

var accentColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools, err := s.cfg.Store.ListTools()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if tools == nil {
		tools = []store.Tool{}
	}
	writeJSON(w, 200, tools)
}

func (s *Server) handleCreateTool(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Command string }
	if err := readJSON(r, &in); err != nil || in.Name == "" || in.Command == "" {
		writeJSON(w, 400, map[string]string{"error": "name and command required"})
		return
	}
	tool, err := s.cfg.Store.CreateTool(in.Name, in.Command)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("tool created", "tool_id", tool.ID, "name", tool.Name)
	writeJSON(w, 201, tool)
}

func (s *Server) handleUpdateTool(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	var in struct{ Name, Command string }
	if err := readJSON(r, &in); err != nil || in.Name == "" || in.Command == "" {
		writeJSON(w, 400, map[string]string{"error": "name and command required"})
		return
	}
	tool := store.Tool{ID: id, Name: in.Name, Command: in.Command}
	if err := s.cfg.Store.UpdateTool(tool); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("tool updated", "tool_id", tool.ID, "name", tool.Name)
	writeJSON(w, 200, tool)
}

func (s *Server) handleDeleteTool(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	if err := s.cfg.Store.DeleteTool(id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("tool deleted", "tool_id", id)
	w.WriteHeader(204)
}

func (s *Server) handleReorderTools(w http.ResponseWriter, r *http.Request) {
	s.reorder(w, r, "tools", s.cfg.Store.ReorderTools)
}

func (s *Server) handleReorderDirs(w http.ResponseWriter, r *http.Request) {
	s.reorder(w, r, "directories", s.cfg.Store.ReorderDirs)
}

// reorder applies a full-list ordering. A mismatched id set means the client is
// working from a stale list, which is its problem to fix, so it gets a 400.
func (s *Server) reorder(w http.ResponseWriter, r *http.Request, what string, apply func([]int64) error) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "ids required"})
		return
	}
	if err := apply(in.IDs); err != nil {
		if errors.Is(err, store.ErrOrderMismatch) {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("reordered", "what", what, "count", len(in.IDs))
	w.WriteHeader(204)
}

func (s *Server) handleListDirs(w http.ResponseWriter, r *http.Request) {
	dirs, err := s.cfg.Store.ListDirs()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if dirs == nil {
		dirs = []store.Dir{}
	}
	writeJSON(w, 200, dirs)
}

func (s *Server) handleCreateDir(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Path string }
	if err := readJSON(r, &in); err != nil || in.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name and path required"})
		return
	}
	if !filepath.IsAbs(in.Path) {
		writeJSON(w, 400, map[string]string{"error": "path must be absolute"})
		return
	}
	if info, err := os.Stat(in.Path); err != nil || !info.IsDir() {
		writeJSON(w, 400, map[string]string{"error": "path is not an existing directory"})
		return
	}
	d, err := s.cfg.Store.CreateDir(in.Name, in.Path)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("directory created", "directory_id", d.ID, "name", d.Name)
	writeJSON(w, 201, d)
}

func (s *Server) handleDeleteDir(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	if err := s.cfg.Store.DeleteDir(id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("directory deleted", "directory_id", id)
	w.WriteHeader(204)
}

// A directory's remembered subdirs. An unknown id answers with an empty list
// rather than 404: a tab's directory list can legitimately race a deletion, and
// "no history" is the right answer either way.
func (s *Server) handleListSubdirs(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	subdirs, err := s.cfg.Store.ListSubdirs(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, subdirs)
}

// childDirLimit caps one listing. A directory with thousands of children is
// not something a type-ahead list can help with, and the cap keeps a pathological
// path from turning a keystroke into a megabyte of JSON.
const childDirLimit = 300

// The immediate child directories of dir/{path}, for subdir type-ahead. `path`
// is the already-typed portion of the subdir, so the client asks once per parent
// segment and filters the last one itself.
//
// resolveSubdir does the containment work: the answer never escapes the
// configured directory, and an unreadable or missing path is an empty list
// rather than an error — type-ahead runs on every keystroke, most of which name
// nothing yet.
func (s *Server) handleListChildDirs(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	dirs, err := s.cfg.Store.ListDirs()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var base string
	for _, d := range dirs {
		if d.ID == id {
			base = d.Path
		}
	}
	if base == "" {
		writeJSON(w, 200, []string{})
		return
	}
	parent, err := resolveSubdir(base, r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, 200, []string{})
		return
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		writeJSON(w, 200, []string{})
		return
	}
	names := []string{}
	for _, e := range entries {
		if len(names) >= childDirLimit {
			break
		}
		// Symlinks report their own type, so a symlinked child needs a stat to
		// be recognised as a directory. A broken one simply drops out.
		if !e.IsDir() {
			info, err := os.Stat(filepath.Join(parent, e.Name()))
			if err != nil || !info.IsDir() {
				continue
			}
		}
		names = append(names, e.Name())
	}
	writeJSON(w, 200, names)
}

// The subdir is a query parameter, not a path segment: subdirs contain
// slashes, which ServeMux's {id} wildcard does not match.
func (s *Server) handleDeleteSubdir(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	subdir := r.URL.Query().Get("subdir")
	if subdir == "" {
		writeJSON(w, 400, map[string]string{"error": "subdir required"})
		return
	}
	if err := s.cfg.Store.DeleteSubdir(id, subdir); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	host, _ := s.cfg.Store.GetSetting("hostname")
	sans, _ := s.cfg.Store.GetSetting("extra_sans")
	port, _ := s.cfg.Store.GetSetting("port")
	writeJSON(w, 200, map[string]string{"hostname": host, "extraSans": sans, "port": port, "version": s.cfg.Version})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hostname, ExtraSans, Port string
		ConfirmRpChange           bool
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	rpChanged, err := identity.Apply(s.cfg.Store, map[string]string{
		"hostname": in.Hostname, "extra_sans": in.ExtraSans, "port": in.Port,
	}, in.ConfirmRpChange)
	var rpErr *identity.RPChangeError
	if errors.As(err, &rpErr) {
		// Changing the RP ID strands every registered passkey; require the UI
		// to confirm explicitly before anything is written.
		writeJSON(w, 409, map[string]any{"error": rpErr.Error(), "rpChange": true, "credentials": rpErr.Credentials})
		return
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("settings changed", "keys", []string{"hostname", "extra_sans", "port"})
	// rpWarning: the RP ID changed — all passkeys stop working after restart.
	writeJSON(w, 200, map[string]any{"ok": true, "rpWarning": rpChanged, "restartRequired": true})
}

// hostLabel is the display name shown in the web UI header: the user's
// host_label setting, falling back to the OS hostname.
func (s *Server) hostLabel() string {
	if label, _ := s.cfg.Store.GetSetting("host_label"); label != "" {
		return label
	}
	name, _ := os.Hostname()
	return name
}

func (s *Server) handleGetAppearance(w http.ResponseWriter, r *http.Request) {
	label, _ := s.cfg.Store.GetSetting("host_label")
	accent, _ := s.cfg.Store.GetSetting("accent_color")
	osHost, _ := os.Hostname()
	writeJSON(w, 200, map[string]string{"hostLabel": label, "accentColor": accent, "osHostname": osHost})
}

func (s *Server) handlePutAppearance(w http.ResponseWriter, r *http.Request) {
	var in struct{ HostLabel, AccentColor string }
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	if len(in.HostLabel) > 64 {
		writeJSON(w, 400, map[string]string{"error": "hostLabel must be 64 characters or fewer"})
		return
	}
	if in.AccentColor != "" && !accentColorRe.MatchString(in.AccentColor) {
		writeJSON(w, 400, map[string]string{"error": "accentColor must be #rrggbb"})
		return
	}
	for k, v := range map[string]string{"host_label": in.HostLabel, "accent_color": in.AccentColor} {
		if err := s.cfg.Store.SetSetting(k, v); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
	}
	slog.Info("appearance changed", "keys", []string{"host_label", "accent_color"})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	confirmTerminate, err := config.Bool(s.cfg.Store, config.ConfirmTerminate)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"confirmTerminate": confirmTerminate})
}

func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConfirmTerminate bool `json:"confirmTerminate"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	if err := config.Set(s.cfg.Store, config.ConfirmTerminate, strconv.FormatBool(in.ConfirmTerminate)); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("preferences changed", "keys", []string{config.ConfirmTerminate})
	// Echo the stored state so the client reconciles against the daemon.
	writeJSON(w, 200, map[string]any{"confirmTerminate": in.ConfirmTerminate})
}
