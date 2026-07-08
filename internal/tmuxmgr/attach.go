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
}

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
	if err := pty.Setsize(t.ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return err
	}
	if !resizeWindow {
		return nil
	}
	return t.mgr.run("resize-window", "-t", ExactTarget(t.name),
		"-x", strconv.Itoa(int(cols)), "-y", strconv.Itoa(int(rows)))
}

// Attach spawns `tmux attach-session` on a fresh PTY.
func (m *Manager) Attach(name string) (PTYConn, error) {
	// Re-assert manual sizing for pre-existing sessions too, so a stale
	// client from another machine cannot shrink the window under us.
	_ = m.run("set-option", "-t", ExactTarget(name), "window-size", "manual")

	cmd := exec.Command("tmux", m.baseArgs("attach-session", "-t", ExactTarget(name))...)
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color", "LANG=en_US.UTF-8")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &tmuxPTY{ptmx: ptmx, cmd: cmd, mgr: m, name: name}, nil
}
