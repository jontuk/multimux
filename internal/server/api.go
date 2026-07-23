package server

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

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
