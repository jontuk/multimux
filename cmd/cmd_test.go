package cmd

import (
	"bytes"
	"errors"
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

func TestServiceUpgradeRunsScript(t *testing.T) {
	orig := runUpgradeScript
	t.Cleanup(func() { runUpgradeScript = orig })
	var got string
	runUpgradeScript = func(script string) error {
		got = script
		return nil
	}
	var out, errOut bytes.Buffer
	if code := Execute([]string{"service", "upgrade"}, "dev", fstest.MapFS{}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, errOut.String())
	}
	if !strings.Contains(got, "install.sh | sh") || !strings.Contains(got, "&& multimux service install") {
		t.Fatalf("script = %q", got)
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
