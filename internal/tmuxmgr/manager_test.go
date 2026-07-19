package tmuxmgr

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

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
