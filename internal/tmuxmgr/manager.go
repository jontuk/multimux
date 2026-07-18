// Package tmuxmgr manages multimux's tmux sessions and PTY attachments.
package tmuxmgr

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Manager manages tmux sessions sharing a name prefix. A non-empty socket
// name isolates all commands onto a private tmux server (tmux -L), used by
// dev mode and tests so they never touch the user's sessions.
type Manager struct {
	prefix string
	socket string
}

func New(prefix, socket string) *Manager {
	return &Manager{prefix: prefix, socket: socket}
}

// SessionName returns the canonical tmux session name for a session row ID.
func (m *Manager) SessionName(id int64) string {
	return fmt.Sprintf("%s-%d", m.prefix, id)
}

// Available reports whether tmux is installed.
func (m *Manager) Available() error {
	_, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found in PATH: %w", err)
	}
	return nil
}

// ExactTarget returns name in tmux's exact-match target syntax "=name:".
// The "=" forces an exact name match instead of tmux's default prefix match:
// without it "-t mm-4" resolves to "mm-42" once "mm-4" is gone, so commands
// like kill-session could destroy the wrong live session. The trailing ":"
// (empty window index) is load-bearing too: several subcommands parse -t as a
// pane target and a bare "=name" fails to resolve for them (respawn-pane,
// pipe-pane, set-option error with "can't find pane"/"no such session" on
// tmux 3.7b) even though has-session succeeds. "=name:" resolves uniformly
// everywhere, and session-level commands still act on the whole session.
func ExactTarget(name string) string {
	return "=" + name + ":"
}

// CreateSession creates a detached session named name in dir. When command is
// non-empty it is launched via respawn-pane AFTER remain-on-exit is set,
// avoiding the race where a fast-exiting command kills the pane before
// remain-on-exit takes effect.
func (m *Manager) CreateSession(name, dir, command string) error {
	// history-limit must be set globally BEFORE new-session: pane scrollback
	// capacity is fixed when the pane is created. 50000 lines gives wheel
	// scrollback real depth (tmux default is 2000).
	_ = m.run("set-option", "-g", "history-limit", "50000")
	if err := m.run("new-session", "-d", "-s", name, "-c", dir); err != nil {
		return err
	}
	target := ExactTarget(name)
	_ = m.run("set-environment", "-t", target, "LANG", "en_US.UTF-8")
	if p := os.Getenv("PATH"); p != "" {
		_ = m.run("set-environment", "-t", target, "PATH", p)
	}
	_ = m.run("set-option", "-t", target, "remain-on-exit", "on")
	_ = m.run("set-option", "-t", target, "status", "off")
	// Manual sizing: tmux must never auto-shrink the window to the smallest
	// or latest attached client (e.g. a stale client from a machine that is
	// now off). multimux drives size explicitly via resize-window from the
	// arbiter-elected owner connection.
	_ = m.run("set-option", "-t", target, "window-size", "manual")
	// Mouse mode: wheel/trackpad scrolls tmux copy-mode instead of xterm.js
	// synthesizing up/down keys (which the shell would treat as history
	// navigation). Scoped per session; user's other tmux sessions untouched.
	// Trade-off: tmux owns click-drag selection; hold Option/Shift for native
	// browser selection.
	_ = m.run("set-option", "-t", target, "mouse", "on")
	_ = m.run("set-option", "-s", "-a", "terminal-features", "xterm*:extkeys")
	_ = m.run("set-option", "-s", "extended-keys", "on")
	// OSC 52 passthrough: copy-mode yanks reach the browser clipboard via
	// xterm.js ClipboardAddon. terminal-features tells tmux the attached
	// client (xterm.js) supports the clipboard escape sequence.
	_ = m.run("set-option", "-s", "-a", "terminal-features", "xterm*:clipboard")
	_ = m.run("set-option", "-s", "set-clipboard", "on")
	if command != "" {
		if err := m.run("respawn-pane", "-k", "-c", dir, "-t", target, command); err != nil {
			return err
		}
	}
	return nil
}

// KillSession destroys the session.
func (m *Manager) KillSession(name string) error {
	return m.run("kill-session", "-t", ExactTarget(name))
}

// ListSessions returns all session names on the server; nil,nil when the
// server is not running or has no sessions.
func (m *Manager) ListSessions() ([]string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("tmux", m.baseArgs("list-sessions", "-F", "#{session_name}")...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		// tmux prints these when there is nothing to list — not an error.
		if strings.Contains(msg, "no server running") ||
			strings.Contains(msg, "no sessions") ||
			strings.Contains(msg, "No such file") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions: %w: %s", err, msg)
	}
	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// IsAlive reports whether the session exists (exact-match; see ExactTarget).
func (m *Manager) IsAlive(name string) bool {
	return m.run("has-session", "-t", ExactTarget(name)) == nil
}

// baseArgs prepends -u (force UTF-8 regardless of daemon locale — launchd
// provides none) and the private socket when set.
func (m *Manager) baseArgs(args ...string) []string {
	base := []string{"-u"}
	if m.socket != "" {
		base = append(base, "-L", m.socket)
	}
	return append(base, args...)
}

func (m *Manager) run(args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("tmux", m.baseArgs(args...)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("tmux %s: %w: %s", args[0], err, msg)
		}
		return fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return nil
}
