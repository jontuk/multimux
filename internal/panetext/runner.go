package panetext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	maxAgentOutput = 1 << 20
	outputSchema   = `{"type":"object","properties":{"join":{"type":"array","items":{"type":"integer"},"uniqueItems":true}},"required":["join"],"additionalProperties":false}`
)

type agent struct {
	name  string
	model string
	path  string
}

type cappedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	remaining := b.limit - b.buffer.Len()
	if remaining < len(p) {
		b.truncated = true
	}
	if remaining <= 0 {
		return written, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return written, nil
}

func (b *cappedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

func (b *cappedBuffer) String() string {
	return b.buffer.String()
}

func discoverAgent(lookPath func(string) (string, error)) (agent, bool) {
	if path, err := lookPath("codex"); err == nil {
		return agent{name: "codex", model: "gpt-5.6-luna", path: path}, true
	}
	if path, err := lookPath("claude"); err == nil {
		return agent{name: "claude", model: "sonnet-5", path: path}, true
	}
	return agent{}, false
}

func commandFor(ctx context.Context, a agent, schemaPath string) *exec.Cmd {
	var cmd *exec.Cmd
	if a.name == "codex" {
		cmd = exec.CommandContext(ctx, a.path,
			"exec",
			"--model", a.model,
			"--sandbox", "read-only",
			"--ephemeral",
			"--ignore-user-config",
			"--ignore-rules",
			"--skip-git-repo-check",
			"--color", "never",
			"--output-schema", schemaPath,
			"-",
		)
	} else {
		cmd = exec.CommandContext(ctx, a.path,
			"--print",
			"--model", a.model,
			"--tools", "",
			"--disable-slash-commands",
			"--no-session-persistence",
			"--output-format", "json",
			"--json-schema", outputSchema,
		)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	return cmd
}

func runAgent(ctx context.Context, a agent, chunk promptChunk) (map[int]bool, error) {
	tempDir, err := os.MkdirTemp("", "multimux-pane-text-")
	if err != nil {
		return nil, cleanupError(a)
	}
	defer os.RemoveAll(tempDir)

	schemaPath := filepath.Join(tempDir, "boundary-schema.json")
	if err := os.WriteFile(schemaPath, []byte(outputSchema), 0o600); err != nil {
		return nil, cleanupError(a)
	}

	cmd := commandFor(ctx, a, schemaPath)
	cmd.Dir = tempDir
	cmd.Stdin = bytes.NewReader(chunk.prompt)
	cmd.WaitDelay = 100 * time.Millisecond
	stdout := cappedBuffer{limit: maxAgentOutput}
	stderr := cappedBuffer{limit: maxAgentOutput}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stdout.truncated || stderr.truncated {
		return nil, errors.New(a.name + " output exceeded limit")
	}
	if runErr != nil {
		return nil, cleanupError(a)
	}

	response := stdout.Bytes()
	if a.name == "claude" {
		var envelope struct {
			StructuredOutput json.RawMessage `json:"structured_output"`
		}
		if err := json.Unmarshal(response, &envelope); err != nil || len(envelope.StructuredOutput) == 0 || bytes.Equal(envelope.StructuredOutput, []byte("null")) {
			return nil, cleanupError(a)
		}
		response = envelope.StructuredOutput
		joins, err := validateJoins(response, chunk)
		if err != nil {
			return nil, cleanupError(a)
		}
		return joins, nil
	}

	return validateJoins(response, chunk)
}

func cleanupError(a agent) error {
	return errors.New(a.name + " cleanup failed")
}
