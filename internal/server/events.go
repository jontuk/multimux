package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// keepaliveInterval paces both the events socket's ping and its keepalive
// event. A var only so tests need not wait out a real interval; production
// never changes it.
var keepaliveInterval = pingInterval

// eventsPongWait is the events socket's share of pongWait. A var for the same
// reason keepaliveInterval is: tests cannot wait out 75 seconds.
var eventsPongWait = pongWait

// The heartbeat clients can actually see. Pre-marshalled: it is identical for
// every connection and every tick.
var keepaliveFrame = []byte(`{"type":"keepalive"}`)

// Hub fans session/layout events out to every connected events WebSocket so
// multiple open tabs stay consistent.
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan []byte]struct{})}
}

func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
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
	h.mu.Lock()
	for ch := range h.subs {
		select {
		case ch <- raw:
		default:
		}
	}
	h.mu.Unlock()
}

// writeText sends one text frame under writeWait, so a peer that stops reading
// cannot wedge this handler in a write that never returns.
func writeText(conn *websocket.Conn, raw []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, raw)
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
	// The hello carries the frontend build ID so a tab that reconnects after a
	// daemon restart can tell it is running assets the daemon no longer serves.
	hello := map[string]string{"type": "hello"}
	if build := s.buildID(); build != "" {
		hello["build"] = build
	}
	if raw, err := json.Marshal(hello); err == nil {
		writeText(conn, raw)
	}

	// A peer that vanishes without a close leaves a socket TCP will not fail
	// for hours, and this handler holds a goroutine, a ticker, a hub
	// subscription and a descriptor the whole time. Same contract as the PTY
	// socket: ping, and require the browser's automatic pong inside pongWait.
	conn.SetReadDeadline(time.Now().Add(eventsPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(eventsPongWait))
	})

	// Reader goroutine detects client close — and, via the deadline above, a
	// client that stopped answering.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			// Traffic proves liveness as well as a pong does, and gorilla only
			// extends the deadline from the pong handler.
			conn.SetReadDeadline(time.Now().Add(eventsPongWait))
		}
	}()
	ping := time.NewTicker(keepaliveInterval)
	defer ping.Stop()
	for {
		select {
		case raw := <-ch:
			if err := writeText(conn, raw); err != nil {
				return
			}
		case <-ping.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
			// The ping keeps proxies and NAT from timing the socket out, but a
			// browser cannot observe ping frames at all: to a page, an idle
			// socket and one whose network died without a close look identical,
			// and the page then stops resyncing while believing it is
			// connected. The keepalive is the same heartbeat made visible to
			// JS. Clients that don't know the type ignore it, as they do any
			// other unknown event.
			if err := writeText(conn, keepaliveFrame); err != nil {
				return
			}
		case <-closed:
			return
		}
	}
}

func (s *Server) sweepExpiredAuthSessions(now time.Time) {
	if n, err := s.cfg.Store.DeleteExpiredAuthSessions(now); err != nil {
		slog.Error("session sweep", "err", err)
	} else if n > 0 {
		slog.Info("auth sessions expired", "count", n)
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
			if err := s.CheckGitInfo(); err != nil {
				slog.Error("git check", "err", err)
			}
		}
	}()
	go func() {
		for range time.Tick(time.Hour) {
			s.sweepExpiredAuthSessions(time.Now())
		}
	}()
}
