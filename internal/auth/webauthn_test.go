package auth

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
)

const testOrigin = "https://localhost:8686"

func virtualRP() (virtualwebauthn.RelyingParty, virtualwebauthn.Authenticator) {
	rp := virtualwebauthn.RelyingParty{Name: "multimux", ID: "localhost", Origin: testOrigin}
	authenticator := virtualwebauthn.NewAuthenticator()
	authenticator.Options.UserHandle = []byte("multimux-owner")
	return rp, authenticator
}

// register drives a full registration ceremony against Manager m.
func register(t *testing.T, m *Manager, rp virtualwebauthn.RelyingParty,
	authenticator *virtualwebauthn.Authenticator, keyName string) virtualwebauthn.Credential {
	t.Helper()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	creation, err := m.BeginRegistration(keyName)
	if err != nil {
		t.Fatal(err)
	}
	optsJSON, _ := json.Marshal(creation)
	attOpts, err := virtualwebauthn.ParseAttestationOptions(string(optsJSON))
	if err != nil {
		t.Fatal(err)
	}
	attResp := virtualwebauthn.CreateAttestationResponse(rp, *authenticator, cred, *attOpts)

	req := httptest.NewRequest("POST", "/api/auth/setup/finish", bytes.NewReader([]byte(attResp)))
	if err := m.FinishRegistration(keyName, req); err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	authenticator.AddCredential(cred)
	return cred
}

func TestRegisterThenLogin(t *testing.T) {
	m, st := testManager(t)
	rp, authenticator := virtualRP()
	if err := m.SetUserName("jon"); err != nil {
		t.Fatal(err)
	}
	register(t, m, rp, &authenticator, "laptop")

	if pending, _ := m.SetupPending(); pending {
		t.Fatal("still setup-pending after registration")
	}
	creds, _ := st.ListCredentials()
	if len(creds) != 1 || creds[0].Name != "laptop" {
		t.Fatalf("stored creds = %+v", creds)
	}

	assertion, err := m.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	optsJSON, _ := json.Marshal(assertion)
	asrtOpts, err := virtualwebauthn.ParseAssertionOptions(string(optsJSON))
	if err != nil {
		t.Fatal(err)
	}
	asrtResp := virtualwebauthn.CreateAssertionResponse(rp, authenticator, authenticator.Credentials[0], *asrtOpts)
	req := httptest.NewRequest("POST", "/api/auth/login/finish", bytes.NewReader([]byte(asrtResp)))
	if err := m.FinishLogin(req); err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
}

func TestLoginWithUnknownCredentialFails(t *testing.T) {
	m, _ := testManager(t)
	rp, authenticator := virtualRP()
	m.SetUserName("jon")
	register(t, m, rp, &authenticator, "laptop")

	// A different authenticator (unregistered key) must fail the assertion.
	rogue := virtualwebauthn.NewAuthenticator()
	rogue.Options.UserHandle = []byte("multimux-owner")
	rogueCred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	rogue.AddCredential(rogueCred)

	assertion, _ := m.BeginLogin()
	optsJSON, _ := json.Marshal(assertion)
	asrtOpts, _ := virtualwebauthn.ParseAssertionOptions(string(optsJSON))
	asrtResp := virtualwebauthn.CreateAssertionResponse(rp, rogue, rogueCred, *asrtOpts)
	req := httptest.NewRequest("POST", "/api/auth/login/finish", bytes.NewReader([]byte(asrtResp)))
	if err := m.FinishLogin(req); err == nil {
		t.Fatal("login with unregistered credential succeeded")
	}
}

// beginLogin starts a login ceremony and returns the options a browser would
// hand to its authenticator.
func beginLogin(t *testing.T, m *Manager) virtualwebauthn.AssertionOptions {
	t.Helper()
	assertion, err := m.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	optsJSON, _ := json.Marshal(assertion)
	opts, err := virtualwebauthn.ParseAssertionOptions(string(optsJSON))
	if err != nil {
		t.Fatal(err)
	}
	return *opts
}

// finishLogin answers the ceremony behind opts with the authenticator's first
// credential.
func finishLogin(m *Manager, rp virtualwebauthn.RelyingParty,
	authenticator virtualwebauthn.Authenticator, opts virtualwebauthn.AssertionOptions) error {
	resp := virtualwebauthn.CreateAssertionResponse(rp, authenticator, authenticator.Credentials[0], opts)
	return finishLoginBody(m, resp)
}

func finishLoginBody(m *Manager, body string) error {
	return m.FinishLogin(httptest.NewRequest("POST", "/api/auth/login/finish", bytes.NewReader([]byte(body))))
}

// registered returns a Manager with one passkey, plus what is needed to log in
// with it.
func registered(t *testing.T) (*Manager, virtualwebauthn.RelyingParty, virtualwebauthn.Authenticator) {
	t.Helper()
	m, _ := testManager(t)
	rp, authenticator := virtualRP()
	if err := m.SetUserName("jon"); err != nil {
		t.Fatal(err)
	}
	register(t, m, rp, &authenticator, "laptop")
	return m, rp, authenticator
}

// Two devices (or two tabs) at the passkey prompt at once: each finish must be
// checked against its own challenge, in whichever order they complete.
func TestOverlappingLoginsBothSucceed(t *testing.T) {
	m, rp, authenticator := registered(t)
	first := beginLogin(t, m)
	second := beginLogin(t, m)
	if err := finishLogin(m, rp, authenticator, second); err != nil {
		t.Fatalf("second login: %v", err)
	}
	if err := finishLogin(m, rp, authenticator, first); err != nil {
		t.Fatalf("first login: %v", err)
	}
}

// login/finish is unauthenticated, so a stray or hostile POST to it must not
// be able to abort somebody's passkey prompt.
func TestStrayFinishKeepsPendingLogin(t *testing.T) {
	m, rp, authenticator := registered(t)
	opts := beginLogin(t, m)

	if err := finishLoginBody(m, "garbage"); err == nil {
		t.Fatal("garbage finish succeeded")
	}
	if err := finishLoginBody(m, "{}"); err == nil {
		t.Fatal("empty finish succeeded")
	}
	// Well-formed and validly signed, but for a challenge this daemon never issued.
	unknown := opts
	unknown.Challenge = bytes.Repeat([]byte{7}, 32)
	if err := finishLogin(m, rp, authenticator, unknown); err == nil {
		t.Fatal("finish for an unknown challenge succeeded")
	}

	if err := finishLogin(m, rp, authenticator, opts); err != nil {
		t.Fatalf("pending login was disturbed: %v", err)
	}
}

func TestLoginChallengeIsSingleUse(t *testing.T) {
	m, rp, authenticator := registered(t)

	opts := beginLogin(t, m)
	resp := virtualwebauthn.CreateAssertionResponse(rp, authenticator, authenticator.Credentials[0], opts)
	if err := finishLoginBody(m, resp); err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if err := finishLoginBody(m, resp); err == nil {
		t.Fatal("replayed assertion succeeded")
	}

	// A failed finish spends the challenge too.
	opts = beginLogin(t, m)
	rogue := virtualwebauthn.NewAuthenticator()
	rogue.Options.UserHandle = []byte("multimux-owner")
	rogue.AddCredential(virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2))
	if err := finishLogin(m, rp, rogue, opts); err == nil {
		t.Fatal("login with unregistered credential succeeded")
	}
	if err := finishLogin(m, rp, authenticator, opts); err == nil {
		t.Fatal("challenge survived a failed finish")
	}
}

func TestLoginChallengeExpires(t *testing.T) {
	m, rp, authenticator := registered(t)
	opts := beginLogin(t, m)

	m.mu.Lock()
	for _, s := range m.logins {
		s.Expires = time.Now().Add(-time.Second)
	}
	m.mu.Unlock()

	if err := finishLogin(m, rp, authenticator, opts); err == nil {
		t.Fatal("expired challenge was accepted")
	}
	// The next begin sweeps what expired, so abandoned prompts do not pile up.
	beginLogin(t, m)
	m.mu.Lock()
	n := len(m.logins)
	m.mu.Unlock()
	if n != 1 {
		t.Fatalf("pending logins after expiry = %d, want 1", n)
	}
}

func TestPendingLoginsAreCapped(t *testing.T) {
	m, rp, authenticator := registered(t)
	oldest := beginLogin(t, m)
	var newest virtualwebauthn.AssertionOptions
	for range maxPendingCeremonies {
		newest = beginLogin(t, m)
	}

	m.mu.Lock()
	n := len(m.logins)
	m.mu.Unlock()
	if n != maxPendingCeremonies {
		t.Fatalf("pending logins = %d, want %d", n, maxPendingCeremonies)
	}
	if err := finishLogin(m, rp, authenticator, oldest); err == nil {
		t.Fatal("the evicted login still finished")
	}
	if err := finishLogin(m, rp, authenticator, newest); err != nil {
		t.Fatalf("newest login: %v", err)
	}
}

// beginRegistration starts a registration ceremony and returns the options a
// browser would hand to its authenticator.
func beginRegistration(t *testing.T, m *Manager) virtualwebauthn.AttestationOptions {
	t.Helper()
	creation, err := m.BeginRegistration("key")
	if err != nil {
		t.Fatal(err)
	}
	optsJSON, _ := json.Marshal(creation)
	opts, err := virtualwebauthn.ParseAttestationOptions(string(optsJSON))
	if err != nil {
		t.Fatal(err)
	}
	return *opts
}

func finishRegistrationBody(m *Manager, name, body string) error {
	return m.FinishRegistration(name, httptest.NewRequest("POST", "/api/auth/register/finish", bytes.NewReader([]byte(body))))
}

func TestOverlappingRegistrationsBothSucceed(t *testing.T) {
	m, st := testManager(t)
	rp, authenticator := virtualRP()
	if err := m.SetUserName("jon"); err != nil {
		t.Fatal(err)
	}
	first := beginRegistration(t, m)
	second := beginRegistration(t, m)

	for name, opts := range map[string]virtualwebauthn.AttestationOptions{"phone": second, "laptop": first} {
		cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
		resp := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, opts)
		if err := finishRegistrationBody(m, name, resp); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	if creds, _ := st.ListCredentials(); len(creds) != 2 {
		t.Fatalf("stored creds = %+v", creds)
	}
}

func TestStrayFinishKeepsPendingRegistration(t *testing.T) {
	m, st := testManager(t)
	rp, authenticator := virtualRP()
	if err := m.SetUserName("jon"); err != nil {
		t.Fatal(err)
	}
	opts := beginRegistration(t, m)

	if err := finishRegistrationBody(m, "x", "garbage"); err == nil {
		t.Fatal("garbage finish succeeded")
	}
	unknown := opts
	unknown.Challenge = bytes.Repeat([]byte{7}, 32)
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	if err := finishRegistrationBody(m, "x",
		virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, unknown)); err == nil {
		t.Fatal("finish for an unknown challenge succeeded")
	}

	resp := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, opts)
	if err := finishRegistrationBody(m, "laptop", resp); err != nil {
		t.Fatalf("pending registration was disturbed: %v", err)
	}
	if err := finishRegistrationBody(m, "again", resp); err == nil {
		t.Fatal("replayed attestation succeeded")
	}
	if creds, _ := st.ListCredentials(); len(creds) != 1 || creds[0].Name != "laptop" {
		t.Fatalf("stored creds = %+v", creds)
	}
}
