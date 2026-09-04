package tmuxmgr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func fakeCaptureTmux(t *testing.T, script string) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + logPath + "\"\n" + script
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return New("mm", "private-socket"), logPath
}

func TestCapturePaneTextUsesExactJoinedHistoryCapture(t *testing.T) {
	m, logPath := fakeCaptureTmux(t, "printf 'alpha β\\n'\nprintf 'ignored warning\\n' >&2\n")
	got, err := m.CapturePaneText(context.Background(), "mm-7")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha β\n" {
		t.Fatalf("CapturePaneText = %q", got)
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "-u\n-L\nprivate-socket\ncapture-pane\n-pJ\n-S\n-\n-E\n-\n-t\n=mm-7:\n"
	if string(args) != want {
		t.Fatalf("tmux args = %q, want %q", args, want)
	}
}

func TestCapturePaneTextAllowsEmptyOutput(t *testing.T) {
	m, _ := fakeCaptureTmux(t, "exit 0\n")
	got, err := m.CapturePaneText(context.Background(), "mm-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("CapturePaneText = %q, %v; want empty success", got, err)
	}
}

func TestCapturePaneTextClassifiesMissingSessionAndServer(t *testing.T) {
	for _, stderr := range []string{"can't find session: mm-1", "no server running on /tmp/tmux.sock"} {
		t.Run(stderr, func(t *testing.T) {
			script := fmt.Sprintf("printf '%%s\\n' %q >&2\nexit 1\n", stderr)
			m, _ := fakeCaptureTmux(t, script)
			_, err := m.CapturePaneText(context.Background(), "mm-1")
			if !errors.Is(err, ErrSessionUnavailable) {
				t.Fatalf("error = %v, want ErrSessionUnavailable", err)
			}
		})
	}
}

func TestCapturePaneTextFailureDoesNotExposeStdout(t *testing.T) {
	m, _ := fakeCaptureTmux(t, "printf 'pane-secret'\nprintf 'permission denied' >&2\nexit 2\n")
	_, err := m.CapturePaneText(context.Background(), "mm-1")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "pane-secret") {
		t.Fatalf("capture stdout leaked through error: %v", err)
	}
}

func TestCapturePaneTextStopsWhenContextDeadlineExpires(t *testing.T) {
	m, _ := fakeCaptureTmux(t, "exec /bin/sleep 60\n")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()

	_, err := m.CapturePaneText(ctx, "mm-1")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("CapturePaneText returned after %v, want within 1s", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestCapturePaneTextCancellationTakesPrecedenceOverSessionAbsentStderr(t *testing.T) {
	m, _ := fakeCaptureTmux(t, "printf \"can't find session: mm-1\\n\" >&2\nexec /bin/sleep 60\n")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()

	_, err := m.CapturePaneText(ctx, "mm-1")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("CapturePaneText returned after %v, want within 1s", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("error = %v, must not classify canceled capture as ErrSessionUnavailable", err)
	}
	if strings.Contains(err.Error(), "can't find session") {
		t.Fatalf("canceled capture error exposed stderr: %v", err)
	}
}

func waitForCapture(t *testing.T, m *Manager, name, want string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := m.CapturePaneText(context.Background(), name)
		if err == nil && strings.Contains(string(got), want) {
			return string(got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := m.CapturePaneText(context.Background(), name)
	t.Fatalf("capture never contained %q: %q, %v", want, got, err)
	return ""
}

func TestCapturePaneTextJoinsSoftWrapsAndKeepsHardNewlines(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(20)
	if err := m.CreateSession(name, t.TempDir(), "sleep 0.2; printf 'hard-line\\nabcdefghijklmnopqr\\n'; sleep 60"); err != nil {
		t.Fatal(err)
	}
	if err := m.run("resize-window", "-x", "12", "-y", "8", "-t", ExactTarget(name)); err != nil {
		t.Fatal(err)
	}
	got := waitForCapture(t, m, name, "abcdefghijklmnopqr")
	if !strings.Contains(got, "hard-line\nabcdefghijklmnopqr") {
		t.Fatalf("hard newline or soft wrap lost: %q", got)
	}
}

func TestCapturePaneTextTargetsExactSession(t *testing.T) {
	m := testManager(t)
	for _, item := range []struct{ name, marker string }{
		{m.SessionName(4), "exact-four"}, {m.SessionName(42), "forty-two"},
	} {
		if err := m.CreateSession(item.name, t.TempDir(), "printf '"+item.marker+"\\n'; sleep 60"); err != nil {
			t.Fatal(err)
		}
		waitForCapture(t, m, item.name, item.marker)
	}
	if err := m.KillSession(m.SessionName(4)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CapturePaneText(context.Background(), m.SessionName(4)); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("capture prefix-matched mm-42: %v", err)
	}
}

func TestCapturePaneTextUsesOnlyActivePane(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(21)
	if err := m.CreateSession(name, t.TempDir(), "printf 'left-pane\\n'; sleep 60"); err != nil {
		t.Fatal(err)
	}
	waitForCapture(t, m, name, "left-pane")
	if err := m.run("split-window", "-d", "-t", ExactTarget(name), "printf 'right-pane\\n'; sleep 60"); err != nil {
		t.Fatal(err)
	}
	left := waitForCapture(t, m, name, "left-pane")
	if strings.Contains(left, "right-pane") {
		t.Fatalf("inactive pane was concatenated: %q", left)
	}
	panes, err := exec.Command("tmux", m.baseArgs("list-panes", "-t", ExactTarget(name), "-F", "#{pane_id}")...).Output()
	if err != nil {
		t.Fatal(err)
	}
	paneIDs := strings.Fields(string(panes))
	if len(paneIDs) != 2 {
		t.Fatalf("pane ids = %q, want two panes", panes)
	}
	if err := m.run("select-pane", "-t", paneIDs[1]); err != nil {
		t.Fatal(err)
	}
	right := waitForCapture(t, m, name, "right-pane")
	if strings.Contains(right, "left-pane") {
		t.Fatalf("previous active pane was concatenated: %q", right)
	}
}

func TestSessionName(t *testing.T) {
	m := New("mm", "")
	if got := m.SessionName(7); got != "mm-7" {
		t.Fatalf("SessionName = %q", got)
	}
}

func TestExactTarget(t *testing.T) {
	if got := ExactTarget("mm-4"); got != "=mm-4:" {
		t.Fatalf("ExactTarget = %q, want =mm-4:", got)
	}
}

// testManager returns a Manager bound to a private throwaway tmux server so
// tests never touch the user's sessions. Skips when tmux is absent.
func testManager(t *testing.T) *Manager {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := fmt.Sprintf("mmtest-%d", time.Now().UnixNano())
	m := New("mm", socket)
	t.Cleanup(func() { exec.Command("tmux", "-L", socket, "kill-server").Run() })
	return m
}

func TestCreateListKill(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(1)
	if err := m.CreateSession(name, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if !m.IsAlive(name) {
		t.Fatal("session should be alive")
	}
	names, err := m.ListSessions()
	if err != nil || !slices.Contains(names, name) {
		t.Fatalf("ListSessions = %v, %v", names, err)
	}
	if err := m.KillSession(name); err != nil {
		t.Fatal(err)
	}
	if m.IsAlive(name) {
		t.Fatal("session should be dead")
	}
}

func TestIsAliveExactMatch(t *testing.T) {
	m := testManager(t)
	// Only mm-12 exists; IsAlive("mm-1") must NOT prefix-match it.
	if err := m.CreateSession(m.SessionName(12), t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if m.IsAlive(m.SessionName(1)) {
		t.Fatal("IsAlive(mm-1) prefix-matched mm-12")
	}
}

func TestFastExitingCommandSurvives(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(2)
	// `true` exits instantly; remain-on-exit must keep the pane (and session).
	if err := m.CreateSession(name, t.TempDir(), "true"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if !m.IsAlive(name) {
		t.Fatal("fast-exiting command killed the session — remain-on-exit race regressed")
	}
}

// Killing a session that is already gone achieves the goal — absence — and
// must not read as failure, with or without a running tmux server.
func TestKillMissingSessionIsNotAnError(t *testing.T) {
	m := testManager(t)
	if err := m.KillSession(m.SessionName(1)); err != nil {
		t.Fatalf("kill with no server = %v, want nil", err)
	}
	if err := m.CreateSession(m.SessionName(12), t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if err := m.KillSession(m.SessionName(1)); err != nil {
		t.Fatalf("kill missing session = %v, want nil", err)
	}
}

// A respawn-pane failure after new-session must not leave the fresh tmux
// session behind: the caller sees an error and deletes the DB row, so a
// surviving session would be an unreachable orphan.
func TestFailedRespawnKillsCreatedSession(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(3)
	// An oversized command overflows the exec arg limit, failing respawn-pane.
	if err := m.CreateSession(name, t.TempDir(), strings.Repeat("x", 2<<20)); err == nil {
		t.Fatal("CreateSession with oversized command succeeded, want error")
	}
	if names, err := m.ListSessions(); err != nil || slices.Contains(names, name) {
		t.Fatalf("ListSessions = %v, %v; orphan session survived failed create", names, err)
	}
}

// The very first CreateSession is the one that starts the tmux server, and
// pane scrollback capacity is fixed at pane creation — so history-limit must
// already be set when that first pane appears. Setting it in a separate tmux
// invocation is silently lost because there is no server to accept it yet.
func TestFirstPaneGetsFullHistoryLimit(t *testing.T) {
	m := testManager(t)
	if err := m.CreateSession(m.SessionName(1), t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("tmux", m.baseArgs("show-options", "-g", "-v", "history-limit")...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "50000" {
		t.Fatalf("history-limit after first create = %s, want 50000 (option was set before the server existed)", got)
	}
}

func TestListSessionsNoServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	m := New("mm", fmt.Sprintf("mmtest-none-%d", time.Now().UnixNano()))
	names, err := m.ListSessions()
	if err != nil || names != nil {
		t.Fatalf("no-server ListSessions = %v, %v; want nil, nil", names, err)
	}
}
