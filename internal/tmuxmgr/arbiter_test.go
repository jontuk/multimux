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
	c := a.Register("mm-1")
	defer c.Unregister()
	if !resizeAllowed(t, c, 80, 24, false) {
		t.Fatal("sole connection should be allowed to resize")
	}
}

func TestOwnershipFollowsInput(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1")
	c2 := a.Register("mm-1")
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
	c := a.Register("mm-1")
	defer c.Unregister()
	resizeAllowed(t, c, 80, 24, false)
	claimInput(t, c)
	if _, _, reapply := claimInput(t, c); reapply {
		t.Fatal("re-claim by current owner must not reapply")
	}
}

func TestUnregisterReleasesOwnership(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1")
	c2 := a.Register("mm-1")
	defer c2.Unregister()
	claimInput(t, c1)
	c1.Unregister()
	if !resizeAllowed(t, c2, 100, 30, false) {
		t.Fatal("resize must be allowed once owner disconnects")
	}
}

func TestUnregisterIdempotent(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1")
	c2 := a.Register("mm-1")
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
	c1 := a.Register("mm-1")
	c2 := a.Register("mm-2")
	defer c1.Unregister()
	defer c2.Unregister()
	claimInput(t, c1)
	if !resizeAllowed(t, c2, 90, 30, false) {
		t.Fatal("ownership of mm-1 must not affect mm-2")
	}
}

func TestActiveResizeClaimsAndTransfersOwnership(t *testing.T) {
	a := NewArbiter()
	foreground := a.Register("mm-1")
	background := a.Register("mm-1")
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

func TestResizeApplicationIsSerializedWithOwnership(t *testing.T) {
	a := NewArbiter()
	first := a.Register("mm-1")
	second := a.Register("mm-1")
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
