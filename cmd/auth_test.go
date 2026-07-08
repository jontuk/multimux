package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jontuk/multimux/internal/store"
)

func TestAuthResetRequiresYes(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	if code := Execute([]string{"auth", "reset"}, "dev", fstest.MapFS{}, &out, &errOut); code != 2 {
		t.Fatalf("code = %d, want 2 (needs --yes)", code)
	}
}

func TestAuthResetWipes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MULTIMUX_DATA_DIR", dir)
	st, err := store.Open(filepath.Join(dir, "multimux.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	st.AddCredential(store.Credential{ID: "c", Name: "k", Data: []byte("{}"), CreatedAt: now, LastUsedAt: now})
	st.CreateAuthSession(store.AuthSession{TokenHash: "h", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	st.Close()

	var out, errOut bytes.Buffer
	if code := Execute([]string{"auth", "reset", "--yes"}, "dev", fstest.MapFS{}, &out, &errOut); code != 0 {
		t.Fatalf("code = %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "setup-pending") {
		t.Fatalf("output missing guidance: %q", out.String())
	}

	st, _ = store.Open(filepath.Join(dir, "multimux.db"))
	defer st.Close()
	if n, _ := st.CountCredentials(); n != 0 {
		t.Fatal("credentials survived reset")
	}
	if list, _ := st.ListAuthSessions(); len(list) != 0 {
		t.Fatal("auth sessions survived reset")
	}
}
