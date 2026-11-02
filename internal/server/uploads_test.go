package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jontuk/multimux/internal/auth"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newUploadServer(t *testing.T) (*Server, *auth.Manager, string) {
	t.Helper()
	cfg, _, am := newTestServerCfg(t, true)
	cfg.UploadDir = filepath.Join(t.TempDir(), "uploads")
	return New(cfg), am, cfg.UploadDir
}

func upload(s *Server, body []byte, ct string, setup func(r *http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/uploads", bytes.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	setup(r)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestUploadSavesImageAndReturnsPath(t *testing.T) {
	s, am, dir := newUploadServer(t)
	token, _ := am.CreateSession("UA")
	img := pngBytes(t)
	w := upload(s, img, "image/png", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) })
	if w.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", w.Code, w.Body.String())
	}
	var out struct{ Path string }
	json.Unmarshal(w.Body.Bytes(), &out)
	if filepath.Dir(out.Path) != dir || !strings.HasSuffix(out.Path, ".png") || !filepath.IsAbs(out.Path) {
		t.Fatalf("path = %q, want absolute .png in %s", out.Path, dir)
	}
	got, err := os.ReadFile(out.Path)
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("saved file mismatch: err=%v", err)
	}
	info, _ := os.Stat(out.Path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

// The extension comes from the bytes, so a mislabeled body can't pick it.
func TestUploadExtensionFollowsContent(t *testing.T) {
	s, am, _ := newUploadServer(t)
	token, _ := am.CreateSession("UA")
	w := upload(s, pngBytes(t), "image/jpeg", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) })
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `.png"`) {
		t.Fatalf("upload = %d %s, want .png", w.Code, w.Body.String())
	}
}

func TestUploadRejects(t *testing.T) {
	s, am, dir := newUploadServer(t)
	token, _ := am.CreateSession("UA")
	cookie, _ := am.CreateSession("UA")
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	tests := []struct {
		name  string
		body  []byte
		ct    string
		setup func(r *http.Request)
		want  int
	}{
		{"unauthenticated", pngBytes(t), "image/png", func(*http.Request) {}, 401},
		{"non-image content type", pngBytes(t), "text/plain", bearer, 415},
		{"image header, non-image bytes", []byte("#!/bin/sh\necho hi\n"), "image/png", bearer, 415},
		{"too large", append(pngBytes(t), make([]byte, maxUploadBytes)...), "image/png", bearer, 413},
		{"cookie from foreign origin", pngBytes(t), "image/png", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
			r.Header.Set("Origin", "https://evil.example")
		}, 403},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if w := upload(s, tc.body, tc.ct, tc.setup); w.Code != tc.want {
				t.Fatalf("= %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("rejected uploads left %d files", len(entries))
	}
}

func TestUploadCookieOwnOriginAllowed(t *testing.T) {
	s, am, _ := newUploadServer(t)
	cookie, _ := am.CreateSession("UA")
	w := upload(s, pngBytes(t), "image/png", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		r.Header.Set("Origin", "https://localhost:8686")
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("= %d %s", w.Code, w.Body.String())
	}
}

// Image content types are an upload-route exception only.
func TestCSRFGateImageTypeOnlyForUploads(t *testing.T) {
	s, am, _ := newUploadServer(t)
	cookie, _ := am.CreateSession("UA")
	r := httptest.NewRequest("POST", "/api/tools", bytes.NewReader(pngBytes(t)))
	r.Header.Set("Content-Type", "image/png")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	r.Header.Set("Origin", "https://localhost:8686")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("image body to /api/tools = %d, want 415", w.Code)
	}
}

func TestUploadDisabledWithoutDir(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	w := upload(s, pngBytes(t), "image/png", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) })
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("= %d, want 503", w.Code)
	}
}

func TestSweepUploads(t *testing.T) {
	s, _, dir := newUploadServer(t)
	os.MkdirAll(dir, 0o700)
	now := time.Now()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o600)
		os.Chtimes(p, now.Add(-age), now.Add(-age))
		return p
	}
	old := write("paste-old.png", uploadTTL+time.Minute)
	fresh := write("paste-fresh.png", time.Minute)
	foreign := write("notes.txt", uploadTTL+time.Minute)
	s.sweepUploads(now)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expired paste survived sweep")
	}
	for _, p := range []string{fresh, foreign} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed: %v", filepath.Base(p), err)
		}
	}
}
