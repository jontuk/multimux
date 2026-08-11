package tmuxmgr

import (
	"sync"
	"time"
)

// ownerGrace is how long a session keeps its owner after that client's last
// connection drops. Reconnects are routine — a network blip, a tile remount, a
// phone waking up — and ownership must survive them, or the next passive resize
// from any other client would reclaim the window and resize it under the person
// actually typing. Once the owner has stayed gone this long it is presumed
// closed, and the next client to resize takes over.
const ownerGrace = 30 * time.Second

// Arbiter decides which connection may change the shared tmux window size for
// a session. Ownership follows keyboard input: the client that most recently
// wrote input to the PTY owns the size. Non-owner resizes are recorded (and
// still size that client's own attach PTY) but do not touch the window. On
// ownership transfer the new owner's last-known dims are reapplied so switching
// machines and typing reclaims the window at that machine's size.
//
// Ownership is keyed on the *client* id (one browser profile), not on the
// connection, so it survives reconnects — see ownerGrace.
type Arbiter struct {
	mu       sync.Mutex
	sessions map[string]*arbSession
	now      func() time.Time // overridden in tests
}

type arbSession struct {
	resizeMu sync.Mutex // serializes ownership changes with their tmux resize
	refs     int
	live     map[string]int // client id → live connections
	// ownerID is "" until some client claims the window; it then outlives that
	// client's connections, lapsing only ownerGrace after its last one drops.
	ownerID     string
	ownerLeftAt time.Time
}

// ArbConn is one connection's handle on the arbiter.
type ArbConn struct {
	arb          *Arbiter
	tmuxName     string
	clientID     string
	session      *arbSession
	cols, rows   uint16 // last dims this conn asked for (guarded by arb.mu)
	unregistered bool   // guarded by arb.mu; true once Unregister has run
}

func NewArbiter() *Arbiter {
	return &Arbiter{sessions: make(map[string]*arbSession), now: time.Now}
}

// Register adds a connection for tmuxName on behalf of clientID (a stable
// per-browser id); pair with Unregister.
func (a *Arbiter) Register(tmuxName, clientID string) *ArbConn {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[tmuxName]
	if s == nil {
		s = &arbSession{live: make(map[string]int)}
		a.sessions[tmuxName] = s
	}
	s.refs++
	s.live[clientID]++
	return &ArbConn{arb: a, tmuxName: tmuxName, clientID: clientID, session: s}
}

// Unregister drops the connection. Ownership is not released with it — it is
// held for ownerGrace so the same client can reconnect into it. Safe to call
// more than once; only the first call has any effect.
func (c *ArbConn) Unregister() {
	c.session.resizeMu.Lock()
	defer c.session.resizeMu.Unlock()
	c.arb.mu.Lock()
	defer c.arb.mu.Unlock()
	if c.unregistered {
		return
	}
	c.unregistered = true
	if c.arb.sessions[c.tmuxName] != c.session {
		return
	}
	c.session.live[c.clientID]--
	if c.session.live[c.clientID] <= 0 {
		delete(c.session.live, c.clientID)
		if c.session.ownerID == c.clientID {
			c.session.ownerLeftAt = c.arb.now()
		}
	}
	c.session.refs--
	if c.session.refs <= 0 {
		delete(c.arb.sessions, c.tmuxName)
	}
}

// mayResize reports whether this conn's passive resize may touch the window.
// Callers hold arb.mu.
func (c *ArbConn) mayResize() bool {
	s := c.session
	if s.ownerID == "" || s.ownerID == c.clientID {
		return true
	}
	// The owning client has no live connection and has not come back within
	// the grace window — treat it as gone and let this client take over.
	return s.live[s.ownerID] == 0 && c.arb.now().Sub(s.ownerLeftAt) >= ownerGrace
}

// Resize records the dims this conn wants and applies the resize while the
// session's resize sequence is locked. An active resize claims ownership.
func (c *ArbConn) Resize(cols, rows uint16, active bool, apply func(resizeWindow bool) error) error {
	c.session.resizeMu.Lock()
	defer c.session.resizeMu.Unlock()

	c.arb.mu.Lock()
	if c.unregistered || c.arb.sessions[c.tmuxName] != c.session {
		c.arb.mu.Unlock()
		return nil
	}
	c.cols, c.rows = cols, rows
	allowed := c.mayResize()
	if active || allowed {
		// Whoever last sized the window owns it; taking over a lapsed
		// ownership must also stop the old owner from silently getting it back.
		c.session.ownerID = c.clientID
		allowed = true
	}
	c.arb.mu.Unlock()

	return apply(allowed)
}

// ClaimInput marks this conn's client as owner (call on keyboard input). If
// ownership changed hands and the conn has known dims, it reapplies them while
// the ownership transfer is locked.
func (c *ArbConn) ClaimInput(apply func(cols, rows uint16) error) error {
	c.session.resizeMu.Lock()
	defer c.session.resizeMu.Unlock()

	c.arb.mu.Lock()
	if c.unregistered || c.arb.sessions[c.tmuxName] != c.session || c.session.ownerID == c.clientID {
		c.arb.mu.Unlock()
		return nil
	}
	c.session.ownerID = c.clientID
	if c.cols == 0 || c.rows == 0 {
		c.arb.mu.Unlock()
		return nil
	}
	cols, rows := c.cols, c.rows
	c.arb.mu.Unlock()

	return apply(cols, rows)
}
