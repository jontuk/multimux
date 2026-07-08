package pki

import (
	"strings"
	"testing"
)

func TestTrustCommandDarwin(t *testing.T) {
	cmd, err := TrustCommand("darwin", "/data/pki/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "security add-trusted-cert") || !strings.Contains(joined, "/data/pki/ca.pem") {
		t.Fatalf("darwin cmd = %q", joined)
	}
}

func TestTrustCommandLinuxUnsupported(t *testing.T) {
	// On a linux box without a known anchors dir the caller gets a clear error.
	if _, err := TrustCommand("plan9", "/x/ca.pem"); err == nil {
		t.Fatal("want error for unsupported OS")
	}
}
