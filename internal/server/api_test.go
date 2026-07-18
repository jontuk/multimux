package server

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jontuk/multimux/internal/store"
)

func TestToolsCRUDOverHTTP(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	w := do(t, s, "POST", "/api/tools", token, `{"name":"claude","command":"claude"}`)
	if w.Code != 201 {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	var tool store.Tool
	json.Unmarshal(w.Body.Bytes(), &tool)

	w = do(t, s, "PUT", fmt.Sprintf("/api/tools/%d", tool.ID), token, `{"name":"claude","command":"claude --continue"}`)
	if w.Code != 200 {
		t.Fatalf("update = %d", w.Code)
	}

	w = do(t, s, "GET", "/api/tools", token)
	var tools []store.Tool
	json.Unmarshal(w.Body.Bytes(), &tools)
	// Seed may add a shell; find ours.
	found := false
	for _, tl := range tools {
		if tl.ID == tool.ID && tl.Command == "claude --continue" {
			found = true
		}
	}
	if !found {
		t.Fatalf("updated tool not listed: %+v", tools)
	}

	if w = do(t, s, "DELETE", fmt.Sprintf("/api/tools/%d", tool.ID), token); w.Code != 204 {
		t.Fatalf("delete = %d", w.Code)
	}
}

func TestDirsValidation(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "POST", "/api/dirs", token, `{"name":"bad","path":"relative/path"}`); w.Code != 400 {
		t.Fatalf("relative path = %d, want 400", w.Code)
	}
	if w := do(t, s, "POST", "/api/dirs", token, `{"name":"gone","path":"/definitely/not/a/real/dir"}`); w.Code != 400 {
		t.Fatalf("missing dir = %d, want 400", w.Code)
	}
	body := fmt.Sprintf(`{"name":"tmp","path":%q}`, t.TempDir())
	if w := do(t, s, "POST", "/api/dirs", token, body); w.Code != 201 {
		t.Fatalf("valid dir = %d", w.Code)
	}
}

func TestSettingsRoundTripAndRPWarning(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	w := do(t, s, "PUT", "/api/settings", token, `{"hostname":"newname","extraSans":"a.ts.net","port":"8686"}`)
	if w.Code != 200 {
		t.Fatalf("put settings = %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["rpWarning"] != true {
		t.Fatalf("hostname change must warn about passkey invalidation: %v", resp)
	}
	w = do(t, s, "GET", "/api/settings", token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostname"] != "newname" || resp["extraSans"] != "a.ts.net" {
		t.Fatalf("settings = %v", resp)
	}
}

func TestAppearanceRoundTripAndHealthz(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")

	w := do(t, s, "PUT", "/api/settings/appearance", token, `{"hostLabel":"work-mac","accentColor":"#3fb950"}`)
	if w.Code != 200 {
		t.Fatalf("put appearance = %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	w = do(t, s, "GET", "/api/settings/appearance", token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostLabel"] != "work-mac" || resp["accentColor"] != "#3fb950" {
		t.Fatalf("appearance = %v", resp)
	}
	if resp["osHostname"] == "" {
		t.Fatalf("osHostname missing: %v", resp)
	}

	w = do(t, s, "GET", "/healthz", "")
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostLabel"] != "work-mac" || resp["accentColor"] != "#3fb950" {
		t.Fatalf("healthz appearance = %v", resp)
	}
}

func TestAppearanceHealthzFallsBackToOSHostname(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	w := do(t, s, "GET", "/healthz", "")
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostLabel"] == "" {
		t.Fatalf("hostLabel should default to OS hostname: %v", resp)
	}
}

func TestAppearanceValidation(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "PUT", "/api/settings/appearance", token, `{"hostLabel":"x","accentColor":"green"}`); w.Code != 400 {
		t.Fatalf("bad accent = %d, want 400", w.Code)
	}
	if w := do(t, s, "PUT", "/api/settings/appearance", token, `{"hostLabel":"x","accentColor":"#12345"}`); w.Code != 400 {
		t.Fatalf("short accent = %d, want 400", w.Code)
	}
	long := fmt.Sprintf(`{"hostLabel":%q,"accentColor":""}`, string(make([]byte, 65)))
	if w := do(t, s, "PUT", "/api/settings/appearance", token, long); w.Code != 400 {
		t.Fatalf("long label = %d, want 400", w.Code)
	}
	if w := do(t, s, "PUT", "/api/settings/appearance", token, `{"hostLabel":"","accentColor":""}`); w.Code != 200 {
		t.Fatalf("empty values = %d, want 200", w.Code)
	}
}
