package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jontuk/multimux/internal/store"
)

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
	w.WriteHeader(204)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	host, _ := s.cfg.Store.GetSetting("hostname")
	sans, _ := s.cfg.Store.GetSetting("extra_sans")
	port, _ := s.cfg.Store.GetSetting("port")
	writeJSON(w, 200, map[string]string{"hostname": host, "extraSans": sans, "port": port})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in struct{ Hostname, ExtraSans, Port string }
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad body"})
		return
	}
	prev, _ := s.cfg.Store.GetSetting("hostname")
	rpWarning := in.Hostname != "" && in.Hostname != prev
	for k, v := range map[string]string{"hostname": in.Hostname, "extra_sans": in.ExtraSans, "port": in.Port} {
		if err := s.cfg.Store.SetSetting(k, v); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
	}
	// rpWarning: changing hostname changes the WebAuthn RP ID — all passkeys
	// stop working after restart. UI must confirm loudly.
	writeJSON(w, 200, map[string]any{"ok": true, "rpWarning": rpWarning, "restartRequired": true})
}
