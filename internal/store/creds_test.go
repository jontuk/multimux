package store

import (
	"testing"
	"time"
)

func TestCredentialCRUD(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	c := Credential{ID: "cred-abc", Name: "laptop", Data: []byte(`{"k":1}`), CreatedAt: now, LastUsedAt: now}
	if err := s.AddCredential(c); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountCredentials(); n != 1 {
		t.Fatalf("count = %d", n)
	}
	if err := s.UpdateCredentialData("cred-abc", []byte(`{"k":2}`)); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListCredentials()
	if len(list) != 1 || string(list[0].Data) != `{"k":2}` || list[0].Name != "laptop" {
		t.Fatalf("list = %+v", list)
	}
	if !list[0].LastUsedAt.After(now.Add(-time.Second)) {
		t.Fatalf("last_used_at not bumped: %v", list[0].LastUsedAt)
	}
	if err := s.DeleteCredential("cred-abc"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountCredentials(); n != 0 {
		t.Fatalf("count after delete = %d", n)
	}
}

func TestAuthSessionLifecycle(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	a := AuthSession{TokenHash: "h1", UserAgent: "Safari", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateAuthSession(a); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetAuthSession("h1")
	if err != nil || !ok || got.UserAgent != "Safari" {
		t.Fatalf("get = %+v, %v, %v", got, ok, err)
	}
	if _, ok, _ := s.GetAuthSession("missing"); ok {
		t.Fatal("missing hash should not be found")
	}
	later := now.Add(48 * time.Hour)
	if err := s.TouchAuthSession("h1", later); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetAuthSession("h1")
	if !got.ExpiresAt.Equal(later) {
		t.Fatalf("expires = %v, want %v", got.ExpiresAt, later)
	}
	// Expiry sweep removes sessions past their expiry.
	n, err := s.DeleteExpiredAuthSessions(later.Add(time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
}

func TestResetAuth(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	s.AddCredential(Credential{ID: "c1", Name: "a", Data: []byte("{}"), CreatedAt: now, LastUsedAt: now})
	s.CreateAuthSession(AuthSession{TokenHash: "h", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	if err := s.ResetAuth(); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountCredentials(); n != 0 {
		t.Fatal("credentials remain")
	}
	if list, _ := s.ListAuthSessions(); len(list) != 0 {
		t.Fatal("auth sessions remain")
	}
}
