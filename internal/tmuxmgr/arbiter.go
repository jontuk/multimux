package tmuxmgr

import "sync"

// Arbiter decides which connection may change the shared tmux window size for
// a session. Ownership follows keyboard input: the connection that most
// recently wrote input to the PTY owns the size. Non-owner resizes are
// recorded (and still size that client's own attach PTY) but do not touch the
// window. On ownership transfer the new owner's last-known dims are reapplied
// so switching machines and typing reclaims the window at that machine's size.
type Arbiter struct {
	mu       sync.Mutex
	sessions map[string]*arbSession
}

type arbSession struct {
	resizeMu sync.Mutex // serializes ownership changes with their tmux resize
	refs     int
	owner    *ArbConn
}

// ArbConn is one connection's handle on the arbiter.
type ArbConn struct {
	arb          *Arbiter
	tmuxName     string
	session      *arbSession
	cols, rows   uint16 // last dims this conn asked for (guarded by arb.mu)
	unregistered bool   // guarded by arb.mu; true once Unregister has run
}

func NewArbiter() *Arbiter {
	return &Arbiter{sessions: make(map[string]*arbSession)}
}

// Register adds a connection for tmuxName; pair with Unregister.
func (a *Arbiter) Register(tmuxName string) *ArbConn {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[tmuxName]
	if s == nil {
		s = &arbSession{}
		a.sessions[tmuxName] = s
	}
	s.refs++
	return &ArbConn{arb: a, tmuxName: tmuxName, session: s}
}

// Unregister drops the connection, releasing ownership if held. It is safe to
// call more than once; only the first call has any effect.
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
	if c.session.owner == c {
		c.session.owner = nil
	}
	c.session.refs--
	if c.session.refs <= 0 {
		delete(c.arb.sessions, c.tmuxName)
	}
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
	allowed := c.session.owner == nil || c.session.owner == c
	if active {
		c.session.owner = c
		allowed = true
	}
	c.arb.mu.Unlock()

	return apply(allowed)
}

// ClaimInput marks this conn as owner (call on keyboard input). If ownership
// changed hands and the conn has known dims, it reapplies them while the
// ownership transfer is locked.
func (c *ArbConn) ClaimInput(apply func(cols, rows uint16) error) error {
	c.session.resizeMu.Lock()
	defer c.session.resizeMu.Unlock()

	c.arb.mu.Lock()
	if c.unregistered || c.arb.sessions[c.tmuxName] != c.session || c.session.owner == c {
		c.arb.mu.Unlock()
		return nil
	}
	c.session.owner = c
	if c.cols == 0 || c.rows == 0 {
		c.arb.mu.Unlock()
		return nil
	}
	cols, rows := c.cols, c.rows
	c.arb.mu.Unlock()

	return apply(cols, rows)
}
