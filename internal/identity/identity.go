// Package identity is the single validation and persistence path for the
// daemon's identity settings (hostname, extra SANs, port). The CLI --hostname
// flag and the settings API both go through Apply, so the WebAuthn RP-ID
// lockout guard cannot be bypassed and invalid or partial writes never land.
package identity

import (
	"errors"
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
// DNS names are case-insensitive, so folding loses nothing. One trailing dot
// (the fully-qualified form DNS tools print) goes too: it names the same host,
// and x509.ParseCertificate rejects it in a name constraint.
func CanonicalHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// ValidateName reports whether a name that has already been through
// CanonicalHost can be a certificate DNS name. It is deliberately stricter than
// the x509 parser: the names here become CA name constraints, and a name that
// fails to parse only surfaces at the next restart. Underscores are not valid
// hostnames but are common in the wild, and Go's verifier accepts them.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("name must not be empty")
	}
	if len(name) > 253 {
		return fmt.Errorf("name %q is longer than 253 characters", name)
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return fmt.Errorf("name %q has an empty label", name)
		}
		if len(label) > 63 {
			return fmt.Errorf("name %q has a label longer than 63 characters", name)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("name %q has a label starting or ending with a hyphen", name)
		}
		for _, c := range label {
			valid := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
			if !valid {
				return fmt.Errorf("name %q contains invalid character %q", name, c)
			}
		}
	}
	return nil
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
	if err := ValidateName(host); err != nil {
		return fmt.Errorf("hostname: %w", err)
	}
	if !strings.Contains(host, ".") && host != "localhost" {
		return fmt.Errorf("hostname %q must contain a dot (or be \"localhost\") — WebAuthn rejects other dotless RP IDs; try %q", host, host+".local")
	}
	return nil
}

// normalizeSANs splits a comma-separated SAN list, canonicalizes entries, and
// drops empties. Entries that cannot be certificate DNS names are rejected.
// Canonicalizing keeps every name this daemon answers to in the one form a
// browser sends back — see CanonicalHost.
func normalizeSANs(raw string) (string, error) {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		s = CanonicalHost(s)
		if s == "" {
			continue
		}
		if err := ValidateName(s); err != nil {
			return "", fmt.Errorf("extra SAN: %w", err)
		}
		out = append(out, s)
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
		// hostname is not a change that strands passkeys. Nor is leaving an
		// invalid name: no browser could reach it, so no passkey is bound to it,
		// and --hostname is the way out when one was stored by an older version.
		if prev != "" && ValidateName(CanonicalHost(prev)) == nil && RPIDForHost(prev) != RPIDForHost(host) {
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
