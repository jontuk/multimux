package pki

import (
	"os/exec"
	"strings"
	"testing"
)

// stubAnchors makes the distro probe deterministic: only dir is reported to
// exist, so tests never depend on the host's trust-store layout.
func stubAnchors(t *testing.T, dir string) {
	t.Helper()
	orig := dirExists
	dirExists = func(p string) bool { return p == dir }
	t.Cleanup(func() { dirExists = orig })
}

func TestTrustCommandsDarwin(t *testing.T) {
	cmds, err := TrustCommands("darwin", "/data/pki/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	joined := strings.Join(cmds[0].Args, " ")
	if !strings.Contains(joined, "security add-trusted-cert") || !strings.Contains(joined, "/data/pki/ca.pem") {
		t.Fatalf("darwin cmd = %q", joined)
	}
}

func TestTrustCommandsLinuxAnchors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dir     string
		dest    string
		refresh string
	}{
		{"debian", "/usr/local/share/ca-certificates", "/usr/local/share/ca-certificates/multimux-ca.crt", "update-ca-certificates"},
		{"fedora", "/etc/pki/ca-trust/source/anchors", "/etc/pki/ca-trust/source/anchors/multimux-ca.pem", "update-ca-trust"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubAnchors(t, tc.dir)
			cmds, err := TrustCommands("linux", "/data/pki/ca.pem")
			if err != nil {
				t.Fatal(err)
			}
			if len(cmds) != 2 {
				t.Fatalf("got %d commands, want copy + refresh", len(cmds))
			}
			if got := cmds[0].Args[len(cmds[0].Args)-1]; got != tc.dest {
				t.Fatalf("copy destination = %q, want %q", got, tc.dest)
			}
			if got := cmds[1].Args[len(cmds[1].Args)-1]; got != tc.refresh {
				t.Fatalf("refresh command = %q, want %q", got, tc.refresh)
			}
		})
	}
}

func TestTrustCommandsUnsupported(t *testing.T) {
	if _, err := TrustCommands("plan9", "/x/ca.pem"); err == nil {
		t.Fatal("want error for unsupported OS")
	}
}

func TestTrustCommandsLinuxNoAnchorsDir(t *testing.T) {
	// On a linux box without a known anchors dir the caller gets a clear error.
	stubAnchors(t, "/nowhere")
	if _, err := TrustCommands("linux", "/x/ca.pem"); err == nil {
		t.Fatal("want error when no anchors directory exists")
	}
}

// TestTrustCommandsNoShellInjection is the regression test for the old
// implementation, which built a `sudo sh -c "cp %q ..."` script with Go's %q
// quoting — not POSIX quoting — so a hostile CA path could run as root.
// Every command must be fixed argv: the path appears as one literal element and
// never inside a shell string.
func TestTrustCommandsNoShellInjection(t *testing.T) {
	hostile := []string{
		`/data/pki/x"; touch /tmp/pwned; #.pem`,
		"/data/pki/$(touch /tmp/pwned).pem",
		"/data/pki/`touch /tmp/pwned`.pem",
		"/data/pki/with spaces/ca.pem",
		"/data/pki/back\\slash\nnewline.pem",
		"--flag-like.pem",
	}
	for _, goos := range []string{"darwin", "linux"} {
		for _, caPath := range hostile {
			t.Run(goos+" "+caPath, func(t *testing.T) {
				if goos == "linux" {
					stubAnchors(t, "/usr/local/share/ca-certificates")
				}
				cmds, err := TrustCommands(goos, caPath)
				if err != nil {
					t.Fatal(err)
				}
				var sawPath bool
				for _, c := range cmds {
					assertNoShell(t, c)
					for _, arg := range c.Args {
						if arg == caPath {
							sawPath = true
							continue
						}
						if strings.Contains(arg, caPath) {
							t.Fatalf("caPath embedded in argument %q", arg)
						}
					}
				}
				if !sawPath {
					t.Fatalf("caPath missing from argv of %v", argvs(cmds))
				}
				if goos == "linux" {
					// "--" must precede the path so a leading "-" is not a flag.
					if got := cmds[0].Args; got[len(got)-3] != "--" {
						t.Fatalf("copy argv %q lacks -- before the source path", got)
					}
				}
			})
		}
	}
}

// assertNoShell fails if c hands anything to a shell for interpretation.
func assertNoShell(t *testing.T, c *exec.Cmd) {
	t.Helper()
	for i, arg := range c.Args {
		switch arg {
		case "sh", "bash", "zsh", "dash", "/bin/sh", "/bin/bash":
			// A shell is only a problem if it is asked to interpret a string.
			for _, rest := range c.Args[i+1:] {
				if rest == "-c" {
					t.Fatalf("command runs a shell script: %q", c.Args)
				}
			}
		}
	}
}

func argvs(cmds []*exec.Cmd) [][]string {
	out := make([][]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Args)
	}
	return out
}
