package tmuxmgr

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

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
	// multimux setting. Attach must repair existing sessions too.
	if err := m.run("set-option", "-s", "extended-keys", "on"); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("extended-keys after Attach = %q, want always", got)
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
