package auth

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

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
