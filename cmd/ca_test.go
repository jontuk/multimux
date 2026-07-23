package cmd

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestCAUsageOnBadSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute([]string{"ca", "bogus"}, "dev", fstest.MapFS{}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage: multimux ca trust") {
		t.Fatalf("stderr missing ca usage: %q", errOut.String())
	}
}

func TestCATrustRejectsStrayArg(t *testing.T) {
	var out, errOut bytes.Buffer
	// The explicit-path form was removed; a positional arg is now a usage error.
	if code := Execute([]string{"ca", "trust", "some.pem"}, "dev", fstest.MapFS{}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unexpected argument") {
		t.Fatalf("stderr = %q, want unexpected-argument error", errOut.String())
	}
}

func TestCATrustMissingLocalCA(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	if code := Execute([]string{"ca", "trust"}, "dev", fstest.MapFS{}, &out, &errOut); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "no local CA found") {
		t.Fatalf("stderr = %q, want missing-CA hint", errOut.String())
	}
}

func TestDescribeCA(t *testing.T) {
	dir := t.TempDir()

	caPath := filepath.Join(dir, "ca.pem")
	writeTestCA(t, caPath, true, []string{"oci1.example.ts.net"})
	desc, err := describeCA(caPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desc, "oci1.example.ts.net") {
		t.Fatalf("describeCA output missing name constraint: %q", desc)
	}

	leafPath := filepath.Join(dir, "leaf.pem")
	writeTestCA(t, leafPath, false, nil) // not a CA
	if _, err := describeCA(leafPath); err == nil {
		t.Fatal("describeCA accepted a non-CA certificate")
	}

	if _, err := describeCA(filepath.Join(dir, "nope.pem")); err == nil {
		t.Fatal("describeCA accepted a missing file")
	}
}

// writeTestCA writes a self-signed certificate to path. isCA and name
// constraints let a single helper cover both the CA and reject-non-CA cases.
func writeTestCA(t *testing.T, path string, isCA bool, permitted []string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "multimux local CA (test)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		PermittedDNSDomains:   permitted,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}
}
