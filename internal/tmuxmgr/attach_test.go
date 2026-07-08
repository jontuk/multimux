package tmuxmgr

import (
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
