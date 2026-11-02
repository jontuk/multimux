package server

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Image paste/drop into a terminal. A terminal paste carries text only, so a
// program that accepts images (Claude Code's Ctrl+V) reads the OS clipboard of
// the machine it runs on — which, for a tile on a remote daemon, is not the
// machine the user copied on. Instead the browser uploads the image to the
// daemon that owns the session and pastes the saved file's path, which such
// programs accept as an image reference.

const (
	uploadsPath = "/api/uploads"
	// maxUploadBytes fits a full-resolution Retina screenshot PNG with room
	// to spare. Only this route gets more than limitBody's 1 MiB.
	maxUploadBytes = 32 << 20
	// uploadTTL is how long a pasted image stays on disk. Long enough for the
	// program it was pasted into to read it; the sweep makes it disappear
	// without anyone having to remember.
	uploadTTL = 24 * time.Hour
)

// uploadExt maps each accepted image type to the extension the saved file
// gets — tools that take an image path decide by extension.
var uploadExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// isUploadContentType reports whether ct is one of the raw image bodies the
// upload route accepts. csrfGate admits these for that route only: like
// application/json, none is a CORS-safelisted request content type, so a
// cross-origin page cannot send one without a preflight, and the preflight
// never grants credentials.
func isUploadContentType(path, ct string) bool {
	_, ok := uploadExt[ct]
	return ok && path == uploadsPath
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.cfg.UploadDir == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "uploads disabled"})
		return
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if _, ok := uploadExt[ct]; !ok {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "expected png, jpeg, gif, or webp image"})
		return
	}
	data, err := io.ReadAll(r.Body)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "image larger than 32 MiB"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read body"})
		return
	}
	// Trust the bytes, not the header: the extension decides how the reading
	// program treats the file.
	sniffed, _, _ := mime.ParseMediaType(http.DetectContentType(data))
	ext, ok := uploadExt[sniffed]
	if !ok {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "body is not a png, jpeg, gif, or webp image"})
		return
	}
	if err := os.MkdirAll(s.cfg.UploadDir, 0o700); err != nil {
		slog.Error("upload dir", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not store image"})
		return
	}
	name := "paste-" + time.Now().UTC().Format("20060102-150405") + "-*" + ext
	f, err := os.CreateTemp(s.cfg.UploadDir, name) // 0600
	if err != nil {
		slog.Error("upload create", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not store image"})
		return
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(f.Name())
		slog.Error("upload write", "err", werr)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not store image"})
		return
	}
	path, err := filepath.Abs(f.Name())
	if err != nil {
		path = f.Name()
	}
	writeJSON(w, http.StatusCreated, map[string]string{"path": path})
}

// sweepUploads removes pasted images older than uploadTTL. Only files this
// daemon names (paste-*) are touched, so a stray file someone put in the dir
// survives.
func (s *Server) sweepUploads(now time.Time) {
	if s.cfg.UploadDir == "" {
		return
	}
	entries, err := os.ReadDir(s.cfg.UploadDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Error("upload sweep", "err", err)
		}
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), "paste-") {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < uploadTTL {
			continue
		}
		if err := os.Remove(filepath.Join(s.cfg.UploadDir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Error("upload sweep", "err", err)
		}
	}
}
