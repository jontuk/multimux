package tmuxmgr

import "testing"

func TestFirstConnMayResize(t *testing.T) {
	a := NewArbiter()
	c := a.Register("mm-1")
	defer c.Unregister()
	if !c.Resize(80, 24) {
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
	c1.Resize(80, 24)
	c1.ClaimInput()
	if c2.Resize(120, 40) {
		t.Fatal("non-owner resize must be denied")
	}
	if !c1.Resize(81, 25) {
		t.Fatal("owner resize must be allowed")
	}

	// c2 types → ownership transfers, its last dims come back for reapply.
	cols, rows, reapply := c2.ClaimInput()
	if !reapply || cols != 120 || rows != 40 {
		t.Fatalf("ClaimInput = %d,%d,%v; want 120,40,true", cols, rows, reapply)
	}
	if c1.Resize(80, 24) {
		t.Fatal("old owner must lose resize rights")
	}
}

func TestClaimInputByOwnerIsNoop(t *testing.T) {
	a := NewArbiter()
	c := a.Register("mm-1")
	defer c.Unregister()
	c.Resize(80, 24)
	c.ClaimInput()
	if _, _, reapply := c.ClaimInput(); reapply {
		t.Fatal("re-claim by current owner must not reapply")
	}
}

func TestUnregisterReleasesOwnership(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1")
	c2 := a.Register("mm-1")
	defer c2.Unregister()
	c1.ClaimInput()
	c1.Unregister()
	if !c2.Resize(100, 30) {
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

	if !c2.Resize(100, 30) {
		t.Fatal("remaining connection should still be sole owner and allowed to resize")
	}
	c2.ClaimInput()
	if !c2.Resize(101, 31) {
		t.Fatal("double-Unregister of c1 must not have torn down arbitration state for c2")
	}
}

func TestSessionsIsolated(t *testing.T) {
	a := NewArbiter()
	c1 := a.Register("mm-1")
	c2 := a.Register("mm-2")
	defer c1.Unregister()
	defer c2.Unregister()
	c1.ClaimInput()
	if !c2.Resize(90, 30) {
		t.Fatal("ownership of mm-1 must not affect mm-2")
	}
}
