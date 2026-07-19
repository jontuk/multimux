package identity

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jontuk/multimux/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func addCredential(t *testing.T, st *store.Store) {
	t.Helper()
	now := time.Now().UTC()
	if err := st.AddCredential(store.Credential{ID: "c1", Name: "k", Data: []byte("{}"), CreatedAt: now, LastUsedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func setting(t *testing.T, st *store.Store, key string) string {
	t.Helper()
	v, err := st.GetSetting(key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRPIDForHost(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"mybox", "mybox.local"},
		{"mybox.local", "mybox.local"},
		{"mux.example.com", "mux.example.com"},
		{"localhost", "localhost"},
	} {
		if got := RPIDForHost(tc.host); got != tc.want {
			t.Errorf("RPIDForHost(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestApplyPersistsAllKeys(t *testing.T) {
	st := newStore(t)
	rpChanged, err := Apply(st, map[string]string{
		"hostname": "mux.example.com", "extra_sans": " a.ts.net ,b.local, ", "port": "9000",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rpChanged {
		t.Fatal("first hostname set must not report an RP change")
	}
	if got := setting(t, st, "hostname"); got != "mux.example.com" {
		t.Fatalf("hostname = %q", got)
	}
	// SANs are normalized: trimmed, empties dropped.
	if got := setting(t, st, "extra_sans"); got != "a.ts.net,b.local" {
		t.Fatalf("extra_sans = %q", got)
	}
	if got := setting(t, st, "port"); got != "9000" {
		t.Fatalf("port = %q", got)
	}
}

func TestApplyInvalidWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes map[string]string
	}{
		{"dotless hostname", map[string]string{"hostname": "mybox", "port": "9000"}},
		{"empty hostname", map[string]string{"hostname": "", "port": "9000"}},
		{"bad port", map[string]string{"hostname": "mux.example.com", "port": "nope"}},
		{"port out of range", map[string]string{"hostname": "mux.example.com", "port": "70000"}},
		{"SAN with whitespace", map[string]string{"hostname": "mux.example.com", "extra_sans": "a b"}},
		{"SAN with scheme", map[string]string{"hostname": "mux.example.com", "extra_sans": "https://x.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newStore(t)
			if _, err := Apply(st, tc.changes, false); err == nil {
				t.Fatal("want validation error")
			}
			// Partial writes are the bug this package exists to prevent.
			for _, k := range []string{"hostname", "extra_sans", "port"} {
				if got := setting(t, st, k); got != "" {
					t.Fatalf("invalid apply wrote %s = %q", k, got)
				}
			}
		})
	}
}

func TestApplyEmptyPortAndSANsClear(t *testing.T) {
	st := newStore(t)
	if _, err := Apply(st, map[string]string{"hostname": "mux.example.com", "extra_sans": "a.ts.net", "port": "9000"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, map[string]string{"hostname": "mux.example.com", "extra_sans": "", "port": ""}, false); err != nil {
		t.Fatal(err)
	}
	if got := setting(t, st, "extra_sans"); got != "" {
		t.Fatalf("extra_sans = %q, want cleared", got)
	}
	if got := setting(t, st, "port"); got != "" {
		t.Fatalf("port = %q, want cleared", got)
	}
}

func TestApplyGuardsRPChangeWithCredentials(t *testing.T) {
	st := newStore(t)
	if _, err := Apply(st, map[string]string{"hostname": "old.example.com"}, false); err != nil {
		t.Fatal(err)
	}
	addCredential(t, st)

	_, err := Apply(st, map[string]string{"hostname": "new.example.com"}, false)
	var rpErr *RPChangeError
	if !errors.As(err, &rpErr) {
		t.Fatalf("want *RPChangeError, got %v", err)
	}
	if rpErr.Prev != "old.example.com" || rpErr.Next != "new.example.com" || rpErr.Credentials != 1 {
		t.Fatalf("RPChangeError = %+v", rpErr)
	}
	if got := setting(t, st, "hostname"); got != "old.example.com" {
		t.Fatalf("guarded apply wrote hostname = %q", got)
	}

	// Explicit confirmation applies the change and reports it.
	rpChanged, err := Apply(st, map[string]string{"hostname": "new.example.com"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rpChanged {
		t.Fatal("confirmed RP change must report rpChanged")
	}
	if got := setting(t, st, "hostname"); got != "new.example.com" {
		t.Fatalf("hostname = %q", got)
	}
}

func TestApplySameRPIDNeedsNoConfirm(t *testing.T) {
	st := newStore(t)
	if _, err := Apply(st, map[string]string{"hostname": "mybox.local"}, false); err != nil {
		t.Fatal(err)
	}
	addCredential(t, st)
	// mybox.local -> localhost changes the RP ID; mybox.local -> mybox.local is a no-op.
	if _, err := Apply(st, map[string]string{"hostname": "mybox.local"}, false); err != nil {
		t.Fatalf("same hostname must not need confirmation: %v", err)
	}
}

func TestApplyHostnameOnlyKeepsOtherKeys(t *testing.T) {
	st := newStore(t)
	if _, err := Apply(st, map[string]string{"hostname": "old.example.com", "extra_sans": "a.ts.net", "port": "9000"}, false); err != nil {
		t.Fatal(err)
	}
	// CLI --hostname path: only hostname in the change set.
	if _, err := Apply(st, map[string]string{"hostname": "next.example.com"}, false); err != nil {
		t.Fatal(err)
	}
	if got := setting(t, st, "extra_sans"); got != "a.ts.net" {
		t.Fatalf("hostname-only apply clobbered extra_sans = %q", got)
	}
	if got := setting(t, st, "port"); got != "9000" {
		t.Fatalf("hostname-only apply clobbered port = %q", got)
	}
}

func TestLoginOrigins(t *testing.T) {
	origins := []string{
		"https://mybox:8686",           // bare alias: browser rejects RP ID mybox.local here
		"https://mybox.local:8686",     // the RP ID itself
		"https://sub.mybox.local:8686", // subdomain of the RP ID
		"https://192.168.1.5:8686",     // IP: never RP-ID compatible
		"https://other.ts.net:8686",    // unrelated SAN
	}
	got := LoginOrigins(origins, "mybox.local")
	want := []string{"https://mybox.local:8686", "https://sub.mybox.local:8686"}
	if len(got) != len(want) {
		t.Fatalf("LoginOrigins = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LoginOrigins = %v, want %v", got, want)
		}
	}
}
