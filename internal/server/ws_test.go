package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dialPTY(t *testing.T, ts *httptest.Server, sessionID int64, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + fmt.Sprintf("/ws/pty/%d?token=%s", sessionID, token)
	conn, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{"https://evil.example"}})
	if err != nil {
		body := ""
		if resp != nil {
			body = fmt.Sprint(resp.StatusCode)
		}
		t.Fatalf("dial: %v (%s)", err, body)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestPTYBridgeEcho(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	tool, _ := st.CreateTool("sh", "sh")
	dir, _ := st.CreateDir("tmp", t.TempDir())
	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	var sess struct{ ID int64 }
	json.Unmarshal(w.Body.Bytes(), &sess)

	conn := dialPTY(t, ts, sess.ID, token) // token auth → any Origin OK
	resize, _ := json.Marshal(map[string]any{"type": "resize", "cols": 100, "rows": 30})
	conn.WriteMessage(websocket.TextMessage, resize)
	conn.WriteMessage(websocket.BinaryMessage, []byte("echo MMWS_MARKER\r"))

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var got strings.Builder
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("marker not seen; read err %v; got %q", err, got.String())
		}
		if mt == websocket.BinaryMessage {
			got.Write(data)
			if strings.Contains(got.String(), "MMWS_MARKER") {
				return
			}
		}
	}
}

// TestPTYClosesOnSessionExit proves that when the PTY hits EOF (the tmux
// session is killed under the attach), the server closes the websocket from
// its end rather than leaving a client that never closes its socket blocked
// forever. Without the fix, the handler goroutine, pty fd, and arbiter
// registration would leak because nothing ever unblocks the client's
// conn.ReadMessage() — this test's own ReadMessage would hang until its
// deadline and then report a deadline-exceeded error instead of a clean
// server-initiated close.
func TestPTYClosesOnSessionExit(t *testing.T) {
	s, st, token := newTmuxTestServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	tool, _ := st.CreateTool("sh", "sh")
	dir, _ := st.CreateDir("tmp", t.TempDir())
	w := do(t, s, "POST", "/api/sessions", token, fmt.Sprintf(`{"toolId":%d,"dirId":%d}`, tool.ID, dir.ID))
	var sess struct{ ID int64 }
	json.Unmarshal(w.Body.Bytes(), &sess)

	conn := dialPTY(t, ts, sess.ID, token)
	// Deliberately never call conn.Close() ourselves: we want to observe the
	// server closing its end, not us closing ours.

	// Kill the underlying tmux session out from under the live attach; this
	// makes `tmux attach-session`'s pty hit EOF, which is what the PTY→WS
	// goroutine must react to by closing the websocket.
	if w := do(t, s, "DELETE", fmt.Sprintf("/api/sessions/%d", sess.ID), token); w.Code != 204 {
		t.Fatalf("kill session = %d", w.Code)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	sawExit := false
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			if !sawExit {
				t.Fatalf("connection closed/errored before an exit frame was seen: %v", err)
			}
			if strings.Contains(err.Error(), "i/o timeout") {
				t.Fatalf("read timed out waiting for server to close the socket after exit; leaked handler? err: %v", err)
			}
			return // server closed its end after the exit frame — fix confirmed.
		}
		if mt == websocket.TextMessage && strings.Contains(string(data), `"type":"exit"`) {
			sawExit = true
		}
	}
}

func TestPTYRejectsUnknownSession(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/pty/999?token=" + token
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("dial to unknown session succeeded")
	}
	if resp == nil || resp.StatusCode != 404 {
		t.Fatalf("status = %v, want 404", resp)
	}
}

func TestPTYRejectsCookieAuthFromForeignOrigin(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/pty/1"
	hdr := http.Header{
		"Origin": []string{"https://evil.example"},
		"Cookie": []string{"mm_session=" + token},
	}
	_, resp, err := websocket.DefaultDialer.Dial(url, hdr)
	if err == nil {
		t.Fatal("cookie-auth WS from foreign origin succeeded (CSWSH)")
	}
	if resp == nil || resp.StatusCode != 403 {
		t.Fatalf("status = %v, want 403", resp)
	}
}
