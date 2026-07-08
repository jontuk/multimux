package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/jontuk/multimux/internal/store"
)

// owner is the single multimux user. Its WebAuthn user handle is a fixed
// byte string — there is exactly one account per daemon.
type owner struct {
	name  string
	creds []webauthn.Credential
}

var ownerHandle = []byte("multimux-owner")

func (o *owner) WebAuthnID() []byte                         { return ownerHandle }
func (o *owner) WebAuthnName() string                       { return o.name }
func (o *owner) WebAuthnDisplayName() string                { return o.name }
func (o *owner) WebAuthnCredentials() []webauthn.Credential { return o.creds }

func (m *Manager) initWebAuthn() error {
	w, err := webauthn.New(&webauthn.Config{
		RPID:          m.rpID,
		RPDisplayName: "multimux",
		RPOrigins:     m.origins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
	})
	if err != nil {
		return err
	}
	m.web = w
	return nil
}

func (m *Manager) SetUserName(name string) error {
	return m.store.SetSetting("user_name", name)
}

func (m *Manager) UserName() (string, error) {
	return m.store.GetSetting("user_name")
}

func (m *Manager) loadOwner() (*owner, error) {
	name, err := m.UserName()
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = "owner"
	}
	stored, err := m.store.ListCredentials()
	if err != nil {
		return nil, err
	}
	o := &owner{name: name}
	for _, c := range stored {
		var wc webauthn.Credential
		if err := json.Unmarshal(c.Data, &wc); err != nil {
			return nil, err
		}
		o.creds = append(o.creds, wc)
	}
	return o, nil
}

// BeginRegistration starts a passkey registration ceremony.
func (m *Manager) BeginRegistration(name string) (*protocol.CredentialCreation, error) {
	o, err := m.loadOwner()
	if err != nil {
		return nil, err
	}
	creation, session, err := m.web.BeginRegistration(o)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.regSession = session
	m.mu.Unlock()
	return creation, nil
}

// FinishRegistration verifies the attestation response in r and stores the
// new credential under the given human-readable name.
func (m *Manager) FinishRegistration(name string, r *http.Request) error {
	m.mu.Lock()
	session := m.regSession
	m.regSession = nil
	m.mu.Unlock()
	if session == nil {
		return errors.New("auth: no registration in progress")
	}
	o, err := m.loadOwner()
	if err != nil {
		return err
	}
	cred, err := m.web.FinishRegistration(o, *session, r)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := m.store.AddCredential(store.Credential{
		ID:         base64.RawURLEncoding.EncodeToString(cred.ID),
		Name:       name,
		Data:       data,
		CreatedAt:  now,
		LastUsedAt: now,
	}); err != nil {
		return err
	}
	m.ClearSetupCode()
	return nil
}

// BeginLogin starts a discoverable (usernameless) login ceremony.
func (m *Manager) BeginLogin() (*protocol.CredentialAssertion, error) {
	assertion, session, err := m.web.BeginDiscoverableLogin()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.loginSession = session
	m.mu.Unlock()
	return assertion, nil
}

// FinishLogin verifies the assertion in r and persists the updated credential
// (sign count) on success.
func (m *Manager) FinishLogin(r *http.Request) error {
	m.mu.Lock()
	session := m.loginSession
	m.loginSession = nil
	m.mu.Unlock()
	if session == nil {
		return errors.New("auth: no login in progress")
	}
	cred, err := m.web.FinishDiscoverableLogin(
		func(rawID, userHandle []byte) (webauthn.User, error) {
			return m.loadOwner()
		}, *session, r)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return m.store.UpdateCredentialData(base64.RawURLEncoding.EncodeToString(cred.ID), data)
}
