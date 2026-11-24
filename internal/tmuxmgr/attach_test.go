package tmuxmgr

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty/v2"
)

// countSetsize counts the ioctls Resize issues, still performing each one.
func countSetsize(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := setsize
	setsize = func(f *os.File, ws *pty.Winsize) error {
		calls++
		return orig(f, ws)
	}
	t.Cleanup(func() { setsize = orig })
	return &calls
}

func openPTY(t *testing.T) *os.File {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ptmx.Close()
		tty.Close()
	})
	return ptmx
}

// A follow-input tile repeats its size with every keystroke's claim; only a
// size the PTY does not already have is worth an ioctl.
func TestResizeSkipsSetsizeForUnchangedDims(t *testing.T) {
	calls := countSetsize(t)
	conn := &tmuxPTY{ptmx: openPTY(t)}

	for range 3 {
		if err := conn.Resize(120, 30, false); err != nil {
			t.Fatal(err)
		}
	}
	if *calls != 1 {
		t.Fatalf("setsize calls for one repeated size = %d, want 1", *calls)
	}

	if err := conn.Resize(121, 30, false); err != nil {
		t.Fatal(err)
	}
	if err := conn.Resize(121, 31, false); err != nil {
		t.Fatal(err)
	}
	if *calls != 3 {
		t.Fatalf("setsize calls after two size changes = %d, want 3", *calls)
	}
	rows, cols, err := pty.Getsize(conn.ptmx)
	if err != nil {
		t.Fatal(err)
	}
	if cols != 121 || rows != 31 {
		t.Fatalf("pty size = %dx%d, want 121x31", cols, rows)
	}

	// Going back to an earlier size is a change too.
	if err := conn.Resize(120, 30, false); err != nil {
		t.Fatal(err)
	}
	if *calls != 4 {
		t.Fatalf("setsize calls after returning to the first size = %d, want 4", *calls)
	}
}

// A failed ioctl must not be remembered as the PTY's size, or the retry that
// would fix it is skipped.
func TestResizeRetriesAfterSetsizeFailure(t *testing.T) {
	orig := setsize
	t.Cleanup(func() { setsize = orig })
	calls, fail := 0, true
	setsize = func(*os.File, *pty.Winsize) error {
		calls++
		if fail {
			return os.ErrClosed
		}
		return nil
	}
	conn := &tmuxPTY{}

	if err := conn.Resize(120, 30, false); err == nil {
		t.Fatal("Resize must report the setsize failure")
	}
	fail = false
	if err := conn.Resize(120, 30, false); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("setsize calls = %d, want the failed size retried", calls)
	}
}

// The shared window is not this connection's alone, so its size is not cached:
// something else may have resized it, and the owner repeating its dims is what
// puts it back.
func TestResizeWindowIsNotSkippedForUnchangedDims(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(5)
	if err := m.CreateSession(name, t.TempDir(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	calls := countSetsize(t)
	conn := &tmuxPTY{ptmx: openPTY(t), mgr: m, name: name}
	windowSize := func() string {
		t.Helper()
		out, err := exec.Command("tmux", m.baseArgs("display-message", "-p", "-t", ExactTarget(name),
			"#{window_width}x#{window_height}")...).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}

	if err := conn.Resize(120, 30, true); err != nil {
		t.Fatal(err)
	}
	if got := windowSize(); got != "120x30" {
		t.Fatalf("window after first resize = %s, want 120x30", got)
	}
	// Behind the daemon's back.
	if err := m.run("resize-window", "-t", ExactTarget(name), "-x", "90", "-y", "20"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Resize(120, 30, true); err != nil {
		t.Fatal(err)
	}
	if got := windowSize(); got != "120x30" {
		t.Fatalf("window after repeating the same dims = %s, want it put back to 120x30", got)
	}
	if *calls != 1 {
		t.Fatalf("setsize calls = %d, want 1", *calls)
	}
}

func TestAttachReadsSessionOutput(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(3)
	if err := m.CreateSession(name, t.TempDir(), "echo MULTIMUX_MARKER; sleep 60"); err != nil {
		t.Fatal(err)
	}
	conn, err := m.Attach(name)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Resize(120, 30, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var got strings.Builder
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		n, err := conn.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
			if strings.Contains(got.String(), "MULTIMUX_MARKER") {
				return
			}
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("marker not seen in attach output: %q", got.String())
}

func TestAttachForwardsExtendedKeyToUnawareApplication(t *testing.T) {
	m := testManager(t)
	name := m.SessionName(4)
	dir := t.TempDir()
	readyPath := dir + "/ready"
	inputPath := dir + "/input"
	command := fmt.Sprintf("stty raw -echo; : > %q; dd bs=1 count=7 of=%q 2>/dev/null", readyPath, inputPath)
	if err := m.CreateSession(name, dir, command); err != nil {
		t.Fatal(err)
	}
	// Simulate a tmux server left running across an upgrade from the old
	// multimux setting. Daemon start must repair existing servers too.
	if err := m.run("set-option", "-s", "extended-keys", "on"); err != nil {
		t.Fatal(err)
	}
	// tmux's own default, and what a server started before multimux pinned the
	// format is left holding. Set it explicitly so the test does not pass just
	// because the developer's tmux.conf already chose csi-u. tmux only grew the
	// option in 3.5 -- older builds emit CSI u unconditionally, so there is
	// nothing to pin there and nothing to assert.
	hasFormat := m.run("set-option", "-s", "extended-keys-format", "xterm") == nil
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pane application did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	m.ConfigureServer()
	conn, err := m.Attach(name)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	option, err := exec.Command("tmux", m.baseArgs("show-options", "-s", "-v", "extended-keys")...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(option)); got != "always" {
		t.Fatalf("extended-keys after ConfigureServer = %q, want always", got)
	}
	if hasFormat {
		format, err := exec.Command("tmux", m.baseArgs("show-options", "-s", "-v", "extended-keys-format")...).Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(format)); got != "csi-u" {
			t.Fatalf("extended-keys-format after ConfigureServer = %q, want csi-u", got)
		}
	}
	const shiftEnter = "\x1b[13;2u"
	if _, err := conn.Write([]byte(shiftEnter + "abcdefg")); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := os.ReadFile(inputPath)
		if err == nil && len(got) == len(shiftEnter) {
			if string(got) != shiftEnter {
				t.Fatalf("pane received %q, want Shift+Enter %q", got, shiftEnter)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := os.ReadFile(inputPath)
	t.Fatalf("pane input = %q, %v; want Shift+Enter %q", got, err, shiftEnter)
}
