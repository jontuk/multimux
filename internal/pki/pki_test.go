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
	if regen != CARegenNone {
		t.Fatalf("first Ensure should not report regeneration, got %v", regen)
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
	if regen, err := p.Ensure(hosts); err != nil || regen != CARegenNone {
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

	if regen, err := p.Ensure(hosts); err != nil || regen != CARegenNone {
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

// withCAAged runs Ensure with the clock wound back by age, so the CA it writes
// is that much closer to its NotAfter by the time the test's real clock reads it.
func withCAAged(t *testing.T, p *PKI, hosts []string, age time.Duration) {
	t.Helper()
	old := now
	now = func() time.Time { return time.Now().Add(-age) }
	defer func() { now = old }()
	if _, err := p.Ensure(hosts); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRegeneratesExpiredCA(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox", "mybox.local"}
	withCAAged(t, p, hosts, caValidity+24*time.Hour) // CA expired yesterday
	oldCA := loadCert(t, p.CACertPath())
	oldLeaf := loadCert(t, p.LeafCertPath()).SerialNumber
	if oldCA.NotAfter.After(time.Now()) {
		t.Fatalf("test setup did not produce an expired CA: NotAfter=%v", oldCA.NotAfter)
	}

	regen, err := p.Ensure(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if regen != CARegenExpiring {
		t.Fatalf("regen = %v, want CARegenExpiring", regen)
	}
	ca := loadCert(t, p.CACertPath())
	if ca.NotAfter.Before(time.Now().AddDate(9, 0, 0)) {
		t.Fatalf("replacement CA validity too short: %v", ca.NotAfter)
	}
	// The leaf must follow the new CA, or TLS breaks with an unverifiable chain.
	leaf := loadCert(t, p.LeafCertPath())
	if leaf.SerialNumber.Cmp(oldLeaf) == 0 {
		t.Fatal("leaf was not reissued")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "mybox.local"}); err != nil {
		t.Fatalf("leaf does not chain to the regenerated CA: %v", err)
	}
	if _, err := tls.LoadX509KeyPair(p.LeafCertPath(), p.LeafKeyPath()); err != nil {
		t.Fatalf("leaf pair unusable: %v", err)
	}
}

func TestEnsureRenewsCAInsideRenewalWindow(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox"}
	// Still valid, but only just: inside caRenewUnder of expiry.
	withCAAged(t, p, hosts, caValidity-caRenewUnder/2)
	if before := loadCert(t, p.CACertPath()); !before.NotAfter.After(time.Now()) {
		t.Fatalf("test setup expired the CA outright: NotAfter=%v", before.NotAfter)
	}

	regen, err := p.Ensure(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if regen != CARegenExpiring {
		t.Fatalf("regen = %v, want CARegenExpiring", regen)
	}
	if ca := loadCert(t, p.CACertPath()); ca.NotAfter.Before(time.Now().AddDate(9, 0, 0)) {
		t.Fatalf("CA was not renewed: %v", ca.NotAfter)
	}
}

func TestEnsureKeepsHealthyCA(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox"}
	// Mid-life: well past creation, well clear of the renewal window. Renewing
	// here would churn the user's trust store on every start.
	withCAAged(t, p, hosts, caValidity/2)
	before := loadCert(t, p.CACertPath()).SerialNumber

	regen, err := p.Ensure(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if regen != CARegenNone {
		t.Fatalf("regen = %v, want CARegenNone", regen)
	}
	if after := loadCert(t, p.CACertPath()).SerialNumber; before.Cmp(after) != 0 {
		t.Fatal("healthy CA regenerated with no reason")
	}
}

func TestLeafNeverOutlivesCA(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox"}
	// CA closer to expiry than a full leaf lifetime, but not yet due for
	// renewal, so the leaf must be clamped to the CA's NotAfter.
	withCAAged(t, p, hosts, caValidity-leafValidity/2)
	ca := loadCert(t, p.CACertPath())

	if _, err := p.Ensure(hosts); err != nil {
		t.Fatal(err)
	}
	leaf := loadCert(t, p.LeafCertPath())
	if leaf.NotAfter.After(ca.NotAfter) {
		t.Fatalf("leaf outlives its issuer: leaf=%v ca=%v", leaf.NotAfter, ca.NotAfter)
	}
	if !leaf.NotAfter.Equal(ca.NotAfter) {
		t.Fatalf("leaf NotAfter = %v, want clamp to CA NotAfter %v", leaf.NotAfter, ca.NotAfter)
	}

	// A clamped leaf must not read as due for rotation, or the daily check
	// would rewrite it forever.
	steady := leaf.SerialNumber
	if _, err := p.Ensure(hosts); err != nil {
		t.Fatal(err)
	}
	if after := loadCert(t, p.LeafCertPath()).SerialNumber; steady.Cmp(after) != 0 {
		t.Fatal("clamped leaf rotated again: rotation loop")
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
	if regen != CARegenHostnames {
		t.Fatalf("regen = %v, want CARegenHostnames", regen)
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
