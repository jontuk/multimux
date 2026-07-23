package pki

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// linuxAnchor describes one distro's trust-anchor layout: the directory that
// marks the distro, the file the CA is copied to, and the refresh tool that
// rebuilds the system bundle afterwards.
type linuxAnchor struct {
	dir     string
	dest    string
	refresh string
}

// linuxAnchors is probed in order; the first existing directory wins.
var linuxAnchors = []linuxAnchor{
	{ // Debian/Ubuntu
		dir:     "/usr/local/share/ca-certificates",
		dest:    "/usr/local/share/ca-certificates/multimux-ca.crt",
		refresh: "update-ca-certificates",
	},
	{ // Fedora/RHEL
		dir:     "/etc/pki/ca-trust/source/anchors",
		dest:    "/etc/pki/ca-trust/source/anchors/multimux-ca.pem",
		refresh: "update-ca-trust",
	},
}

// dirExists probes for a trust-anchors directory. It is a package var so tests
// can drive the distro detection instead of depending on the host layout.
var dirExists = func(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// TrustCommands returns the commands that install the CA cert into the OS trust
// store, to be run in order. Separated from execution for testability.
//
// Every command is fixed argv — nothing is ever handed to a shell, so a CA path
// containing shell metacharacters (or one that looks like a flag) is inert.
//
// darwin: adds to the user's login keychain (no sudo; Keychain prompts once).
// linux: copies into the distro's anchors dir and refreshes — needs sudo, and
// therefore takes two commands.
// Note for docs: Firefox/Chromium on Linux use their own NSS store; see
// docs/install.md for the certutil incantation.
func TrustCommands(goos, caPath string) ([]*exec.Cmd, error) {
	switch goos {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		keychain := filepath.Join(home, "Library", "Keychains", "login.keychain-db")
		return []*exec.Cmd{
			exec.Command("security", "add-trusted-cert", "-r", "trustRoot", "-k", keychain, caPath),
		}, nil
	case "linux":
		for _, a := range linuxAnchors {
			if !dirExists(a.dir) {
				continue
			}
			return []*exec.Cmd{
				// "--" keeps a caPath starting with "-" from being read as a flag.
				exec.Command("sudo", "install", "-m", "0644", "-T", "--", caPath, a.dest),
				exec.Command("sudo", "--", a.refresh),
			}, nil
		}
		return nil, fmt.Errorf("pki: no known CA anchors directory; install %s manually", caPath)
	default:
		return nil, fmt.Errorf("pki: unsupported OS %q", goos)
	}
}
