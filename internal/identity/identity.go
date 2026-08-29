// Package identity is the single validation and persistence path for the
// daemon's identity settings (hostname, extra SANs, port). The CLI --hostname
// flag and the settings API both go through Apply, so the WebAuthn RP-ID
// lockout guard cannot be bypassed and invalid or partial writes never land.
package identity

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jontuk/multimux/internal/store"
)

// CanonicalHost folds a DNS name to the form a browser will send it back in.
// Browsers lowercase the host when they serialize an Origin, and the RP ID
// hash an authenticator signs is over the lowercase effective domain, so a
// configured name that keeps its case (os.Hostname() on a Mac commonly
// returns "MacBook-Pro.local") matches nothing: the origin checks in
// internal/server reject every mutation and cookie-auth WebSocket, and
// go-webauthn's byte comparison of the RP ID hash fails every registration.
// DNS names are case-insensitive, so folding loses nothing.
func CanonicalHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

// RPIDForHost returns the WebAuthn RP ID for a configured hostname.
// go-webauthn's RPID validator rejects any value without a dot (except the
// literal "localhost"), so a bare single-label hostname (the very common case
// on Macs, e.g. "macmini") falls back to its ".local" form. The host is
// canonicalized first so a name persisted before that was enforced still
// derives the RP ID a browser will actually agree with.
func RPIDForHost(host string) string {
	host = CanonicalHost(host)
	if strings.Contains(host, ".") || host == "localhost" {
		return host
	}
	return host + ".local"
}

// RPChangeError reports that a hostname change would alter the WebAuthn RP ID
// while registered passkeys exist, stranding them. Callers must get explicit
// confirmation and retry with confirm=true (or run `multimux auth reset`).
type RPChangeError struct {
	Prev, Next  string
	Credentials int
}

func (e *RPChangeError) Error() string {
	return fmt.Sprintf("hostname %q would change the WebAuthn RP ID from %q to %q, which invalidates all %d registered passkey(s)",
		e.Next, RPIDForHost(e.Prev), RPIDForHost(e.Next), e.Credentials)
}

// validateHostname checks a hostname that has already been through
// CanonicalHost.
func validateHostname(host string) error {
	if host == "" {
		return fmt.Errorf("hostname must not be empty")
	}
	if !strings.Contains(host, ".") && host != "localhost" {
		return fmt.Errorf("hostname %q must contain a dot (or be \"localhost\") — WebAuthn rejects other dotless RP IDs; try %q", host, host+".local")
	}
	return nil
}

// normalizeSANs splits a comma-separated SAN list, trims and lowercases
// entries, and drops empties. Entries with whitespace, slashes, or colons
// cannot be certificate DNS names and are rejected. Lowercasing keeps every
// name this daemon answers to in the one form a browser sends back — see
// CanonicalHost.
func normalizeSANs(raw string) (string, error) {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.ContainsAny(s, " \t/:") {
			return "", fmt.Errorf("extra SAN %q is not a valid name", s)
		}
		out = append(out, CanonicalHost(s))
	}
	return strings.Join(out, ","), nil
}

func validatePort(p string) error {
	if p == "" {
		return nil // cleared: daemon falls back to the default port
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port %q must be an integer between 1 and 65535", p)
	}
	return nil
}

// Apply validates changes (any subset of "hostname", "extra_sans", "port")
// and persists them in one transaction — on any error nothing is written.
// A hostname change that alters the RP ID while credentials exist returns
// *RPChangeError unless confirm is true. rpChanged reports whether the
// persisted change altered the RP ID.
func Apply(st *store.Store, changes map[string]string, confirm bool) (rpChanged bool, err error) {
	validated := make(map[string]string, len(changes))
	for key, value := range changes {
		switch key {
		case "hostname":
			value = CanonicalHost(value)
			if err := validateHostname(value); err != nil {
				return false, err
			}
			validated[key] = value
		case "extra_sans":
			norm, err := normalizeSANs(value)
			if err != nil {
				return false, err
			}
			validated[key] = norm
		case "port":
			if err := validatePort(value); err != nil {
				return false, err
			}
			validated[key] = value
		default:
			return false, fmt.Errorf("unknown identity setting %q", key)
		}
	}

	if host, ok := validated["hostname"]; ok {
		prev, err := st.GetSetting("hostname")
		if err != nil {
			return false, err
		}
		// RPIDForHost canonicalizes, so a stored "MacBook-Pro.local" and the
		// folded "macbook-pro.local" compare equal: canonicalizing an old
		// hostname is not a change that strands passkeys.
		if prev != "" && RPIDForHost(prev) != RPIDForHost(host) {
			rpChanged = true
			n, err := st.CountCredentials()
			if err != nil {
				return false, err
			}
			if n > 0 && !confirm {
				return false, &RPChangeError{Prev: prev, Next: host, Credentials: n}
			}
		}
	}

	return rpChanged, st.SetSettings(validated)
}

// LoginOrigins filters origins to those a browser will actually allow to run
// WebAuthn ceremonies for rpID: the origin's host must equal the RP ID or be
// a subdomain of it. Bare aliases, IPs, and unrelated SANs are dropped —
// printing them as setup/login URLs only advertises dead ends.
func LoginOrigins(origins []string, rpID string) []string {
	var out []string
	for _, o := range origins {
		u, err := url.Parse(o)
		if err != nil {
			continue
		}
		h := u.Hostname()
		if h == rpID || strings.HasSuffix(h, "."+rpID) {
			out = append(out, o)
		}
	}
	return out
}
