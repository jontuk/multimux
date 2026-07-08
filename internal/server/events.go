package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Hub fans session/layout events out to every connected events WebSocket so
// multiple open tabs stay consistent.
type Hub struct {
	mu   chan struct{} // 1-buffered channel as mutex (keeps select simple)
	subs map[chan []byte]struct{}
}

func NewHub() *Hub {
	h := &Hub{mu: make(chan struct{}, 1), subs: make(map[chan []byte]struct{})}
	h.mu <- struct{}{}
	return h
}

func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 16)
	<-h.mu
	h.subs[ch] = struct{}{}
	h.mu <- struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(ch chan []byte) {
	<-h.mu
	delete(h.subs, ch)
	h.mu <- struct{}{}
}

// Broadcast sends {"type":...,"payload":...} to all subscribers. Sends are
// non-blocking: a subscriber that stops draining loses events instead of
// wedging the daemon.
func (h *Hub) Broadcast(eventType string, payload any) {
	raw, err := json.Marshal(map[string]any{"type": eventType, "payload": payload})
	if err != nil {
		slog.Error("hub marshal", "err", err)
		return
	}
	<-h.mu
	for ch := range h.subs {
		select {
		case ch <- raw:
		default:
		}
	}
	h.mu <- struct{}{}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	up := s.wsUpgrader()
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)
	conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))

	// Reader goroutine detects client close.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case raw := <-ch:
			if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
				return
			}
		case <-ping.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-closed:
			return
		}
	}
}

// StartBackground runs startup reconcile plus periodic maintenance.
func (s *Server) StartBackground() {
	if _, err := s.Reconcile(); err != nil {
		slog.Error("startup reconcile", "err", err)
	}
	go func() {
		for range time.Tick(5 * time.Second) {
			if _, err := s.Reconcile(); err != nil {
				slog.Error("reconcile", "err", err)
			}
		}
	}()
	go func() {
		for range time.Tick(time.Hour) {
			if _, err := s.cfg.Store.DeleteExpiredAuthSessions(time.Now()); err != nil {
				slog.Error("session sweep", "err", err)
			}
		}
	}()
}
