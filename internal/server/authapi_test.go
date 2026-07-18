package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"
)

const testOrigin = "https://localhost:8686"

// setupViaHTTP drives the full first-run setup ceremony over the HTTP API and
// returns the session cookie value plus the virtual authenticator.
func setupViaHTTP(t *testing.T, s *Server, code string) (string, virtualwebauthn.RelyingParty, virtualwebauthn.Authenticator) {
	t.Helper()
	rp := virtualwebauthn.RelyingParty{Name: "multimux", ID: "localhost", Origin: testOrigin}
	authenticator := virtualwebauthn.NewAuthenticator()
	authenticator.Options.UserHandle = []byte("multimux-owner")
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	w := do(t, s, "POST", "/api/auth/setup/begin", "", `{"code":"`+code+`","userName":"jon","keyName":"laptop"}`)
	if w.Code != 200 {
		t.Fatalf("setup/begin = %d: %s", w.Code, w.Body.String())
	}
	attOpts, err := virtualwebauthn.ParseAttestationOptions(w.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	attResp := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, *attOpts)

	r := httptest.NewRequest("POST", "/api/auth/setup/finish?code="+code+"&keyName=laptop", bytes.NewReader([]byte(attResp)))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("setup/finish = %d: %s", rec.Code, rec.Body.String())
	}
	authenticator.AddCredential(cred)

	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mm_session" {
			cookie = c.Value
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("cookie flags wrong: %+v", c)
			}
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie set")
	}
	return cookie, rp, authenticator
}

func TestSetupThenLoginFlow(t *testing.T) {
	s, _, am := newTestServer(t, false)
	code, _ := am.NewSetupCode()
	buf := captureLogs(t)

	// Wrong code rejected.
	if w := do(t, s, "POST", "/api/auth/setup/begin", "", `{"code":"NOPE99","userName":"jon","keyName":"k"}`); w.Code != 403 {
		t.Fatalf("bad code = %d, want 403", w.Code)
	}

	cookie, rp, authenticator := setupViaHTTP(t, s, code)

	// Cookie works on the API.
	r := httptest.NewRequest("GET", "/api/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: "mm_session", Value: cookie})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("me = %d", rec.Code)
	}
	var me map[string]string
	json.Unmarshal(rec.Body.Bytes(), &me)
	if me["name"] != "jon" {
		t.Fatalf("me = %v", me)
	}

	// Fresh login ceremony.
	w := do(t, s, "POST", "/api/auth/login/begin", "")
	if w.Code != 200 {
		t.Fatalf("login/begin = %d", w.Code)
	}
	asrtOpts, err := virtualwebauthn.ParseAssertionOptions(w.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	asrtResp := virtualwebauthn.CreateAssertionResponse(rp, authenticator, authenticator.Credentials[0], *asrtOpts)
	r = httptest.NewRequest("POST", "/api/auth/login/finish", bytes.NewReader([]byte(asrtResp)))
	r.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("login/finish = %d: %s", rec.Code, rec.Body.String())
	}

	logged := buf.String()
	for _, want := range []string{
		`"msg":"setup completed"`,
		`"msg":"passkey registered"`,
		`"key_name":"laptop"`,
		`"msg":"login succeeded"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("auth log missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{code, cookie, "jon"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("auth log exposed %q: %s", secret, logged)
		}
	}
}

func TestMintBearerToken(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	buf := captureLogs(t)
	w := do(t, s, "POST", "/api/auth/token", token)
	if w.Code != 200 {
		t.Fatalf("token = %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["token"] == "" || resp["token"] == token {
		t.Fatalf("bad minted token: %v", resp)
	}
	if w := do(t, s, "GET", "/api/tools", resp["token"]); w.Code != 200 {
		t.Fatalf("minted token unusable: %d", w.Code)
	}
	logged := buf.String()
	if !strings.Contains(logged, `"msg":"bearer session minted"`) {
		t.Fatalf("bearer session event missing: %s", logged)
	}
	for _, secret := range []string{token, resp["token"]} {
		if strings.Contains(logged, secret) {
			t.Fatalf("auth log exposed token %q: %s", secret, logged)
		}
	}
}

func TestLogoutAndAuthSessionRevocationAreLoggedWithoutIdentifiers(t *testing.T) {
	s, st, am := newTestServer(t, true)
	primary, _ := am.CreateSession("primary-agent-must-not-leak")
	_, _ = am.CreateSession("secondary-agent-must-not-leak")
	sessions, err := st.ListAuthSessions()
	if err != nil || len(sessions) != 2 {
		t.Fatalf("auth sessions = %v, %v", sessions, err)
	}
	victimHash := sessions[1].TokenHash
	buf := captureLogs(t)

	if w := do(t, s, "DELETE", "/api/auth/sessions/"+victimHash, primary); w.Code != http.StatusNoContent {
		t.Fatalf("revoke auth session = %d: %s", w.Code, w.Body.String())
	}
	if w := do(t, s, "POST", "/api/auth/logout", primary); w.Code != http.StatusNoContent {
		t.Fatalf("logout = %d: %s", w.Code, w.Body.String())
	}

	logged := buf.String()
	for _, want := range []string{`"msg":"auth session revoked"`, `"msg":"logout completed"`} {
		if !strings.Contains(logged, want) {
			t.Fatalf("auth log missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{primary, victimHash, "primary-agent", "secondary-agent"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("auth log exposed %q: %s", secret, logged)
		}
	}
}

func TestPasskeyRegistrationAndDeletionAreLoggedWithoutCredentialID(t *testing.T) {
	s, st, am := newTestServer(t, false)
	code, _ := am.NewSetupCode()
	token, rp, authenticator := setupViaHTTP(t, s, code)
	buf := captureLogs(t)

	w := do(t, s, "POST", "/api/auth/register/begin", token, `{"keyName":"desktop"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("register begin = %d: %s", w.Code, w.Body.String())
	}
	attOpts, err := virtualwebauthn.ParseAttestationOptions(w.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	attResp := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, *attOpts)
	r := httptest.NewRequest("POST", "/api/auth/register/finish?keyName=desktop", bytes.NewReader([]byte(attResp)))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("register finish = %d: %s", rec.Code, rec.Body.String())
	}

	creds, err := st.ListCredentials()
	if err != nil || len(creds) != 2 {
		t.Fatalf("credentials = %v, %v", creds, err)
	}
	deletedID := creds[0].ID
	w = do(t, s, "DELETE", "/api/auth/credentials/"+url.PathEscape(deletedID), token)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete credential = %d: %s", w.Code, w.Body.String())
	}

	logged := buf.String()
	for _, want := range []string{
		`"msg":"passkey registered"`,
		`"key_name":"desktop"`,
		`"msg":"passkey deleted"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("passkey log missing %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, deletedID) {
		t.Fatalf("passkey log exposed credential ID %q: %s", deletedID, logged)
	}
}

func TestLastCredentialUndeletable(t *testing.T) {
	s, _, am := newTestServer(t, false)
	code, _ := am.NewSetupCode()
	cookie, _, _ := setupViaHTTP(t, s, code)
	r := httptest.NewRequest("GET", "/api/auth/credentials", nil)
	r.AddCookie(&http.Cookie{Name: "mm_session", Value: cookie})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	var creds []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &creds)
	if len(creds) != 1 {
		t.Fatalf("creds = %v", creds)
	}
	id := creds[0]["id"].(string)
	r = httptest.NewRequest("DELETE", "/api/auth/credentials/"+id, nil)
	r.AddCookie(&http.Cookie{Name: "mm_session", Value: cookie})
	r.Header.Set("Origin", "https://localhost:8686") // cookie mutations need own Origin (CSRF gate)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 400 {
		t.Fatalf("deleting last credential = %d, want 400 (lockout guard)", rec.Code)
	}
}
