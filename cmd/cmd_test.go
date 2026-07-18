package cmd

import (
	"bytes"
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
	if !strings.Contains(errOut.String(), "install|uninstall|status|logs") {
		t.Fatalf("service usage should mention logs, got %q", errOut.String())
	}
}

func TestTopLevelUsageMentionsServiceLogs(t *testing.T) {
	var out, errOut bytes.Buffer
	Execute(nil, "dev", fstest.MapFS{}, &out, &errOut)
	if !strings.Contains(errOut.String(), "install|uninstall|status|logs") {
		t.Fatalf("usage should mention service logs, got %q", errOut.String())
	}
}

func TestNoArgsPrintsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Execute(nil, "dev", fstest.MapFS{}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
