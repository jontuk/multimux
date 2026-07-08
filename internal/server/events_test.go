package server

import (
	"encoding/json"
	"testing"
	"time"
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
