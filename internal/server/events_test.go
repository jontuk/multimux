package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
