// Package tmuxmgr manages multimux's tmux sessions and PTY attachments.
package tmuxmgr

import (
	"bytes"
	"errors"
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
//
// A group launch multiplies this, so it costs three tmux processes: the
// error-checked create, one best-effort chain of options, and the
// error-checked respawn. They cannot merge further because a tmux chain stops
// at its first failing command and reports one exit status for the lot.
func (m *Manager) CreateSession(name, dir, command string) error {
	// history-limit must be set globally BEFORE new-session: pane scrollback
	// capacity is fixed when the pane is created. 50000 lines gives wheel
	// scrollback real depth (tmux default is 2000). It is chained with
	// start-server and new-session in ONE tmux invocation: before the first
	// session the server doesn't exist (a lone set-option fails), and a server
	// started with no sessions exits again (exit-empty) before a second
	// invocation could reach it.
	if err := m.run("start-server", ";",
		"set-option", "-g", "history-limit", "50000", ";",
		"new-session", "-d", "-s", name, "-c", dir); err != nil {
		return err
	}
	target := ExactTarget(name)
	setup := [][]string{{"set-environment", "-t", target, "LANG", "en_US.UTF-8"}}
	if p := os.Getenv("PATH"); p != "" {
		setup = append(setup, []string{"set-environment", "-t", target, "PATH", p})
	}
	setup = append(setup, sessionOptions(target)...)
	// Server options go with every create, not just daemon start: tmux exits
	// when its last session does, and the server this create may have just
	// started has none of them.
	setup = append(setup, serverOptions()...)
	_ = m.run(chain(setup)...)
	if command != "" {
		if err := m.run("respawn-pane", "-k", "-c", dir, "-t", target, command); err != nil {
			// The caller rolls its DB row back on error; the fresh tmux
			// session must go with it or it becomes an unreachable orphan.
			_ = m.KillSession(name)
			return err
		}
	}
	return nil
}

// ConfigureServer applies multimux's options to a tmux server that is already
// running, in one tmux process. The server survives daemon upgrades, so the
// daemon calls this once at start to repair sessions and server options an
// older multimux left behind; Attach no longer re-asserts them per
// connection. With no server running there is nothing to repair, and the
// failed call starts none.
func (m *Manager) ConfigureServer() {
	names, err := m.ListSessions()
	if err != nil {
		return
	}
	var setup [][]string
	for _, name := range names {
		// On the default socket the server is shared with the user's own
		// sessions, which must keep whatever sizing they chose.
		if strings.HasPrefix(name, m.prefix+"-") {
			setup = append(setup, sessionOptions(ExactTarget(name))...)
		}
	}
	setup = append(setup, serverOptions()...)
	_ = m.run(chain(setup)...)
}

// sessionOptions are the per-session settings every multimux session gets.
func sessionOptions(target string) [][]string {
	return [][]string{
		{"set-option", "-t", target, "remain-on-exit", "on"},
		{"set-option", "-t", target, "status", "off"},
		// Manual sizing: tmux must never auto-shrink the window to the
		// smallest or latest attached client (e.g. a stale client from a
		// machine that is now off). multimux drives size explicitly via
		// resize-window from the arbiter-elected owner connection.
		{"set-option", "-t", target, "window-size", "manual"},
		// Mouse mode: wheel/trackpad scrolls tmux copy-mode instead of
		// xterm.js synthesizing up/down keys (which the shell would treat as
		// history navigation). Scoped per session; user's other tmux sessions
		// untouched. Trade-off: tmux owns click-drag selection; hold
		// Option/Shift for native browser selection.
		{"set-option", "-t", target, "mouse", "on"},
	}
}

// serverOptions are the server-wide settings multimux depends on. They are
// ordered by the tmux release that introduced them, oldest first: a chain
// stops at its first failing command, so an option an older tmux lacks must
// come after every option it has.
func serverOptions() [][]string {
	return [][]string{
		// OSC 52 passthrough: copy-mode yanks reach the browser clipboard via
		// xterm.js ClipboardAddon. terminal-features tells tmux the attached
		// client (xterm.js) supports the clipboard escape sequence.
		setArrayEntry("terminal-features", 90, "xterm*:clipboard"),
		{"set-option", "-s", "set-clipboard", "on"},
		setArrayEntry("terminal-features", 91, "xterm*:extkeys"),
		// The browser sends Shift+Enter as CSI u. "on" only preserves extended
		// keys while the pane application has requested the protocol; "always"
		// also preserves them at ordinary prompts and in applications unaware
		// of extended keys. Claude Code requests the protocol itself, which
		// otherwise masks this difference.
		{"set-option", "-s", "extended-keys", "always"},
		// tmux defaults extended-keys-format to "xterm", which re-encodes the
		// CSI u the browser sent as the older CSI 27;mods;key~ form before
		// handing it to the pane. Pin the format so applications see the
		// sequence xterm.js actually produced, whatever the user's own
		// tmux.conf says. tmux only grew the option in 3.5, hence last.
		{"set-option", "-s", "extended-keys-format", "csi-u"},
	}
}

// setArrayEntry sets one entry of a server array option at a fixed index. A
// plain `set-option -a` adds a duplicate every time it runs, and this runs on
// every create for the life of the tmux server. Guarding the append with a
// format match does not work portably either: on tmux 3.4 an unindexed
// array option expands to nothing in a format, so the guard never matches.
// tmux arrays are sparse, so a high index stays clear of the entries a user's
// tmux.conf appends from the bottom.
func setArrayEntry(option string, index int, value string) []string {
	return []string{"set-option", "-s", fmt.Sprintf("%s[%d]", option, index), value}
}

// chain joins tmux commands with ";" so they run in one tmux invocation.
func chain(cmds [][]string) []string {
	var args []string
	for i, c := range cmds {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args, c...)
	}
	return args
}

// sessionAbsent reports whether a tmux error message means the target session
// (or the whole tmux server) is already gone, as opposed to a real failure.
func sessionAbsent(msg string) bool {
	return strings.Contains(msg, "can't find session") ||
		strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "No such file")
}

// ErrSessionUnavailable means the requested tmux session, or its tmux server,
// disappeared before pane text could be captured.
var ErrSessionUnavailable = errors.New("tmux session unavailable")

// CapturePaneText returns the active pane's retained history and current
// screen as plain text. tmux owns logical boundaries: -J joins only rows tmux
// marks wrapped, while -S/-E include all retained history and the full screen.
// Leading and trailing whitespace is stripped: retained history that has not
// filled yet pads the top with blank lines, and the unused rows below the
// cursor pad the bottom, neither of which is pane content.
func (m *Manager) CapturePaneText(name string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("tmux", m.baseArgs(
		"capture-pane", "-pJ", "-S", "-", "-E", "-", "-t", ExactTarget(name),
	)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if sessionAbsent(msg) {
			return nil, fmt.Errorf("%w: %s", ErrSessionUnavailable, msg)
		}
		if msg != "" {
			return nil, fmt.Errorf("tmux capture-pane: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("tmux capture-pane: %w", err)
	}
	return bytes.TrimSpace(stdout.Bytes()), nil
}

// KillSession destroys the session. A session that is already gone — or a
// tmux server that isn't running at all — counts as success: the goal is
// absence. Any other error is a real failure the caller must handle.
func (m *Manager) KillSession(name string) error {
	err := m.run("kill-session", "-t", ExactTarget(name))
	if err != nil && sessionAbsent(err.Error()) {
		return nil
	}
	return err
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
		if sessionAbsent(msg) || strings.Contains(msg, "no sessions") {
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
