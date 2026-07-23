package cmd

import (
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jontuk/multimux/internal/pki"
)

func dataDir() string {
	if d := os.Getenv("MULTIMUX_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "multimux")
}

// defaultRemoteCAPath is where `multimux serve` writes the CA on a default
// install. The remote shell expands ~ when scp copies from it.
const defaultRemoteCAPath = "~/.local/share/multimux/pki/ca.pem"

const caUsage = `usage: multimux ca trust [flags]

Install a multimux CA certificate into this machine's OS trust store, so the
browser accepts the daemon's HTTPS certificate.

Forms:
  multimux ca trust                 trust THIS host's own CA
                                    (~/.local/share/multimux/pki/ca.pem)
  multimux ca trust --remote HOST   copy the CA from a remote multimux host over
                                    ssh/scp, then trust it (run this on the CLIENT)

flags:
  --remote [user@]HOST   remote host running multimux; its CA is fetched via scp
  --remote-path PATH     path to ca.pem on the remote host
                         (default ~/.local/share/multimux/pki/ca.pem)

The multimux CA is X.509 name-constrained to the daemon's own hostnames, so
trusting it cannot be used to intercept any other site. The permitted hostnames
are printed before the trust is installed.

Platform notes:
  macOS   adds to your login keychain (Keychain may prompt once; no sudo).
  Linux   copies into the system trust anchors and refreshes (needs sudo).
          Firefox/Chromium keep their own NSS store — see docs/install.md.
`

func runCA(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "trust" {
		fmt.Fprint(stderr, caUsage)
		return 2
	}

	fs := flag.NewFlagSet("ca trust", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, caUsage) }
	remote := fs.String("remote", "", "remote host to copy the CA from via scp, e.g. user@host")
	remotePath := fs.String("remote-path", defaultRemoteCAPath, "path to ca.pem on the remote host")
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "ca trust: unexpected argument %q\n", fs.Arg(0))
		fmt.Fprint(stderr, caUsage)
		return 2
	}

	var caPath string
	if *remote != "" {
		tmp, err := os.CreateTemp("", "multimux-remote-ca-*.pem")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		tmp.Close()
		defer os.Remove(tmp.Name())
		src := fmt.Sprintf("%s:%s", *remote, *remotePath)
		fmt.Fprintf(stdout, "copying CA from %s ...\n", src)
		scp := exec.Command("scp", src, tmp.Name())
		scp.Stdout, scp.Stderr = stdout, stderr
		if err := scp.Run(); err != nil {
			fmt.Fprintf(stderr, "scp failed: %v\n", err)
			return 1
		}
		caPath = tmp.Name()
	} else {
		caPath = pki.New(filepath.Join(dataDir(), "pki")).CACertPath()
		if _, err := os.Stat(caPath); err != nil {
			fmt.Fprintln(stderr, "no local CA found — run `multimux serve` once first,\nor use --remote HOST to trust another host's CA")
			return 1
		}
	}

	desc, err := describeCA(caPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprint(stdout, desc)

	c, err := pki.TrustCommand(runtime.GOOS, caPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	c.Stdout, c.Stderr = stdout, stderr
	if err := c.Run(); err != nil {
		fmt.Fprintf(stderr, "trust install failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "CA installed into OS trust store")
	return 0
}

// describeCA validates that path holds a CA certificate and returns a short
// human summary — including the name constraints, so the user sees exactly
// which hostnames trusting it would vouch for before it is installed.
func describeCA(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", fmt.Errorf("ca trust: %s is not a PEM certificate", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("ca trust: parsing %s: %w", path, err)
	}
	if !cert.IsCA {
		return "", fmt.Errorf("ca trust: %s is not a CA certificate", path)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CA: %s\n", cert.Subject.CommonName)
	if len(cert.PermittedDNSDomains) > 0 {
		fmt.Fprintf(&b, "  constrained to: %s\n", strings.Join(cert.PermittedDNSDomains, ", "))
	}
	fmt.Fprintf(&b, "  expires: %s\n", cert.NotAfter.Format("2006-01-02"))
	return b.String(), nil
}
