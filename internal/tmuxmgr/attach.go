package tmuxmgr

import (
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/creack/pty/v2"
)

// PTYConn is a pseudoterminal attached to a tmux session.
type PTYConn interface {
	io.ReadWriteCloser
	// Resize sizes this connection's own attach PTY; when resizeWindow is
	// true it also resizes the shared tmux window (only the arbiter-elected
	// owner connection may pass true — see Arbiter).
	Resize(cols, rows uint16, resizeWindow bool) error
}

type tmuxPTY struct {
	ptmx *os.File
	cmd  *exec.Cmd
	mgr  *Manager
	name string
	// cols, rows are the size last set on ptmx, 0 until the first Resize.
	// Nothing but Resize sizes this PTY, so they cannot go stale. Unguarded:
	// Resize is only ever called from the connection's own read loop.
	cols, rows uint16
}

// setsize is pty.Setsize; overridden in tests.
var setsize = pty.Setsize

func (t *tmuxPTY) Read(p []byte) (int, error)  { return t.ptmx.Read(p) }
func (t *tmuxPTY) Write(p []byte) (int, error) { return t.ptmx.Write(p) }

func (t *tmuxPTY) Close() error {
	err := t.ptmx.Close()
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}
	return err
}

func (t *tmuxPTY) Resize(cols, rows uint16, resizeWindow bool) error {
	// A follow-input tile repeats its size with every claim of the window (see
	// Arbiter), so most calls bring the dims the PTY already has.
	if cols != t.cols || rows != t.rows {
		if err := setsize(t.ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
			return err
		}
		t.cols, t.rows = cols, rows
	}
	if !resizeWindow {
		return nil
	}
	// Never skipped for unchanged dims: the window is shared, and tmux, another
	// connection, or the user's own tmux client can have resized it since. The
	// owner's ordinary resizes are what put it back.
	return t.mgr.run("resize-window", "-t", ExactTarget(t.name),
		"-x", strconv.Itoa(int(cols)), "-y", strconv.Itoa(int(rows)))
}

// Attach spawns `tmux attach-session` on a fresh PTY. It spawns nothing
// else: every tile reconnect lands here, and the options a session needs are
// set by CreateSession and, for servers older than the daemon, ConfigureServer.
func (m *Manager) Attach(name string) (PTYConn, error) {
	cmd := exec.Command("tmux", m.baseArgs("attach-session", "-t", ExactTarget(name))...)
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color", "LANG=en_US.UTF-8")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &tmuxPTY{ptmx: ptmx, cmd: cmd, mgr: m, name: name}, nil
}
