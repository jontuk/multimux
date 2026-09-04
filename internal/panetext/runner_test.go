package panetext

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestDiscoverAgentPrefersCodex(t *testing.T) {
	wanted := map[string]string{
		"codex":  "/tools/codex",
		"claude": "/tools/claude",
	}
	var lookedUp []string

	got, ok := discoverAgent(func(name string) (string, error) {
		lookedUp = append(lookedUp, name)
		if path, found := wanted[name]; found {
			return path, nil
		}
		return "", os.ErrNotExist
	})

	if !ok {
		t.Fatal("discoverAgent reported no agent")
	}
	if want := (agent{name: "codex", model: "gpt-5.6-luna", path: "/tools/codex"}); got != want {
		t.Fatalf("discoverAgent = %#v, want %#v", got, want)
	}
	if want := []string{"codex"}; !reflect.DeepEqual(lookedUp, want) {
		t.Fatalf("lookups = %v, want %v", lookedUp, want)
	}
}

func TestDiscoverAgentFallsBackToClaude(t *testing.T) {
	got, ok := discoverAgent(func(name string) (string, error) {
		if name == "claude" {
			return "/tools/claude", nil
		}
		return "", os.ErrNotExist
	})

	if !ok {
		t.Fatal("discoverAgent reported no agent")
	}
	if want := (agent{name: "claude", model: "sonnet-5", path: "/tools/claude"}); got != want {
		t.Fatalf("discoverAgent = %#v, want %#v", got, want)
	}
}

func TestDiscoverAgentReturnsFalseWhenUnavailable(t *testing.T) {
	got, ok := discoverAgent(func(string) (string, error) {
		return "", os.ErrNotExist
	})
	if ok || got != (agent{}) {
		t.Fatalf("discoverAgent = (%#v, %v), want zero agent and false", got, ok)
	}
}

func TestRunCodexUsesIsolatedInvocationAndParsesResponse(t *testing.T) {
	t.Setenv("PANETEXT_RUNNER_TEST_ENV", "inherited")
	temp := t.TempDir()
	argsPath := filepath.Join(temp, "args")
	stdinPath := filepath.Join(temp, "stdin")
	cwdPath := filepath.Join(temp, "cwd")
	schemaCopyPath := filepath.Join(temp, "schema")
	modePath := filepath.Join(temp, "mode")
	envPath := filepath.Join(temp, "env")
	executable := writeFakeAgent(t, temp, "codex", fmt.Sprintf(`
printf '%%s\n' "$@" > %s
cat > %s
pwd > %s
cp "${13}" %s
(stat -c '%%a' "${13}" 2>/dev/null || stat -f '%%Lp' "${13}") > %s
printf '%%s' "$PANETEXT_RUNNER_TEST_ENV" > %s
printf '%%s' '{"join":[1]}'
`, shellQuote(argsPath), shellQuote(stdinPath), shellQuote(cwdPath), shellQuote(schemaCopyPath), shellQuote(modePath), shellQuote(envPath)))
	chunk := promptChunk{prompt: []byte("terminal prompt only"), ids: map[int]struct{}{1: {}}}

	joins, err := runAgent(context.Background(), agent{name: "codex", model: "gpt-5.6-luna", path: executable}, chunk)
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if !reflect.DeepEqual(joins, map[int]bool{1: true}) {
		t.Fatalf("joins = %v, want map[1:true]", joins)
	}

	args := readLines(t, argsPath)
	wantArgs := []string{
		"exec", "--model", "gpt-5.6-luna", "--sandbox", "read-only",
		"--ephemeral", "--ignore-user-config", "--ignore-rules",
		"--skip-git-repo-check", "--color", "never", "--output-schema",
		args[12], "-",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
	if got := readFile(t, stdinPath); got != string(chunk.prompt) {
		t.Fatalf("stdin = %q, want %q", got, chunk.prompt)
	}
	if got := readFile(t, schemaCopyPath); got != outputSchema {
		t.Fatalf("schema = %q, want %q", got, outputSchema)
	}
	if got := strings.TrimSpace(readFile(t, modePath)); got != "600" {
		t.Fatalf("schema mode = %q, want 600", got)
	}
	if got := readFile(t, envPath); got != "inherited" {
		t.Fatalf("inherited environment = %q, want inherited", got)
	}
	cwd := strings.TrimSpace(readFile(t, cwdPath))
	if !strings.Contains(filepath.Base(cwd), "multimux-pane-text-") {
		t.Fatalf("working directory = %q, want multimux-pane-text- prefix", cwd)
	}
	if _, err := os.Stat(cwd); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary working directory still exists: %v", err)
	}
}

func TestRunCodexRejectsJoinOutsideChunk(t *testing.T) {
	temp := t.TempDir()
	executable := writeFakeAgent(t, temp, "codex", `
cat >/dev/null
printf '%s' '{"join":[99]}'
`)
	chunk := promptChunk{prompt: []byte("prompt"), ids: map[int]struct{}{1: {}}}

	_, err := runAgent(context.Background(), agent{name: "codex", model: "gpt-5.6-luna", path: executable}, chunk)
	if err == nil || !strings.Contains(err.Error(), "outside chunk authority") {
		t.Fatalf("runAgent error = %v, want outside-chunk rejection", err)
	}
}

func TestRunClaudeUsesRequiredInvocationAndUnwrapsStructuredOutput(t *testing.T) {
	temp := t.TempDir()
	argsPath := filepath.Join(temp, "args")
	stdinPath := filepath.Join(temp, "stdin")
	executable := writeFakeAgent(t, temp, "claude", fmt.Sprintf(`
printf '%%s\n' "$@" > %s
cat > %s
printf '%%s' '{"structured_output":{"join":[2]}}'
`, shellQuote(argsPath), shellQuote(stdinPath)))
	chunk := promptChunk{prompt: []byte("only this prompt"), ids: map[int]struct{}{2: {}}}

	joins, err := runAgent(context.Background(), agent{name: "claude", model: "sonnet-5", path: executable}, chunk)
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if !reflect.DeepEqual(joins, map[int]bool{2: true}) {
		t.Fatalf("joins = %v, want map[2:true]", joins)
	}
	wantArgs := []string{
		"--print", "--model", "sonnet-5", "--tools", "", "--disable-slash-commands",
		"--no-session-persistence", "--output-format", "json", "--json-schema", outputSchema,
	}
	if got := readLines(t, argsPath); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %#v, want %#v", got, wantArgs)
	}
	if got := readFile(t, stdinPath); got != string(chunk.prompt) {
		t.Fatalf("stdin = %q, want %q", got, chunk.prompt)
	}
}

func TestRunClaudeRejectsMalformedOrMissingStructuredOutputPrivately(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{name: "malformed envelope", output: `not-json SECRET`},
		{name: "missing structured output", output: `{"result":"SECRET"}`},
		{name: "null structured output", output: `{"structured_output":null,"result":"SECRET"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			temp := t.TempDir()
			executable := writeFakeAgent(t, temp, "claude", "cat >/dev/null\nprintf '%s' "+shellQuote(tc.output)+"\n")
			chunk := promptChunk{prompt: []byte("prompt SECRET"), ids: map[int]struct{}{1: {}}}

			_, err := runAgent(context.Background(), agent{name: "claude", model: "sonnet-5", path: executable}, chunk)
			if err == nil {
				t.Fatal("runAgent succeeded, want error")
			}
			if got := err.Error(); got != "claude cleanup failed" {
				t.Fatalf("error = %q, want private generic error", got)
			}
		})
	}
}

func TestRunAgentCapsOutput(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "stdout", command: "dd if=/dev/zero bs=1048577 count=1 2>/dev/null"},
		{name: "stderr", command: "dd if=/dev/zero bs=1048577 count=1 2>/dev/null | cat >&2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			temp := t.TempDir()
			body := "cat >/dev/null\n" + tc.command + "\n"
			executable := writeFakeAgent(t, temp, "codex", body)

			_, err := runAgent(context.Background(), agent{name: "codex", model: "gpt-5.6-luna", path: executable}, promptChunk{prompt: []byte("prompt")})
			if err == nil || err.Error() != "codex output exceeded limit" {
				t.Fatalf("runAgent error = %v, want output limit error", err)
			}
		})
	}
}

func TestRunAgentNonzeroFailureIsPrivate(t *testing.T) {
	temp := t.TempDir()
	executable := writeFakeAgent(t, temp, "codex", `
cat >/dev/null
printf '%s' 'STDOUT SECRET'
printf '%s' 'STDERR SECRET' >&2
exit 7
`)

	_, err := runAgent(context.Background(), agent{name: "codex", model: "gpt-5.6-luna", path: executable}, promptChunk{prompt: []byte("PROMPT SECRET")})
	if err == nil || err.Error() != "codex cleanup failed" {
		t.Fatalf("runAgent error = %v, want private generic error", err)
	}
}

func TestRunAgentCancellationReturnsContextErrorPromptly(t *testing.T) {
	temp := t.TempDir()
	childPIDPath := filepath.Join(temp, "child-pid")
	executable := writeFakeAgent(t, temp, "codex", fmt.Sprintf(`
cat >/dev/null
sleep 10 &
child=$!
printf '%%s' "$child" > %s
wait
`, shellQuote(childPIDPath)))
	ctx := newControlledDeadlineContext()
	defer ctx.expire()
	result := make(chan error, 1)
	go func() {
		_, err := runAgent(ctx, agent{name: "codex", model: "gpt-5.6-luna", path: executable}, promptChunk{prompt: []byte("prompt")})
		result <- err
	}()

	startDeadline := time.Now().Add(2 * time.Second)
	var childPID int
	for {
		if body, err := os.ReadFile(childPIDPath); err == nil {
			if pid, err := strconv.Atoi(string(body)); err == nil && pid > 0 {
				childPID = pid
				break
			}
		}
		if time.Now().After(startDeadline) {
			t.Fatal("fake agent did not record a valid child PID")
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	ctx.expire()
	var err error
	select {
	case err = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("runAgent did not return within 2s of cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runAgent error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("cancellation took %v, want under 2s", elapsed)
	}

	t.Cleanup(func() {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	})
	deadline := time.Now().Add(time.Second)
	for processExists(childPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processExists(childPID) {
		t.Fatalf("child process %d survived cancellation", childPID)
	}
}

func TestRunAgentKillsDescendantsAfterSuccessfulExit(t *testing.T) {
	temp := t.TempDir()
	childPIDPath := filepath.Join(temp, "child-pid")
	executable := writeFakeAgent(t, temp, "codex", fmt.Sprintf(`
cat >/dev/null
(trap '' HUP; exec sleep 10) </dev/null >/dev/null 2>&1 &
child=$!
printf '%%s' "$child" > %s
printf '%%s' '{"join":[1]}'
`, shellQuote(childPIDPath)))
	chunk := promptChunk{prompt: []byte("prompt"), ids: map[int]struct{}{1: {}}}

	joins, err := runAgent(context.Background(), agent{name: "codex", model: "gpt-5.6-luna", path: executable}, chunk)
	if err != nil {
		t.Fatalf("runAgent: %v", err)
	}
	if !reflect.DeepEqual(joins, map[int]bool{1: true}) {
		t.Fatalf("joins = %v, want map[1:true]", joins)
	}
	childPID, err := strconv.Atoi(readFile(t, childPIDPath))
	if err != nil {
		t.Fatalf("parse child PID: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	})
	deadline := time.Now().Add(time.Second)
	for processExists(childPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processExists(childPID) {
		t.Fatalf("child process %d survived successful agent exit", childPID)
	}
}

func TestRunAgentRefusesToKillControllerProcessGroup(t *testing.T) {
	if err := killProcessGroup(syscall.Getpgrp()); err == nil {
		t.Fatal("killProcessGroup accepted the controller process group")
	}
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

type controlledDeadlineContext struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newControlledDeadlineContext() *controlledDeadlineContext {
	return &controlledDeadlineContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *controlledDeadlineContext) Done() <-chan struct{} {
	return c.done
}

func (c *controlledDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (c *controlledDeadlineContext) expire() {
	c.once.Do(func() { close(c.done) })
}

func TestRunAgentCappedBufferAcceptsFullWrites(t *testing.T) {
	var buffer cappedBuffer
	buffer.limit = 3
	n, err := buffer.Write([]byte("abcde"))
	if err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v), want (5, nil)", n, err)
	}
	if got := buffer.String(); got != "abc" {
		t.Fatalf("retained output = %q, want abc", got)
	}
	if !buffer.truncated {
		t.Fatal("truncated = false, want true")
	}
}

func writeFakeAgent(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	contents := "#!/bin/sh\nset -eu\n" + body
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	contents := strings.TrimSuffix(readFile(t, path), "\n")
	if contents == "" {
		return nil
	}
	return strings.Split(contents, "\n")
}

func TestRunAgentOutputSchemaIsExact(t *testing.T) {
	want := `{"type":"object","properties":{"join":{"type":"array","items":{"type":"integer"},"uniqueItems":true}},"required":["join"],"additionalProperties":false}`
	if outputSchema != want {
		t.Fatalf("outputSchema = %s, want %s", strconv.Quote(outputSchema), strconv.Quote(want))
	}
}
