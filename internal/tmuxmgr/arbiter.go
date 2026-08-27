package tmuxmgr

import (
	"log/slog"
	"sync"
	"time"
)

// ownerGrace is how long a session keeps its owner after that client's last
// connection drops, before another client may take the window with an ordinary
// resize. Reconnects are routine — a network blip, a tile remount, a phone
// waking up — and ownership must survive them, or the next permitted resize
// from another follow-input client would reclaim the window and resize it under
// the person actually typing.
const ownerGrace = 30 * time.Second

// presenceWindow is how long a client counts as having a human at it after
// keyboard input or an explicit active resize. Inheriting an absent owner's
// window needs one of those: absence alone cannot tell a machine someone has
// just sat down at from a laptop that wakes for half a minute every hour on
// macOS dark wake and reconnects its tiles with nobody there. Without the
// check, that wake takes the window and shrinks every tile to the dormant
// machine's dims — repeatedly, for as long as the laptop is left on.
const presenceWindow = 5 * time.Minute

// Arbiter decides which connection may change the shared tmux window size for
// a session. Follow-input connections keep the existing behavior: ownership
// follows keyboard input, and the client that most recently wrote input to the
// PTY owns the size. Passive connections only resize their own attach PTY during
// ordinary resize and input activity. An active resize is an explicit ownership
// claim for either policy. On a follow-input ownership transfer the new owner's
// last-known dims are reapplied so switching machines and typing reclaims the
// window at that machine's size.
//
// Ownership is keyed on the *client* id (one browser profile), not on the
// connection, so it survives reconnects — see ownerGrace — and outlives the
// session's last connection entirely, so an emptied session is not up for
// grabs. Only Prune forgets it, when tmux no longer has the session.
type Arbiter struct {
	mu       sync.Mutex
	sessions map[string]*arbSession
	// lastHuman records when a human was last at each client (keyboard input,
	// or an explicit active resize). Swept by Prune.
	lastHuman map[string]time.Time
	now       func() time.Time // overridden in tests
}

type arbSession struct {
	resizeMu sync.Mutex // serializes ownership changes with their tmux resize
	refs     int
	live     map[string]int // client id → live connections
	// ownerID is "" until some client claims the window; it then outlives that
	// client's connections, and the session's, until Prune drops the record.
	ownerID     string
	ownerLeftAt time.Time
}

// SizePolicy controls whether ordinary resize and input activity may claim the
// shared tmux window for a connection.
type SizePolicy uint8

const (
	SizePolicyFollowInput SizePolicy = iota
	SizePolicyPassive
)

// ArbConn is one connection's handle on the arbiter.
type ArbConn struct {
	arb          *Arbiter
	tmuxName     string
	clientID     string
	sizePolicy   SizePolicy
	session      *arbSession
	cols, rows   uint16 // last dims this conn asked for (guarded by arb.mu)
	unregistered bool   // guarded by arb.mu; true once Unregister has run
}

func NewArbiter() *Arbiter {
	return &Arbiter{
		sessions:  make(map[string]*arbSession),
		lastHuman: make(map[string]time.Time),
		now:       time.Now,
	}
}

// Register adds a connection for tmuxName on behalf of clientID (a stable
// per-browser id) using sizePolicy; pair with Unregister.
func (a *Arbiter) Register(tmuxName, clientID string, sizePolicy SizePolicy) *ArbConn {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[tmuxName]
	if s == nil {
		s = &arbSession{live: make(map[string]int)}
		a.sessions[tmuxName] = s
	}
	s.refs++
	s.live[clientID]++
	return &ArbConn{
		arb:        a,
		tmuxName:   tmuxName,
		clientID:   clientID,
		sizePolicy: sizePolicy,
		session:    s,
	}
}

// Unregister drops the connection. Ownership is not released with it, not even
// when it was the session's last one: who owns the window has to outlive an
// empty session, or every gap in the grid — a closed tile, a reconnect, a dir
// filter hiding the tile — would leave the size to whichever client resizes
// next, including one nobody is sitting at. Safe to call more than once; only
// the first call has any effect.
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
}

// Prune forgets the sessions tmux no longer has, which is the only thing that
// ends an ownership record, and drops client presence too old to matter. A
// record with live connections is kept whatever the caller passes: those
// connections are still arbitrating through it. Called from the reconcile tick,
// whose listing already aborts rather than act on a transient tmux failure.
func (a *Arbiter) Prune(alive map[string]bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for name, s := range a.sessions {
		if s.refs <= 0 && !alive[name] {
			delete(a.sessions, name)
		}
	}
	cutoff := a.now().Add(-presenceWindow)
	for id, at := range a.lastHuman {
		if at.Before(cutoff) {
			delete(a.lastHuman, id)
		}
	}
}

// present reports whether a human was at clientID recently enough to count.
// Callers hold arb.mu.
func (a *Arbiter) present(clientID string) bool {
	at, ok := a.lastHuman[clientID]
	return ok && a.now().Sub(at) <= presenceWindow
}

// mayResize reports whether this follow-input conn's ordinary resize may touch
// the window. Callers hold arb.mu.
func (c *ArbConn) mayResize() bool {
	s := c.session
	if s.ownerID == "" || s.ownerID == c.clientID {
		return true
	}
	if s.live[s.ownerID] > 0 {
		return false // the owner is attached; only a human elsewhere takes it away
	}
	if c.arb.now().Sub(s.ownerLeftAt) < ownerGrace {
		return false
	}
	// The owning client has stayed gone, so the window is inheritable — but
	// only by a machine someone is actually at. A tab reconnecting on a wake
	// with nobody there leaves the window where the last person left it.
	return c.arb.present(c.clientID)
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
	if active {
		c.arb.lastHuman[c.clientID] = c.arb.now() // an active resize is a gesture
	}
	allowed := c.sizePolicy == SizePolicyFollowInput && c.mayResize()
	prev := c.session.ownerID
	claim := ""
	if active || allowed {
		// Whoever last sized the window owns it; taking over a lapsed
		// ownership must also stop the old owner from silently getting it back.
		c.session.ownerID = c.clientID
		allowed = true
		switch {
		case prev == c.clientID:
		case active:
			claim = "active resize"
		case prev == "":
			claim = "unowned session"
		default:
			claim = "owner gone"
		}
	}
	c.arb.mu.Unlock()

	// Window size changing hands is the thing to have on record when a session
	// turns up sized for a machine nobody was using.
	switch {
	case claim != "":
		slog.Info("tmux window claimed", "tmux", c.tmuxName, "client", c.clientID,
			"prev_owner", prev, "why", claim, "cols", cols, "rows", rows)
	case !allowed && c.sizePolicy == SizePolicyFollowInput:
		slog.Debug("tmux window resize denied", "tmux", c.tmuxName, "client", c.clientID,
			"owner", prev, "cols", cols, "rows", rows)
	}
	return apply(allowed)
}

// ClaimInput marks this conn's client as owner (call on keyboard input). If
// ownership changed hands and the conn has known dims, it reapplies them while
// the ownership transfer is locked.
func (c *ArbConn) ClaimInput(apply func(cols, rows uint16) error) error {
	c.session.resizeMu.Lock()
	defer c.session.resizeMu.Unlock()

	c.arb.mu.Lock()
	if c.unregistered || c.arb.sessions[c.tmuxName] != c.session {
		c.arb.mu.Unlock()
		return nil
	}
	// Every keystroke is a human at this browser, whatever the size policy: a
	// phone may not size the shared window, but the person holding it still
	// counts when that browser's other tiles ask.
	c.arb.lastHuman[c.clientID] = c.arb.now()
	if c.sizePolicy == SizePolicyPassive || c.session.ownerID == c.clientID {
		c.arb.mu.Unlock()
		return nil
	}
	prev := c.session.ownerID
	c.session.ownerID = c.clientID
	cols, rows := c.cols, c.rows
	c.arb.mu.Unlock()

	slog.Info("tmux window claimed", "tmux", c.tmuxName, "client", c.clientID,
		"prev_owner", prev, "why", "input", "cols", cols, "rows", rows)
	if cols == 0 || rows == 0 {
		return nil
	}
	return apply(cols, rows)
}
