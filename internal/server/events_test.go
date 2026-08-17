package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jontuk/multimux/internal/store"
)

func TestHubBroadcast(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)
	h.Broadcast("session_created", map[string]int{"id": 1})
	select {
	case raw := <-ch:
		var ev struct {
			Type    string         `json:"type"`
			Payload map[string]int `json:"payload"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil || ev.Type != "session_created" || ev.Payload["id"] != 1 {
			t.Fatalf("event = %s, err %v", raw, err)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
}

func TestHubSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe() // never drained
	defer h.Unsubscribe(ch)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ { // > channel buffer
			h.Broadcast("layout_changed", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked on slow subscriber")
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	h.Unsubscribe(ch)
	h.Unsubscribe(ch) // must not panic
	h.Broadcast("x", nil)
}

// The hello frame carries the embedded frontend's build ID so an open tab can
// tell, on reconnect, that the daemon now serves different assets than the ones
// it is running.
func TestEventsHelloCarriesBuildID(t *testing.T) {
	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	build := ""
	for i := 0; i < 2; i++ { // stable across connections
		var hello struct {
			Type  string `json:"type"`
			Build string `json:"build"`
		}
		if err := json.Unmarshal(dialEventsHello(t, ts, token), &hello); err != nil {
			t.Fatal(err)
		}
		if hello.Type != "hello" {
			t.Fatalf("first frame type = %q, want hello", hello.Type)
		}
		if hello.Build == "" {
			t.Fatal("hello carried no build id")
		}
		if i == 1 && hello.Build != build {
			t.Fatalf("build id changed between connections: %q then %q", build, hello.Build)
		}
		build = hello.Build
	}
}

func TestBuildIDTracksIndexHTML(t *testing.T) {
	s, _, _ := newTestServer(t, true)
	other, _, _ := newTestServer(t, true)
	other.cfg.WebFS = fstest.MapFS{
		"index.html":    {Data: []byte("<html>multimux rebuilt</html>")},
		"assets/app.js": {Data: []byte("//js")},
	}
	if s.buildID() == other.buildID() {
		t.Fatalf("different index.html gave the same build id %q", s.buildID())
	}
}

// A bare checkout embeds web/dist/.gitkeep and nothing else. With no
// index.html there is no build to identify, so the field is omitted and the
// client never prompts for a reload it cannot justify.
func TestBuildIDEmptyWithoutIndexHTML(t *testing.T) {
	s, _, am := newTestServer(t, true)
	s.cfg.WebFS = fstest.MapFS{".gitkeep": {Data: []byte("")}}
	if s.buildID() != "" {
		t.Fatalf("build id without index.html = %q, want empty", s.buildID())
	}

	token, _ := am.CreateSession("UA")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	var hello map[string]any
	if err := json.Unmarshal(dialEventsHello(t, ts, token), &hello); err != nil {
		t.Fatal(err)
	}
	if _, ok := hello["build"]; ok {
		t.Fatalf("hello carried a build key without index.html: %v", hello)
	}
}

// dialEventsHello opens /ws/events and returns its first frame.
func dialEventsHello(t *testing.T, ts *httptest.Server, token string) []byte {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/events?token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("dial events: %v (%d)", err, code)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	return raw
}

func TestExpiredAuthSessionSweepLogsOnlyWhenRowsChange(t *testing.T) {
	s, st, _ := newTestServer(t, true)
	now := time.Now().UTC()
	for _, session := range []store.AuthSession{
		{
			TokenHash: "expired-hash-must-not-leak",
			UserAgent: "expired-agent-must-not-leak",
			CreatedAt: now.Add(-2 * time.Hour),
			ExpiresAt: now.Add(-time.Hour),
		},
		{
			TokenHash: "active-hash-must-not-leak",
			UserAgent: "active-agent-must-not-leak",
			CreatedAt: now,
			ExpiresAt: now.Add(time.Hour),
		},
	} {
		if err := st.CreateAuthSession(session); err != nil {
			t.Fatal(err)
		}
	}

	buf := captureLogs(t)
	s.sweepExpiredAuthSessions(now)
	s.sweepExpiredAuthSessions(now)

	logged := buf.String()
	if strings.Count(logged, `"msg":"auth sessions expired"`) != 1 ||
		!strings.Contains(logged, `"count":1`) {
		t.Fatalf("expired session sweep log = %s", logged)
	}
	for _, secret := range []string{"expired-hash", "active-hash", "expired-agent", "active-agent"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("session sweep log exposed %q: %s", secret, logged)
		}
	}
}

// A browser cannot see WS ping frames, so an idle events socket gives the page
// no way to tell a live connection from one a sleeping phone's network killed
// without a close — the state where the grid quietly stops resyncing. The
// keepalive event is that signal, and it must be plain enough that a client
// which does not know the type can ignore it.
func TestEventsSendsKeepalive(t *testing.T) {
	old := keepaliveInterval
	keepaliveInterval = 10 * time.Millisecond
	t.Cleanup(func() { keepaliveInterval = old })

	s, _, am := newTestServer(t, true)
	token, _ := am.CreateSession("UA")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/events?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial events: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil { // hello
		t.Fatalf("read hello: %v", err)
	}

	var ev struct {
		Type string `json:"type"`
	}
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read keepalive: %v", err)
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("keepalive frame %s: %v", raw, err)
		}
		if ev.Type == "keepalive" {
			return
		}
	}
}
