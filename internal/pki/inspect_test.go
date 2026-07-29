package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

func inspectionPEM(t *testing.T, isCA bool) ([]byte, []byte, time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Date(2036, 7, 28, 12, 34, 56, 0, time.FixedZone("offset", 3600))
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(7),
		Subject:               pkix.Name{CommonName: "multimux local CA (phone)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              expiry,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		PermittedDNSDomains:   []string{"phone.local", "phone.example.ts.net"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), der, expiry
}

func TestInspectCA(t *testing.T) {
	raw, der, expiry := inspectionPEM(t, true)
	info, err := InspectCA(append(append([]byte("\n\t"), raw...), []byte(" \n")...))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	plain := strings.ToUpper(hex.EncodeToString(sum[:]))
	var pairs []string
	for len(plain) > 0 {
		pairs, plain = append(pairs, plain[:2]), plain[2:]
	}
	wantFingerprint := strings.Join(pairs, ":")
	if info.Subject != "multimux local CA (phone)" ||
		!slices.Equal(info.PermittedDNSDomains, []string{"phone.local", "phone.example.ts.net"}) ||
		info.Expires != expiry.UTC().Format(time.RFC3339) ||
		info.SHA256Fingerprint != wantFingerprint {
		t.Fatalf("InspectCA = %+v", info)
	}
}

func TestInspectCARejectsAnythingExceptOneCACertificate(t *testing.T) {
	ca, _, _ := inspectionPEM(t, true)
	leaf, _, _ := inspectionPEM(t, false)
	cases := map[string][]byte{
		"empty":         nil,
		"malformed":     []byte("not pem"),
		"wrong type":    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("x")}),
		"leading text":  append([]byte("secret\n"), ca...),
		"trailing text": append(append([]byte{}, ca...), []byte("secret")...),
		"second block":  append(append([]byte{}, ca...), ca...),
		"non CA":        leaf,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectCA(raw); err == nil {
				t.Fatal("accepted invalid CA input")
			}
		})
	}
}
