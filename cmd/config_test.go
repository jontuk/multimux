package cmd

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"
)

// run executes the CLI against a throwaway data dir and returns code/stdout/stderr.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute(args, "dev", fstest.MapFS{}, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestConfigListShowsDefaults(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	code, out, errOut := runCLI(t, "config", "list")
	if code != 0 {
		t.Fatalf("code = %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out, "confirm-terminate") {
		t.Fatalf("list missing the setting: %q", out)
	}
	if !strings.Contains(out, "false") {
		t.Fatalf("list missing the effective value: %q", out)
	}
	if !strings.Contains(out, "(default)") {
		t.Fatalf("list should mark unset keys as default: %q", out)
	}
}

func TestConfigSetThenGet(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())

	code, out, errOut := runCLI(t, "config", "set", "confirm-terminate", "true")
	if code != 0 {
		t.Fatalf("set code = %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out, "confirm-terminate = true") {
		t.Fatalf("set should echo the new value: %q", out)
	}
	// The CLI cannot push to open tabs; say so rather than implying it can.
	if !strings.Contains(out, "reload") {
		t.Fatalf("set should say open tabs pick this up on reload: %q", out)
	}

	code, out, errOut = runCLI(t, "config", "get", "confirm-terminate")
	if code != 0 {
		t.Fatalf("get code = %d, stderr %s", code, errOut)
	}
	if strings.TrimSpace(out) != "true" {
		t.Fatalf("get should print the bare value for scripts, got %q", out)
	}

	code, out, _ = runCLI(t, "config", "list")
	if code != 0 {
		t.Fatalf("list code = %d", code)
	}
	if strings.Contains(out, "(default)") {
		t.Fatalf("a stored key must not be marked default: %q", out)
	}
}

func TestConfigRejectsUnknownKey(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	for _, args := range [][]string{
		{"config", "get", "nope"},
		{"config", "set", "nope", "true"},
	} {
		code, _, errOut := runCLI(t, args...)
		if code != 2 {
			t.Fatalf("%v code = %d, want 2", args, code)
		}
		if !strings.Contains(errOut, "nope") {
			t.Fatalf("%v error should name the key: %q", args, errOut)
		}
	}
}

func TestConfigRejectsInvalidValue(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	code, _, errOut := runCLI(t, "config", "set", "confirm-terminate", "yes")
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "true") {
		t.Fatalf("error should name the accepted values: %q", errOut)
	}
}

func TestConfigUsageOnBadInvocation(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	for _, args := range [][]string{
		{"config"},
		{"config", "wat"},
		{"config", "get"},
		{"config", "set", "confirm-terminate"},
	} {
		code, _, errOut := runCLI(t, args...)
		if code != 2 {
			t.Fatalf("%v code = %d, want 2", args, code)
		}
		if !strings.Contains(errOut, "usage: multimux config") {
			t.Fatalf("%v should print usage, got %q", args, errOut)
		}
	}
}

func TestConfigIsDiscoverable(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())

	_, out, _ := runCLI(t, "help")
	if !strings.Contains(out, "config") {
		t.Fatalf("top-level usage does not mention config: %q", out)
	}

	code, out, errOut := runCLI(t, "help", "config")
	if code != 0 {
		t.Fatalf("help config code = %d, stderr %s", code, errOut)
	}
	if !strings.Contains(out, "usage: multimux config") {
		t.Fatalf("help config printed the wrong text: %q", out)
	}
}
