package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jontuk/multimux/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "multimux.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestGetReturnsDefaultWhenUnset(t *testing.T) {
	st := testStore(t)
	got, err := Get(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if got != "false" {
		t.Fatalf("Get = %q, want %q", got, "false")
	}
	b, err := Bool(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if b {
		t.Fatal("Bool = true, want false (terminate must not confirm by default)")
	}
}

func TestSetGetRoundTrip(t *testing.T) {
	st := testStore(t)
	if err := Set(st, ConfirmTerminate, "true"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if got != "true" {
		t.Fatalf("Get = %q, want %q", got, "true")
	}
	b, err := Bool(st, ConfirmTerminate)
	if err != nil {
		t.Fatal(err)
	}
	if !b {
		t.Fatal("Bool = false, want true")
	}
}

func TestUnknownKeyIsRejected(t *testing.T) {
	st := testStore(t)
	if _, ok := Lookup("nope"); ok {
		t.Fatal("Lookup found an undefined key")
	}
	if _, err := Get(st, "nope"); err == nil {
		t.Fatal("Get accepted an unknown key")
	}
	// A typo must never write an orphan row.
	if err := Set(st, "nope", "true"); err == nil {
		t.Fatal("Set accepted an unknown key")
	}
	if v, _ := st.GetSetting("nope"); v != "" {
		t.Fatalf("Set wrote an orphan row: %q", v)
	}
}

func TestNormalizeBool(t *testing.T) {
	k, ok := Lookup(ConfirmTerminate)
	if !ok {
		t.Fatal("confirm-terminate is not registered")
	}
	for _, raw := range []string{"true", "false"} {
		got, err := Normalize(k, raw)
		if err != nil {
			t.Fatalf("Normalize(%q) errored: %v", raw, err)
		}
		if got != raw {
			t.Fatalf("Normalize(%q) = %q", raw, got)
		}
	}
	for _, raw := range []string{"", "yes", "1", "TRUE", "maybe"} {
		if _, err := Normalize(k, raw); err == nil {
			t.Fatalf("Normalize(%q) was accepted, want an error", raw)
		} else if !strings.Contains(err.Error(), "true") {
			t.Fatalf("error should name the accepted values, got %v", err)
		}
	}
}

func TestSetRejectsInvalidValue(t *testing.T) {
	st := testStore(t)
	if err := Set(st, ConfirmTerminate, "yes"); err == nil {
		t.Fatal("Set accepted an invalid bool")
	}
	if v, _ := st.GetSetting("confirm_terminate"); v != "" {
		t.Fatalf("invalid value was written: %q", v)
	}
}

func TestEveryKeyHasAValidDefaultAndHelp(t *testing.T) {
	for _, k := range Keys {
		if _, err := Normalize(k, k.Default); err != nil {
			t.Fatalf("key %q has an invalid default %q: %v", k.Name, k.Default, err)
		}
		if k.Help == "" {
			t.Fatalf("key %q has no help text", k.Name)
		}
		if strings.Contains(k.Name, "_") {
			t.Fatalf("key %q must use dashes, not underscores (that is the storage spelling)", k.Name)
		}
	}
}
