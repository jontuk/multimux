package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jontuk/multimux/internal/store"
)

func TestResourceMutationsAreLoggedWithoutSensitiveValues(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	buf := captureLogs(t)

	secretCommand := "command-value-must-not-leak"
	w := do(t, s, "POST", "/api/tools", token,
		`{"name":"codex","command":"`+secretCommand+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tool = %d: %s", w.Code, w.Body.String())
	}

	secretPath := t.TempDir()
	w = do(t, s, "POST", "/api/dirs", token,
		fmt.Sprintf(`{"name":"workspace","path":%q}`, secretPath))
	if w.Code != http.StatusCreated {
		t.Fatalf("create dir = %d: %s", w.Code, w.Body.String())
	}

	secretHostname := "private-host.example"
	w = do(t, s, "PUT", "/api/settings", token,
		`{"hostname":"`+secretHostname+`","extraSans":"","port":"8686"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put settings = %d: %s", w.Code, w.Body.String())
	}

	logged := buf.String()
	for _, want := range []string{
		`"msg":"tool created"`,
		`"tool_id":`,
		`"name":"codex"`,
		`"msg":"directory created"`,
		`"name":"workspace"`,
		`"msg":"settings changed"`,
		`"keys":`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("mutation log missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{secretCommand, secretPath, secretHostname} {
		if strings.Contains(logged, secret) {
			t.Fatalf("mutation log exposed %q: %s", secret, logged)
		}
	}
}

func TestResourceUpdatesAndDeletesAreLogged(t *testing.T) {
	s, st, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	tool, _ := st.CreateTool("old", "old-command")
	dir, _ := st.CreateDir("old-dir", t.TempDir())
	buf := captureLogs(t)

	if w := do(t, s, "PUT", fmt.Sprintf("/api/tools/%d", tool.ID), token,
		`{"name":"updated","command":"new-command-must-not-leak"}`); w.Code != http.StatusOK {
		t.Fatalf("update tool = %d: %s", w.Code, w.Body.String())
	}
	if w := do(t, s, "DELETE", fmt.Sprintf("/api/tools/%d", tool.ID), token); w.Code != http.StatusNoContent {
		t.Fatalf("delete tool = %d: %s", w.Code, w.Body.String())
	}
	if w := do(t, s, "DELETE", fmt.Sprintf("/api/dirs/%d", dir.ID), token); w.Code != http.StatusNoContent {
		t.Fatalf("delete dir = %d: %s", w.Code, w.Body.String())
	}
	secretLabel := "host-label-must-not-leak"
	if w := do(t, s, "PUT", "/api/settings/appearance", token,
		`{"hostLabel":"`+secretLabel+`","accentColor":"#123456"}`); w.Code != http.StatusOK {
		t.Fatalf("put appearance = %d: %s", w.Code, w.Body.String())
	}

	logged := buf.String()
	for _, want := range []string{
		`"msg":"tool updated"`,
		`"name":"updated"`,
		`"msg":"tool deleted"`,
		`"msg":"directory deleted"`,
		`"msg":"appearance changed"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("mutation log missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{"new-command-must-not-leak", secretLabel, "#123456"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("mutation log exposed %q: %s", secret, logged)
		}
	}
}

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
	w := do(t, s, "PUT", "/api/settings", token, `{"hostname":"newname.example","extraSans":"a.ts.net","port":"8686"}`)
	if w.Code != 200 {
		t.Fatalf("put settings = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	w = do(t, s, "GET", "/api/settings", token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostname"] != "newname.example" || resp["extraSans"] != "a.ts.net" {
		t.Fatalf("settings = %v", resp)
	}

	// A registered credential exists (newTestServer registered=true), so an
	// RP-ID-changing hostname write must be refused until confirmed.
	w = do(t, s, "PUT", "/api/settings", token, `{"hostname":"other.example","extraSans":"a.ts.net","port":"8686"}`)
	if w.Code != 409 {
		t.Fatalf("unconfirmed RP change = %d, want 409: %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["rpChange"] != true {
		t.Fatalf("409 body must flag rpChange: %v", resp)
	}
	w = do(t, s, "GET", "/api/settings", token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostname"] != "newname.example" {
		t.Fatalf("refused write must change nothing: %v", resp)
	}

	w = do(t, s, "PUT", "/api/settings", token, `{"hostname":"other.example","extraSans":"a.ts.net","port":"8686","confirmRpChange":true}`)
	if w.Code != 200 {
		t.Fatalf("confirmed RP change = %d: %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["rpWarning"] != true {
		t.Fatalf("confirmed RP change must warn about passkey invalidation: %v", resp)
	}
	w = do(t, s, "GET", "/api/settings", token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostname"] != "other.example" {
		t.Fatalf("settings = %v", resp)
	}
}

func TestSettingsInvalidWritesNothing(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	if w := do(t, s, "PUT", "/api/settings", token, `{"hostname":"good.example","extraSans":"","port":"8686"}`); w.Code != 200 {
		t.Fatalf("seed settings = %d: %s", w.Code, w.Body.String())
	}
	for _, body := range []string{
		`{"hostname":"dotless","extraSans":"","port":"8686"}`,
		`{"hostname":"","extraSans":"","port":"8686"}`,
		`{"hostname":"good.example","extraSans":"","port":"notaport"}`,
		`{"hostname":"good.example","extraSans":"bad san","port":"8686"}`,
	} {
		if w := do(t, s, "PUT", "/api/settings", token, body); w.Code != 400 {
			t.Fatalf("invalid settings %s = %d, want 400: %s", body, w.Code, w.Body.String())
		}
	}
	var resp map[string]any
	w := do(t, s, "GET", "/api/settings", token)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["hostname"] != "good.example" || resp["extraSans"] != "" || resp["port"] != "8686" {
		t.Fatalf("invalid writes must change nothing: %v", resp)
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
