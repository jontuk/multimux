package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/gorilla/websocket"

	"github.com/jontuk/multimux/internal/auth"
	"github.com/jontuk/multimux/internal/store"
)

// checkWSOrigin defends against cross-site WebSocket hijacking: a browser can
// be tricked into opening a WS with the victim's cookie, so cookie-authenticated
// upgrades must come from our own origins. Token-authenticated upgrades carry
// the secret explicitly, so any origin is fine (that is how cross-daemon
// tiles connect).
func (s *Server) checkWSOrigin(r *http.Request) bool {
	if _, err := r.Cookie(auth.CookieName); err != nil {
		return true // token-authenticated
	}
	origin := r.Header.Get("Origin")
	return origin == "" || slices.Contains(s.cfg.Origins, origin)
}

func (s *Server) wsUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize: 4096, WriteBufferSize: 4096,
		CheckOrigin: s.checkWSOrigin,
	}
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (s *Server) handlePTY(w http.ResponseWriter, r *http.Request) {
	// CSWSH guard runs before we reveal anything about session existence.
	if !s.checkWSOrigin(r) {
		writeJSON(w, 403, map[string]string{"error": "forbidden origin"})
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad id"})
		return
	}
	sess, err := s.cfg.Store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if sess.Status != "running" {
		writeJSON(w, 409, map[string]string{"error": "session is dead"})
		return
	}
	up := s.wsUpgrader()
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade wrote the response (403 on origin failure)
	}
	defer conn.Close()

	arb := s.cfg.Arbiter.Register(sess.TmuxName)
	defer arb.Unregister()

	ptyConn, err := s.cfg.Tmux.Attach(sess.TmuxName)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"exit"}`))
		return
	}
	defer ptyConn.Close()

	// PTY → WS.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 16384)
		for {
			n, err := ptyConn.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"exit"}`))
				return
			}
		}
	}()

	// WS → PTY. Note the label: a bare `break` inside the switch would only
	// exit the switch, not the read loop.
readLoop:
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			// Keyboard input claims window-size ownership; if ownership just
			// transferred, restore this connection's window size (its earlier
			// resize may have been denied).
			if cols, rows, reapply := arb.ClaimInput(); reapply {
				if err := ptyConn.Resize(cols, rows, true); err != nil {
					slog.Debug("reapply resize", "err", err)
				}
			}
			if _, err := ptyConn.Write(data); err != nil {
				break readLoop
			}
		case websocket.TextMessage:
			var msg resizeMsg
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
				allowed := arb.Resize(msg.Cols, msg.Rows)
				if err := ptyConn.Resize(msg.Cols, msg.Rows, allowed); err != nil {
					slog.Debug("resize", "err", err)
				}
			}
		}
	}
	ptyConn.Close()
	<-done
}
