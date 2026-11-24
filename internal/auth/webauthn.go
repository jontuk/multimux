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

const (
	// ceremonyTTL is how long a begun ceremony may wait for its finish. It
	// matches the timeout go-webauthn hands the browser, after which the
	// passkey prompt has given up anyway.
	ceremonyTTL = 5 * time.Minute

	// maxPendingCeremonies bounds each ceremonies map. login/begin is
	// unauthenticated, so without a cap anyone who can reach the port could
	// grow the map for a whole ceremonyTTL. One person has a handful of tabs
	// and devices at a prompt at most; 64 is far beyond that and still only a
	// few KB.
	maxPendingCeremonies = 64
)

// ceremonies holds the WebAuthn ceremonies that have begun and not finished,
// keyed by challenge so overlapping ones (two devices, two tabs, a connect
// popup) each verify against their own. Guard with Manager.mu.
type ceremonies map[string]*webauthn.SessionData

// add records a begun ceremony, dropping the expired ones and, at the cap, the
// oldest. A flood of begins can therefore still push out a real prompt; that
// is the price of the bound, and the user's retry gets a fresh challenge.
func (c ceremonies) add(session *webauthn.SessionData) {
	now := time.Now()
	// go-webauthn only sets Expires when its timeouts are enforced. Setting it
	// here also makes the library refuse a late finish.
	if session.Expires.IsZero() {
		session.Expires = now.Add(ceremonyTTL)
	}
	c.prune(now)
	for len(c) >= maxPendingCeremonies {
		oldest := ""
		for challenge, s := range c {
			if oldest == "" || s.Expires.Before(c[oldest].Expires) {
				oldest = challenge
			}
		}
		delete(c, oldest)
	}
	c[session.Challenge] = session
}

// take removes and returns the ceremony that issued challenge, or nil. A
// challenge is spent by the first finish that presents it, pass or fail; a
// finish presenting anything else leaves every pending ceremony alone.
func (c ceremonies) take(challenge string) *webauthn.SessionData {
	c.prune(time.Now())
	session := c[challenge]
	delete(c, challenge)
	return session
}

func (c ceremonies) prune(now time.Time) {
	for challenge, s := range c {
		if s.Expires.Before(now) {
			delete(c, challenge)
		}
	}
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
	m.regs.add(session)
	m.mu.Unlock()
	return creation, nil
}

// FinishRegistration verifies the attestation response in r and stores the
// new credential under the given human-readable name.
func (m *Manager) FinishRegistration(name string, r *http.Request) error {
	// Parse before touching any pending ceremony: the challenge in the body
	// says which one this finish belongs to, and a body without a known one
	// must not cost anybody their passkey prompt.
	parsed, err := protocol.ParseCredentialCreationResponse(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	session := m.regs.take(parsed.Response.CollectedClientData.Challenge)
	m.mu.Unlock()
	if session == nil {
		return errors.New("auth: no registration in progress")
	}
	o, err := m.loadOwner()
	if err != nil {
		return err
	}
	cred, err := m.web.CreateCredential(o, *session, parsed)
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
	m.logins.add(session)
	m.mu.Unlock()
	return assertion, nil
}

// FinishLogin verifies the assertion in r and persists the updated credential
// (sign count) on success.
func (m *Manager) FinishLogin(r *http.Request) error {
	// Parse first, as in FinishRegistration. This route is unauthenticated, so
	// it matters most here: a stray POST must not abort a pending login.
	parsed, err := protocol.ParseCredentialRequestResponse(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	session := m.logins.take(parsed.Response.CollectedClientData.Challenge)
	m.mu.Unlock()
	if session == nil {
		return errors.New("auth: no login in progress")
	}
	cred, err := m.web.ValidateDiscoverableLogin(
		func(rawID, userHandle []byte) (webauthn.User, error) {
			return m.loadOwner()
		}, *session, parsed)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return m.store.UpdateCredentialData(base64.RawURLEncoding.EncodeToString(cred.ID), data)
}
