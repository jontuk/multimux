package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("login/finish = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMintBearerToken(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
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
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 400 {
		t.Fatalf("deleting last credential = %d, want 400 (lockout guard)", rec.Code)
	}
}
