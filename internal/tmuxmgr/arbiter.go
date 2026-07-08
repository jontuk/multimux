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
	refs  int
	owner *ArbConn
}

// ArbConn is one connection's handle on the arbiter.
type ArbConn struct {
	arb        *Arbiter
	tmuxName   string
	cols, rows uint16 // last dims this conn asked for (guarded by arb.mu)
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
	return &ArbConn{arb: a, tmuxName: tmuxName}
}

// Unregister drops the connection, releasing ownership if held.
func (c *ArbConn) Unregister() {
	c.arb.mu.Lock()
	defer c.arb.mu.Unlock()
	s := c.arb.sessions[c.tmuxName]
	if s == nil {
		return
	}
	if s.owner == c {
		s.owner = nil
	}
	s.refs--
	if s.refs <= 0 {
		delete(c.arb.sessions, c.tmuxName)
	}
}

// Resize records the dims this conn wants and reports whether it may resize
// the shared tmux window (owner, or no owner set).
func (c *ArbConn) Resize(cols, rows uint16) bool {
	c.arb.mu.Lock()
	defer c.arb.mu.Unlock()
	c.cols, c.rows = cols, rows
	s := c.arb.sessions[c.tmuxName]
	if s == nil {
		return true
	}
	return s.owner == nil || s.owner == c
}

// ClaimInput marks this conn as owner (call on keyboard input). If ownership
// changed hands and the conn has known dims they are returned with
// reapply=true so the caller restores this conn's window size — its earlier
// resize may have been denied while another conn owned the window.
func (c *ArbConn) ClaimInput() (cols, rows uint16, reapply bool) {
	c.arb.mu.Lock()
	defer c.arb.mu.Unlock()
	s := c.arb.sessions[c.tmuxName]
	if s == nil || s.owner == c {
		return 0, 0, false
	}
	s.owner = c
	if c.cols == 0 || c.rows == 0 {
		return 0, 0, false
	}
	return c.cols, c.rows, true
}
