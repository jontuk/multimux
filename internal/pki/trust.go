package pki

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// TrustCommand returns the command that installs the CA cert into the OS
// trust store. Separated from execution for testability.
//
// darwin: adds to the user's login keychain (no sudo; Keychain prompts once).
// linux: copies into the distro's anchors dir and refreshes — needs sudo.
// Note for docs: Firefox/Chromium on Linux use their own NSS store; see
// docs/install.md for the certutil incantation.
func TrustCommand(goos, caPath string) (*exec.Cmd, error) {
	switch goos {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		keychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
		return exec.Command("security", "add-trusted-cert", "-r", "trustRoot", "-k", keychain, caPath), nil
	case "linux":
		if _, err := os.Stat("/usr/local/share/ca-certificates"); err == nil { // Debian/Ubuntu
			script := fmt.Sprintf("cp %q /usr/local/share/ca-certificates/multimux-ca.crt && update-ca-certificates", caPath)
			return exec.Command("sudo", "sh", "-c", script), nil
		}
		if _, err := os.Stat("/etc/pki/ca-trust/source/anchors"); err == nil { // Fedora/RHEL
			script := fmt.Sprintf("cp %q /etc/pki/ca-trust/source/anchors/multimux-ca.pem && update-ca-trust", caPath)
			return exec.Command("sudo", "sh", "-c", script), nil
		}
		return nil, fmt.Errorf("pki: no known CA anchors directory; install %s manually", caPath)
	default:
		return nil, fmt.Errorf("pki: unsupported OS %q", goos)
	}
}
