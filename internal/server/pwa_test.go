package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestManifestIsPerHostAndOpen(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	st.SetSetting("host_label", "workbox")
	st.SetSetting("accent_color", "#ff8800")

	// No token: the manifest must be reachable unauthenticated so the browser
	// can install the PWA.
	w := do(t, s, "GET", "/manifest.webmanifest", "")
	if w.Code != http.StatusOK {
		t.Fatalf("manifest = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Fatalf("content-type = %q", ct)
	}
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("bad manifest json: %v", err)
	}
	if m["name"] != "workbox" || m["short_name"] != "workbox" {
		t.Fatalf("name not from host label: %v", m["name"])
	}
	if m["theme_color"] != "#ff8800" {
		t.Fatalf("theme_color not from accent: %v", m["theme_color"])
	}
	if m["start_url"] != "/" || m["scope"] != "/" || m["id"] != "/" {
		t.Fatalf("install identity not origin-scoped: %v", m)
	}
}

func TestManifestFallsBackWhenAccentUnset(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	w := do(t, s, "GET", "/manifest.webmanifest", "")
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	if m["theme_color"] != defaultThemeColor {
		t.Fatalf("theme_color fallback = %v, want %s", m["theme_color"], defaultThemeColor)
	}
}

func TestIconTintedWithAccent(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	st.SetSetting("accent_color", "#123456")
	w := do(t, s, "GET", "/icon.svg", "")
	if w.Code != http.StatusOK {
		t.Fatalf("icon = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("content-type = %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, `fill="#123456"`) {
		t.Fatalf("icon not tinted with accent: %s", body)
	}
	if strings.Contains(body, "{{bg}}") {
		t.Fatalf("icon template placeholder not substituted")
	}
}

func TestIconRejectsBadAccentAndFallsBack(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	st.SetSetting("accent_color", "not-a-color")
	w := do(t, s, "GET", "/icon.svg", "")
	if !strings.Contains(w.Body.String(), `fill="`+defaultThemeColor+`"`) {
		t.Fatalf("bad accent not rejected: %s", w.Body.String())
	}
}
