package pki

import (
	"crypto/x509"
	"os"
	"testing"
	"time"
)

func TestTLSConfigReloadsRotatedLeaf(t *testing.T) {
	p := New(t.TempDir())
	hosts := []string{"mybox.local"}
	if _, err := p.Ensure(hosts); err != nil {
		t.Fatal(err)
	}

	tc := p.TLSConfig()
	first, err := tc.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Force a rotation: expire the leaf on disk and re-Ensure. Backdate the
	// file's mtime too so the reload can't be missed on filesystems with
	// coarse timestamps.
	caCert, caKey, err := p.loadCA()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.createLeaf(caCert, caKey, []string{"rotated.local"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p.LeafCertPath(), old, old); err != nil {
		t.Fatal(err)
	}

	second, err := tc.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Certificate[0]) == string(first.Certificate[0]) {
		t.Fatal("GetCertificate kept serving the pre-rotation leaf")
	}
	leaf, err := x509.ParseCertificate(second.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "rotated.local" {
		t.Fatalf("reloaded leaf SANs = %v", leaf.DNSNames)
	}
}

func TestTLSConfigServesCachedPairWhenDiskIsBroken(t *testing.T) {
	p := New(t.TempDir())
	if _, err := p.Ensure([]string{"mybox.local"}); err != nil {
		t.Fatal(err)
	}
	tc := p.TLSConfig()
	if _, err := tc.GetCertificate(nil); err != nil {
		t.Fatal(err)
	}
	// Simulate the mid-rotation window: cert on disk no longer matches key.
	if err := os.WriteFile(p.LeafCertPath(), []byte("not a cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	cert, err := tc.GetCertificate(nil)
	if err != nil || cert == nil {
		t.Fatalf("cached pair not served during broken-disk window: %v", err)
	}
}

func TestTLSConfigErrorsWithoutAnyLeaf(t *testing.T) {
	p := New(t.TempDir())
	if _, err := p.TLSConfig().GetCertificate(nil); err == nil {
		t.Fatal("want error when no leaf exists and nothing is cached")
	}
}
