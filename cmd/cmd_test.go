package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"--version"}, "1.2.3", fstest.MapFS{}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out.String()); got != "multimux 1.2.3" {
		t.Fatalf("output = %q, want %q", got, "multimux 1.2.3")
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"bogus"}, "dev", fstest.MapFS{}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Fatalf("stderr should print usage, got %q", errOut.String())
	}
}

func TestServiceUsageMentionsLogs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Execute([]string{"service", "bogus"}, "dev", fstest.MapFS{}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "install|uninstall|upgrade|status|logs") {
		t.Fatalf("service usage should mention logs, got %q", errOut.String())
	}
}

func TestTopLevelUsageMentionsServiceLogs(t *testing.T) {
	var out, errOut bytes.Buffer
	Execute(nil, "dev", fstest.MapFS{}, &out, &errOut)
	if !strings.Contains(errOut.String(), "install|uninstall|upgrade|status|logs") {
		t.Fatalf("usage should mention service logs, got %q", errOut.String())
	}
}

func TestNoArgsPrintsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute(nil, "dev", fstest.MapFS{}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// stubServiceInstall makes the service-management calls inert so the upgrade
// tests never touch the real launchd/systemd unit on the machine running them.
func stubServiceInstall(t *testing.T, installed bool) *[]string {
	t.Helper()
	origInstalled, origInstall := serviceUnitInstalled, installService
	t.Cleanup(func() { serviceUnitInstalled, installService = origInstalled, origInstall })
	var got []string
	serviceUnitInstalled = func() bool { return installed }
	installService = func(execPath string) error {
		got = append(got, execPath)
		return nil
	}
	return &got
}

func TestServiceUpgradeRunsScriptThenReinstalls(t *testing.T) {
	orig := runUpgradeScript
	t.Cleanup(func() { runUpgradeScript = orig })
	var got string
	runUpgradeScript = func(script string) error {
		got = script
		return nil
	}
	dir := t.TempDir()
	t.Setenv("MULTIMUX_INSTALL_DIR", dir)
	exe := filepath.Join(dir, "multimux")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	installed := stubServiceInstall(t, true)

	var out, errOut bytes.Buffer
	if code := Execute([]string{"service", "upgrade"}, "dev", fstest.MapFS{}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, errOut.String())
	}
	if !strings.Contains(got, "install.sh | sh") {
		t.Fatalf("script = %q", got)
	}
	// The unit must point at the freshly downloaded binary, not at whatever
	// "multimux" a PATH lookup would have found.
	if len(*installed) != 1 || (*installed)[0] != exe {
		t.Fatalf("installService calls = %v, want [%s]", *installed, exe)
	}
}

func TestServiceUpgradeSkipsReinstallWithoutUnit(t *testing.T) {
	orig := runUpgradeScript
	t.Cleanup(func() { runUpgradeScript = orig })
	runUpgradeScript = func(string) error { return nil }
	installed := stubServiceInstall(t, false)

	var out, errOut bytes.Buffer
	if code := Execute([]string{"service", "upgrade"}, "dev", fstest.MapFS{}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, errOut.String())
	}
	if len(*installed) != 0 {
		t.Fatalf("installService called %v, want no service install", *installed)
	}
	if !strings.Contains(out.String(), "no service unit is installed") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestServiceUpgradeReportsFailure(t *testing.T) {
	orig := runUpgradeScript
	t.Cleanup(func() { runUpgradeScript = orig })
	runUpgradeScript = func(string) error { return errors.New("boom") }
	var out, errOut bytes.Buffer
	if code := Execute([]string{"service", "upgrade"}, "dev", fstest.MapFS{}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

// A restart that genuinely fails leaves the binary and the unit installed, so
// the message has to say which step is outstanding — otherwise "upgrade
// failed" reads as "start over".
func TestServiceUpgradeRestartFailureSaysWhatIsLeft(t *testing.T) {
	origScript := runUpgradeScript
	t.Cleanup(func() { runUpgradeScript = origScript })
	runUpgradeScript = func(string) error { return nil }
	dir := t.TempDir()
	t.Setenv("MULTIMUX_INSTALL_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "multimux"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubServiceInstall(t, true)
	origInstall := installService
	t.Cleanup(func() { installService = origInstall })
	installService = func(string) error { return errors.New("the previous daemon did not stop") }

	var out, errOut bytes.Buffer
	if code := Execute([]string{"service", "upgrade"}, "dev", fstest.MapFS{}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "not restarted") {
		t.Fatalf("stderr = %q, want it to say the daemon was not restarted", errOut.String())
	}
}
