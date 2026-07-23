package cmd

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
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

func TestTrustCA(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	writeTestCA(t, caPath, true, []string{"mux.example.com"})

	t.Run("success installs and reports", func(t *testing.T) {
		var gotCmd *exec.Cmd
		orig := runTrustCmd
		runTrustCmd = func(c *exec.Cmd) error { gotCmd = c; return nil }
		defer func() { runTrustCmd = orig }()

		var out, errOut bytes.Buffer
		if err := trustCA(caPath, &out, &errOut); err != nil {
			t.Fatalf("trustCA err = %v", err)
		}
		if gotCmd == nil {
			t.Fatal("runTrustCmd was not called")
		}
		if !strings.Contains(out.String(), "mux.example.com") {
			t.Fatalf("stdout missing name constraint: %q", out.String())
		}
		if !strings.Contains(out.String(), "CA installed into OS trust store") {
			t.Fatalf("stdout missing install confirmation: %q", out.String())
		}
	})

	t.Run("trust command failure is wrapped", func(t *testing.T) {
		orig := runTrustCmd
		runTrustCmd = func(c *exec.Cmd) error { return errors.New("boom") }
		defer func() { runTrustCmd = orig }()

		var out, errOut bytes.Buffer
		err := trustCA(caPath, &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), "trust install failed") {
			t.Fatalf("err = %v, want wrapped trust-install failure", err)
		}
	})

	t.Run("bad CA fails before running command", func(t *testing.T) {
		called := false
		orig := runTrustCmd
		runTrustCmd = func(c *exec.Cmd) error { called = true; return nil }
		defer func() { runTrustCmd = orig }()

		var out, errOut bytes.Buffer
		if err := trustCA(filepath.Join(dir, "nope.pem"), &out, &errOut); err == nil {
			t.Fatal("trustCA accepted a missing CA file")
		}
		if called {
			t.Fatal("runTrustCmd ran despite describeCA failure")
		}
	})
}

// TestCATrustInstalls exercises the `ca trust` command end-to-end with the OS
// trust step stubbed, covering the runCA -> trustCA wiring.
func TestCATrustInstalls(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MULTIMUX_DATA_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "pki"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestCA(t, filepath.Join(dir, "pki", "ca.pem"), true, []string{"mux.example.com"})

	orig := runTrustCmd
	runTrustCmd = func(c *exec.Cmd) error { return nil }
	defer func() { runTrustCmd = orig }()

	var out, errOut bytes.Buffer
	if code := Execute([]string{"ca", "trust"}, "dev", fstest.MapFS{}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "CA installed into OS trust store") {
		t.Fatalf("stdout missing install confirmation: %q", out.String())
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
