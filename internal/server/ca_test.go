package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jontuk/multimux/internal/pki"
)

func TestCAExportIsPublicDuringAndAfterSetup(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(fmt.Sprint("registered=", registered), func(t *testing.T) {
			s, _, _ := newTestServer(t, registered)
			crt := do(t, s, "GET", "/ca.crt", "")
			if crt.Code != http.StatusOK ||
				crt.Header().Get("Content-Type") != "application/x-x509-ca-cert" ||
				crt.Header().Get("Content-Disposition") != `attachment; filename="multimux-ca.crt"` ||
				crt.Header().Get("Cache-Control") != "no-store" ||
				crt.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("certificate response = %d, headers=%v", crt.Code, crt.Header())
			}
			if !strings.Contains(crt.Body.String(), "-----BEGIN CERTIFICATE-----") {
				t.Fatalf("certificate response = %q", crt.Body.String())
			}

			info := do(t, s, "GET", "/ca/info", "")
			if info.Code != http.StatusOK ||
				info.Header().Get("Cache-Control") != "no-store" ||
				info.Header().Get("X-Content-Type-Options") != "nosniff" ||
				!strings.Contains(info.Body.String(), `"sha256Fingerprint"`) {
				t.Fatalf("info response = %d, headers=%v: %s", info.Code, info.Header(), info.Body.String())
			}
		})
	}
}

func TestCARoutesReadAndValidateCurrentCAOnEveryRequest(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	first := validTestCA(t, "first.local")
	second := validTestCA(t, "second.local")
	current := first
	reads := 0
	s.cfg.ReadCA = func() ([]byte, error) {
		reads++
		return append([]byte(nil), current...), nil
	}

	firstResponse := do(t, s, "GET", "/ca/info", "")
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first response = %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	current = second
	secondResponse := do(t, s, "GET", "/ca/info", "")
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second response = %d: %s", secondResponse.Code, secondResponse.Body.String())
	}
	if reads != 2 {
		t.Fatalf("ReadCA calls = %d, want 2", reads)
	}
	if firstResponse.Body.String() == secondResponse.Body.String() ||
		!strings.Contains(secondResponse.Body.String(), "second.local") {
		t.Fatalf("responses did not reflect current CA:\nfirst=%s\nsecond=%s", firstResponse.Body.String(), secondResponse.Body.String())
	}
}

func TestCARoutesRejectUnavailableOrInvalidCA(t *testing.T) {
	valid := validTestCA(t, "localhost")
	nonCA := validTestCertificate(t, "leaf.local", false)
	cases := []struct {
		name   string
		readCA func() ([]byte, error)
	}{
		{"missing reader", nil},
		{"read error", func() ([]byte, error) { return nil, errors.New("read failed") }},
		{"malformed", func() ([]byte, error) { return []byte("not pem"), nil }},
		{"multiple certificates", func() ([]byte, error) { return append(append([]byte(nil), valid...), valid...), nil }},
		{"non CA", func() ([]byte, error) { return nonCA, nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := newTestServer(t, true)
			s.cfg.ReadCA = tc.readCA
			for _, path := range []string{"/ca.crt", "/ca/info"} {
				response := do(t, s, "GET", path, "")
				if response.Code != http.StatusInternalServerError {
					t.Fatalf("GET %s = %d, want 500: %s", path, response.Code, response.Body.String())
				}
				if strings.Contains(response.Body.String(), "-----BEGIN CERTIFICATE-----") {
					t.Fatalf("GET %s leaked certificate: %s", path, response.Body.String())
				}
			}
		})
	}
}

func TestCAExportDoesNotServeSPAFallback(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	s.cfg.ReadCA = func() ([]byte, error) { return nil, errors.New("unavailable") }
	response := do(t, s, "GET", "/ca.crt", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("GET /ca.crt = %d, want 500", response.Code)
	}
	if strings.Contains(response.Body.String(), "<html>") {
		t.Fatalf("GET /ca.crt served SPA: %s", response.Body.String())
	}
}

func validTestCA(t *testing.T, hostname string) []byte {
	t.Helper()
	mgr := pki.New(t.TempDir())
	if _, err := mgr.Ensure([]string{hostname}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(mgr.CACertPath())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func validTestCertificate(t *testing.T, hostname string, isCA bool) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostname},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
