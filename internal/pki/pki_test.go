package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"
)

func loadCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("no PEM in %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestEnsureCreatesConstrainedCAAndLeaf(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox", "mybox.local"}
	regen, err := p.Ensure(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if regen {
		t.Fatal("first Ensure should not report regeneration")
	}

	ca := loadCert(t, p.CACertPath())
	if !ca.IsCA || !ca.PermittedDNSDomainsCritical {
		t.Fatalf("CA missing critical name constraints: IsCA=%v critical=%v", ca.IsCA, ca.PermittedDNSDomainsCritical)
	}
	if len(ca.PermittedDNSDomains) != 2 || ca.PermittedDNSDomains[0] != "mybox" {
		t.Fatalf("constraints = %v", ca.PermittedDNSDomains)
	}
	if ca.NotAfter.Before(time.Now().AddDate(9, 0, 0)) {
		t.Fatalf("CA validity too short: %v", ca.NotAfter)
	}

	leaf := loadCert(t, p.LeafCertPath())
	if len(leaf.DNSNames) != 2 || leaf.DNSNames[1] != "mybox.local" {
		t.Fatalf("leaf SANs = %v", leaf.DNSNames)
	}
	// Leaf chains to the CA under its name constraints.
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "mybox.local"}); err != nil {
		t.Fatalf("leaf does not verify: %v", err)
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox"}
	if _, err := p.Ensure(hosts); err != nil {
		t.Fatal(err)
	}
	before := loadCert(t, p.LeafCertPath()).SerialNumber
	if regen, err := p.Ensure(hosts); err != nil || regen {
		t.Fatalf("second Ensure: regen=%v err=%v", regen, err)
	}
	after := loadCert(t, p.LeafCertPath()).SerialNumber
	if before.Cmp(after) != 0 {
		t.Fatal("leaf rotated with no reason")
	}
}

func TestEnsureRepairsMismatchedLeafKey(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox", "mybox.local"}
	if _, err := p.Ensure(hosts); err != nil {
		t.Fatal(err)
	}
	before := loadCert(t, p.LeafCertPath()).SerialNumber

	// A crash between writePair's two renames leaves the new cert beside the
	// previous generation's key: still valid, chaining and unexpired, so only a
	// pairing check can spot it.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	stray := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(p.LeafKeyPath(), stray, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tls.LoadX509KeyPair(p.LeafCertPath(), p.LeafKeyPath()); err == nil {
		t.Fatal("test setup did not produce a mismatched pair")
	}

	if regen, err := p.Ensure(hosts); err != nil || regen {
		t.Fatalf("repairing Ensure: regen=%v err=%v", regen, err)
	}
	if _, err := tls.LoadX509KeyPair(p.LeafCertPath(), p.LeafKeyPath()); err != nil {
		t.Fatalf("leaf pair still mismatched after Ensure: %v", err)
	}
	if after := loadCert(t, p.LeafCertPath()).SerialNumber; before.Cmp(after) == 0 {
		t.Fatal("leaf was not regenerated")
	}

	// The healthy pair that repair just produced must survive untouched;
	// rotating on every start would churn certs (and user trust prompts).
	steady := loadCert(t, p.LeafCertPath()).SerialNumber
	if _, err := p.Ensure(hosts); err != nil {
		t.Fatal(err)
	}
	if now := loadCert(t, p.LeafCertPath()).SerialNumber; steady.Cmp(now) != 0 {
		t.Fatal("healthy leaf rotated with no reason")
	}
}

func TestEnsureRotatesLeafOnSANChange(t *testing.T) {
	p := New(t.TempDir())
	if _, err := p.Ensure([]string{"mybox"}); err != nil {
		t.Fatal(err)
	}
	// New hostname set → CA constraints no longer match → CA + leaf regenerated.
	regen, err := p.Ensure([]string{"mybox", "mybox.tail1234.ts.net"})
	if err != nil {
		t.Fatal(err)
	}
	if !regen {
		t.Fatal("expected CA regeneration to be reported")
	}
	ca := loadCert(t, p.CACertPath())
	if len(ca.PermittedDNSDomains) != 2 {
		t.Fatalf("constraints = %v", ca.PermittedDNSDomains)
	}
	leaf := loadCert(t, p.LeafCertPath())
	if len(leaf.DNSNames) != 2 {
		t.Fatalf("leaf SANs = %v", leaf.DNSNames)
	}
}
