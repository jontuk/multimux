package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// defaultThemeColor matches the static theme used before per-host theming and
// is the fallback whenever the accent_color setting is unset or malformed.
const defaultThemeColor = "#18142d"

// iconTemplate is the app icon SVG with the background rect fill left as a
// {{bg}} placeholder so each host can tint it with its accent color. Kept in
// sync with web/public/icon.svg (only the rect fill is parameterized).
const iconTemplate = `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512">
  <rect width="512" height="512" rx="96" fill="{{bg}}" />
  <g stroke="#ffffff" stroke-width="1" opacity="0.05">
    <path d="M128 0V512 M256 0V512 M384 0V512 M0 128H512 M0 256H512 M0 384H512" />
  </g>
  <text x="49%" y="48%" dominant-baseline="central" text-anchor="middle"
        font-family="Menlo, 'DejaVu Sans Mono', monospace" font-weight="700"
        font-size="200" fill="#e5e8ed" letter-spacing="-6">~mm</text>
</svg>
`

// accentOrDefault returns the host's accent_color setting when it is a valid
// #rrggbb value, otherwise the default theme color.
func (s *Server) accentOrDefault() string {
	accent, _ := s.cfg.Store.GetSetting("accent_color")
	if !accentColorRe.MatchString(accent) {
		return defaultThemeColor
	}
	return accent
}

// handleManifest serves a per-host PWA manifest so each daemon installs as a
// distinct app: name from hostLabel, theme from the accent setting, and a
// stable id/scope so the browser keys the install to this origin.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	label := s.hostLabel()
	m := map[string]any{
		"name":             label,
		"short_name":       label,
		"id":               "/",
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"background_color": defaultThemeColor,
		"theme_color":      s.accentOrDefault(),
		"icons": []map[string]string{
			{"src": "/icon.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "any maskable"},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(m)
}

// handleIconSVG serves the app icon tinted with the host's accent color so
// installed PWAs are distinguishable on the home screen / dock.
func (s *Server) handleIconSVG(w http.ResponseWriter, r *http.Request) {
	svg := strings.Replace(iconTemplate, "{{bg}}", s.accentOrDefault(), 1)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, svg)
}
