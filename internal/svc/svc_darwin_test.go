//go:build darwin

package svc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The unit tests stub the launchd domain query, so only this one can catch a
// wrong belief about what launchctl actually *does* — which is how "Bootstrap
// failed: 5: Input/output error" came to be read as a transient teardown race
// and "fixed" with a retry loop that turned it into a permanent failure.
//
// Everything here runs under a throwaway label in a temporary HOME, so the
// user's own com.jontuk.multimux job is never touched.
func TestRestartAgentAgainstRealLaunchd(t *testing.T) {
	if _, err := exec.LookPath("launchctl"); err != nil {
		t.Skip("launchctl not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	origLabel := label
	label = fmt.Sprintf("com.jontuk.multimux-test-%d", os.Getpid())
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	t.Cleanup(func() {
		_ = exec.Command("launchctl", "bootout", target).Run()
		label = origLabel
	})

	// A stand-in for the daemon: long-running, so the job stays up between the
	// assertions below, and indifferent to the "serve" argument the unit adds.
	exe := filepath.Join(home, "fake-multimux")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexec /bin/sleep 120\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Install("darwin", exe); err != nil {
		t.Fatalf("Install (fresh) = %v", err)
	}
	first := jobPID(t, target)
	if first == 0 {
		t.Fatal("no job running after a fresh Install")
	}

	// The premise the fix rests on: launchctl refuses to bootstrap a label that
	// is already loaded, and says so with the same EIO the user reported.
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	err := runCmd("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist)
	if err == nil {
		t.Fatal("bootstrap of an already-loaded label succeeded; restartAgent's EIO handling assumes it fails")
	}
	if !strings.Contains(err.Error(), "Input/output error") {
		t.Logf("note: already-loaded bootstrap now reports %v, not the expected EIO", err)
	}
	if !jobLoaded(target) {
		t.Fatal("jobLoaded = false while the job is loaded; restartAgent cannot tell success from failure")
	}

	// What `service upgrade` does, and what used to report a bogus failure:
	// reinstall over the running job.
	if err := Install("darwin", exe); err != nil {
		t.Fatalf("Install (over the running job) = %v, want nil", err)
	}
	second := jobPID(t, target)
	if second == 0 {
		t.Fatal("no job running after reinstall")
	}
	if second == first {
		t.Fatalf("pid still %d after reinstall; the daemon was not restarted onto the new binary", first)
	}
}

var pidLine = regexp.MustCompile(`(?m)^\s+pid = (\d+)$`)

// jobPID returns the pid of the job running under target, 0 when there is none.
func jobPID(t *testing.T, target string) int {
	t.Helper()
	out, err := exec.Command("launchctl", "print", target).CombinedOutput()
	if err != nil {
		return 0
	}
	m := pidLine.FindSubmatch(out)
	if m == nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(string(m[1]), "%d", &pid); err != nil {
		t.Fatalf("parsing pid from launchctl print: %v", err)
	}
	return pid
}
