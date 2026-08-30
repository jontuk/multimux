package tmuxmgr

import (
	"testing"
	"time"
)

func resizeAllowed(t *testing.T, c *ArbConn, cols, rows uint16, active bool) bool {
	t.Helper()
	allowed := false
	if err := c.Resize(cols, rows, active, func(resizeWindow bool) error {
		allowed = resizeWindow
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return allowed
}

func claimInput(t *testing.T, c *ArbConn) (cols, rows uint16, reapplied bool) {
	t.Helper()
	if err := c.ClaimInput(func(c, r uint16) error {
		cols, rows, reapplied = c, r, true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return cols, rows, reapplied
}

func TestFirstConnMayResize(t *testing.T) {
	a := NewArbiter()
	c := a.Register("mm-1", "A", SizePolicyFollowInput)
	defer c.Unregister()
	if !resizeAllowed(t, c, 80, 24, false) {
		t.Fatal("sole connection should be allowed to resize")
	}
}

func TestOwnershipFollowsInput(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1", "A", SizePolicyFollowInput)
	c2 := a.Register("mm-1", "B", SizePolicyFollowInput)
	defer c1.Unregister()
	defer c2.Unregister()

	// c1 types → owns the window.
	resizeAllowed(t, c1, 80, 24, false)
	claimInput(t, c1)
	if resizeAllowed(t, c2, 120, 40, false) {
		t.Fatal("non-owner resize must be denied")
	}
	if !resizeAllowed(t, c1, 81, 25, false) {
		t.Fatal("owner resize must be allowed")
	}

	// c2 types → ownership transfers, its last dims come back for reapply.
	cols, rows, reapply := claimInput(t, c2)
	if !reapply || cols != 120 || rows != 40 {
		t.Fatalf("ClaimInput = %d,%d,%v; want 120,40,true", cols, rows, reapply)
	}
	if resizeAllowed(t, c1, 80, 24, false) {
		t.Fatal("old owner must lose resize rights")
	}
}

func TestClaimInputByOwnerIsNoop(t *testing.T) {
	a := NewArbiter()
	c := a.Register("mm-1", "A", SizePolicyFollowInput)
	defer c.Unregister()
	resizeAllowed(t, c, 80, 24, false)
	claimInput(t, c)
	if _, _, reapply := claimInput(t, c); reapply {
		t.Fatal("re-claim by current owner must not reapply")
	}
}

// Ownership is keyed on the client, not the socket: a tile that reconnects
// (network blip, remount, tab restore) must come back as the same owner, and a
// different machine must not be able to grab the window in the gap.
func TestOwnershipSurvivesOwnerReconnect(t *testing.T) {
	a := NewArbiter()
	owner := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	other := a.Register("mm-1", "phone", SizePolicyFollowInput)
	defer other.Unregister()

	resizeAllowed(t, owner, 200, 50, false)
	claimInput(t, owner)
	owner.Unregister() // desktop's socket drops

	if resizeAllowed(t, other, 60, 20, false) {
		t.Fatal("another client must not claim the window while the owner is merely reconnecting")
	}

	reconnected := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	defer reconnected.Unregister()
	if !resizeAllowed(t, reconnected, 200, 50, false) {
		t.Fatal("the owning client must still own the window after reconnecting")
	}
	if resizeAllowed(t, other, 60, 20, false) {
		t.Fatal("non-owner client must stay denied after the owner is back")
	}
}

// A client that is gone for good must not hold the window hostage.
func TestOwnershipLapsesAfterOwnerStaysGone(t *testing.T) {
	a := NewArbiter()
	now := time.Now()
	a.now = func() time.Time { return now }

	owner := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	other := a.Register("mm-1", "phone", SizePolicyFollowInput)
	defer other.Unregister()
	claimInput(t, owner)
	owner.Unregister()
	markPresent(t, a, "phone") // someone is at the other machine

	now = now.Add(ownerGrace - time.Second)
	if resizeAllowed(t, other, 60, 20, false) {
		t.Fatal("resize must stay denied inside the reconnect grace window")
	}

	now = now.Add(2 * time.Second)
	if !resizeAllowed(t, other, 60, 20, false) {
		t.Fatal("resize must be allowed once the owner has stayed gone past the grace window")
	}
	if _, _, reapply := claimInput(t, other); reapply {
		t.Fatal("the lapsed-grace resize should have transferred ownership already")
	}
}

// Two tabs of the same browser share a client id; whichever one is live owns.
func TestSecondConnOfOwningClientKeepsOwnership(t *testing.T) {
	a := NewArbiter()
	now := time.Now()
	a.now = func() time.Time { return now }

	first := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	second := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	defer second.Unregister()
	other := a.Register("mm-1", "phone", SizePolicyFollowInput)
	defer other.Unregister()

	claimInput(t, first)
	first.Unregister()

	now = now.Add(2 * ownerGrace)
	if resizeAllowed(t, other, 60, 20, false) {
		t.Fatal("grace must not start while the owning client still has a live connection")
	}
	if !resizeAllowed(t, second, 200, 50, false) {
		t.Fatal("the owning client's other connection must be allowed to resize")
	}
}

func TestUnregisterIdempotent(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1", "A", SizePolicyFollowInput)
	c2 := a.Register("mm-1", "B", SizePolicyFollowInput)
	defer c2.Unregister()

	c1.Unregister()
	c1.Unregister() // second call must be a no-op, not a second refcount decrement

	if !resizeAllowed(t, c2, 100, 30, false) {
		t.Fatal("remaining connection should still be sole owner and allowed to resize")
	}
	claimInput(t, c2)
	if !resizeAllowed(t, c2, 101, 31, false) {
		t.Fatal("double-Unregister of c1 must not have torn down arbitration state for c2")
	}
}

func TestSessionsIsolated(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1", "A", SizePolicyFollowInput)
	c2 := a.Register("mm-2", "B", SizePolicyFollowInput)
	defer c1.Unregister()
	defer c2.Unregister()
	claimInput(t, c1)
	if !resizeAllowed(t, c2, 90, 30, false) {
		t.Fatal("ownership of mm-1 must not affect mm-2")
	}
}

func TestActiveResizeClaimsAndTransfersOwnership(t *testing.T) {
	a := NewArbiter()
	foreground := a.Register("mm-1", "A", SizePolicyFollowInput)
	background := a.Register("mm-1", "B", SizePolicyFollowInput)
	defer foreground.Unregister()
	defer background.Unregister()

	if !resizeAllowed(t, foreground, 129, 76, true) {
		t.Fatal("active foreground resize must be allowed")
	}
	if resizeAllowed(t, background, 97, 76, false) {
		t.Fatal("inactive background resize must not override the active owner")
	}
	if !resizeAllowed(t, background, 97, 76, true) {
		t.Fatal("newly active connection must take ownership")
	}
	if resizeAllowed(t, foreground, 129, 76, false) {
		t.Fatal("previous owner must not override the newly active connection")
	}
}

func TestRepeatedActiveResizeByOwnerDoesNotReapplySharedWindow(t *testing.T) {
	a := NewArbiter()
	owner := a.Register("mm-1", "A", SizePolicyFollowInput)
	defer owner.Unregister()

	if !resizeAllowed(t, owner, 129, 76, true) {
		t.Fatal("first active resize must claim the shared window")
	}
	if resizeAllowed(t, owner, 129, 76, true) {
		t.Fatal("repeated active resize by the owner must not reapply the shared window")
	}
}

func TestPassiveConnectionDoesNotResizeSharedWindow(t *testing.T) {
	a := NewArbiter()
	phone := a.Register("mm-1", "phone", SizePolicyPassive)
	defer phone.Unregister()

	if resizeAllowed(t, phone, 60, 20, false) {
		t.Fatal("passive resize must only resize the connection PTY")
	}
	if _, _, reapplied := claimInput(t, phone); reapplied {
		t.Fatal("passive input must not reapply shared dimensions")
	}
}

func TestPassiveFitIsOneShotAndDesktopInputReclaims(t *testing.T) {
	a := NewArbiter()
	desktop := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	phone := a.Register("mm-1", "phone", SizePolicyPassive)
	defer desktop.Unregister()
	defer phone.Unregister()

	resizeAllowed(t, desktop, 160, 50, false)
	claimInput(t, desktop)
	if resizeAllowed(t, phone, 60, 20, false) {
		t.Fatal("ordinary phone resize must remain local")
	}
	if !resizeAllowed(t, phone, 60, 20, true) {
		t.Fatal("explicit phone fit must resize the shared window")
	}
	if resizeAllowed(t, phone, 61, 21, false) {
		t.Fatal("fit must not turn later phone resizes into shared resizes")
	}
	if _, _, reapplied := claimInput(t, phone); reapplied {
		t.Fatal("phone input after fit must remain passive")
	}

	cols, rows, reapplied := claimInput(t, desktop)
	if !reapplied || cols != 160 || rows != 50 {
		t.Fatalf("desktop reclaim = %d,%d,%v; want 160,50,true", cols, rows, reapplied)
	}
}

func TestResizeApplicationIsSerializedWithOwnership(t *testing.T) {
	a := NewArbiter()
	first := a.Register("mm-1", "A", SizePolicyFollowInput)
	second := a.Register("mm-1", "B", SizePolicyFollowInput)
	defer first.Unregister()
	defer second.Unregister()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	applied := make(chan string, 2)
	errs := make(chan error, 2)

	go func() {
		errs <- first.Resize(129, 76, true, func(bool) error {
			close(firstStarted)
			<-releaseFirst
			applied <- "first"
			return nil
		})
	}()
	<-firstStarted

	go func() {
		errs <- second.Resize(97, 76, true, func(bool) error {
			applied <- "second"
			return nil
		})
	}()

	select {
	case got := <-applied:
		t.Fatalf("second resize ran before first completed: %s", got)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFirst)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if got := <-applied; got != "first" {
		t.Fatalf("first applied resize = %q, want first", got)
	}
	if got := <-applied; got != "second" {
		t.Fatalf("last applied resize = %q, want focused second owner", got)
	}
}

func TestResizeApplicationDoesNotBlockOtherSessions(t *testing.T) {
	a := NewArbiter()
	first := a.Register("mm-1", "A", SizePolicyFollowInput)
	second := a.Register("mm-2", "B", SizePolicyFollowInput)
	defer first.Unregister()
	defer second.Unregister()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.Resize(129, 76, true, func(bool) error {
			close(firstStarted)
			<-releaseFirst
			return nil
		})
	}()
	<-firstStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.Resize(97, 76, true, func(bool) error {
			return nil
		})
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(50 * time.Millisecond):
		close(releaseFirst)
		<-firstDone
		t.Fatal("resize in mm-1 blocked independent session mm-2")
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

// markPresent stamps clientID as having a human at it the way a real keystroke
// does — input on some session that client is connected to.
func markPresent(t *testing.T, a *Arbiter, clientID string) {
	t.Helper()
	c := a.Register("mm-presence", clientID, SizePolicyFollowInput)
	claimInput(t, c)
	c.Unregister()
}

// A grid tile is routinely the only connection a session has, so closing it
// empties the session. Ownership must survive that: a laptop that wakes for
// half a minute every hour with nobody at it reconnects into exactly this
// state, and its ordinary reconnect resize would otherwise claim the vacated
// window and shrink it under the machine actually being used.
func TestOwnershipOutlivesEveryConnectionDropping(t *testing.T) {
	a := NewArbiter()
	now := time.Now()
	a.now = func() time.Time { return now }

	owner := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	resizeAllowed(t, owner, 200, 50, false)
	owner.Unregister() // the session now has no connections at all

	now = now.Add(time.Hour)
	waker := a.Register("mm-1", "laptop", SizePolicyFollowInput)
	defer waker.Unregister()
	if resizeAllowed(t, waker, 60, 20, false) {
		t.Fatal("a reconnect into an emptied session must not claim the previous owner's window")
	}
}

// Absence alone is not a takeover: the machine claiming the window must have
// someone at it. A dormant tab reconnecting on a wake has not.
func TestIdleClientCannotTakeOverAnAbsentOwner(t *testing.T) {
	a := NewArbiter()
	now := time.Now()
	a.now = func() time.Time { return now }

	owner := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	other := a.Register("mm-1", "laptop", SizePolicyFollowInput)
	defer other.Unregister()
	claimInput(t, owner)
	owner.Unregister()
	markPresent(t, a, "laptop")

	now = now.Add(presenceWindow + time.Second) // the human at the laptop has long gone
	if resizeAllowed(t, other, 60, 20, false) {
		t.Fatal("a client with nobody at it must not take an absent owner's window")
	}
}

func TestPresentClientTakesOverAnAbsentOwner(t *testing.T) {
	a := NewArbiter()
	now := time.Now()
	a.now = func() time.Time { return now }

	owner := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	other := a.Register("mm-1", "laptop", SizePolicyFollowInput)
	defer other.Unregister()
	claimInput(t, owner)
	owner.Unregister()

	now = now.Add(ownerGrace + time.Second)
	markPresent(t, a, "laptop")
	if !resizeAllowed(t, other, 60, 20, false) {
		t.Fatal("a machine with someone at it must take over once the owner has stayed gone")
	}
}

// Records are kept for as long as tmux still has the session; the reconcile
// tick is what forgets the rest, so the map cannot grow without bound.
func TestPruneForgetsOnlySessionsTmuxNoLongerHas(t *testing.T) {
	a := NewArbiter()
	now := time.Now()
	a.now = func() time.Time { return now }

	owner := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	resizeAllowed(t, owner, 200, 50, false)
	owner.Unregister()

	a.Prune(map[string]bool{"mm-1": true})
	kept := a.Register("mm-1", "laptop", SizePolicyFollowInput)
	if resizeAllowed(t, kept, 60, 20, false) {
		t.Fatal("pruning a session tmux still has must keep its owner")
	}
	kept.Unregister()

	a.Prune(map[string]bool{}) // tmux no longer has mm-1
	fresh := a.Register("mm-1", "laptop", SizePolicyFollowInput)
	defer fresh.Unregister()
	if !resizeAllowed(t, fresh, 60, 20, false) {
		t.Fatal("a session tmux has forgotten must arbitrate from scratch")
	}
}

// A connection still registered pins its record: pruning under it would strand
// the live connection's ownership on a map entry nothing else can reach.
func TestPruneKeepsSessionsThatStillHaveConnections(t *testing.T) {
	a := NewArbiter()
	owner := a.Register("mm-1", "desktop", SizePolicyFollowInput)
	defer owner.Unregister()
	resizeAllowed(t, owner, 200, 50, false)

	a.Prune(map[string]bool{})
	if !resizeAllowed(t, owner, 200, 50, false) {
		t.Fatal("a live connection must keep arbitrating after a prune")
	}
	laptop := a.Register("mm-1", "laptop", SizePolicyFollowInput)
	defer laptop.Unregister()
	if resizeAllowed(t, laptop, 60, 20, false) {
		t.Fatal("prune must not have dropped the live owner")
	}
}
