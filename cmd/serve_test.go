package cmd

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jontuk/multimux/internal/store"
)

func TestRPIDForHost(t *testing.T) {
	cases := []struct{ host, want string }{
		{"mux.example.com", "mux.example.com"},
		{"mybox.local", "mybox.local"},
		{"localhost", "localhost"},
		// go-webauthn rejects dotless RP IDs, so bare names fall back to .local.
		{"mybox", "mybox.local"},
	}
	for _, tc := range cases {
		if got := rpIDForHost(tc.host); got != tc.want {
			t.Errorf("rpIDForHost(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestDisplayOrigins(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			// Bare single-label hostnames are the least likely to resolve from
			// another device; the .local form goes first.
			name: "dotted form preferred over bare hostname",
			in:   []string{"https://mybox:8686", "https://mybox.local:8686"},
			want: []string{"https://mybox.local:8686", "https://mybox:8686"},
		},
		{
			name: "already dotted stays in order",
			in:   []string{"https://mux.example.com:8686", "https://mux.example.com"},
			want: []string{"https://mux.example.com:8686", "https://mux.example.com"},
		},
		{
			name: "extra SANs keep relative order after reorder",
			in:   []string{"https://mybox:8686", "https://mybox.local:8686", "https://mybox.tail1234.ts.net:8686"},
			want: []string{"https://mybox.local:8686", "https://mybox.tail1234.ts.net:8686", "https://mybox:8686"},
		},
		{
			// localhost has no dot but always resolves locally; don't demote it.
			name: "localhost counts as resolvable",
			in:   []string{"https://mybox:8686", "https://localhost:8686"},
			want: []string{"https://localhost:8686", "https://mybox:8686"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := displayOrigins(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("displayOrigins(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSetupBanner(t *testing.T) {
	t.Run("one line per origin, singular hint", func(t *testing.T) {
		got := setupBanner([]string{"https://mux.example.com:8686"}, "ABC123")
		want := "\n=== multimux setup ===\n" +
			"Open: https://mux.example.com:8686/setup?code=ABC123\n" +
			"(code expires in 15 minutes; restart to regenerate)\n" +
			"If this doesn't resolve from your browser, restart with: multimux serve --hostname <name-your-browser-can-reach>\n\n"
		if got != want {
			t.Fatalf("got:\n%q\nwant:\n%q", got, want)
		}
	})
	t.Run("multiple origins, plural hint", func(t *testing.T) {
		got := setupBanner([]string{"https://mybox.local:8686", "https://mybox:8686"}, "ABC123")
		want := "\n=== multimux setup ===\n" +
			"Open: https://mybox.local:8686/setup?code=ABC123\n" +
			"  or: https://mybox:8686/setup?code=ABC123\n" +
			"(code expires in 15 minutes; restart to regenerate)\n" +
			"If none of these resolve from your browser, restart with: multimux serve --hostname <name-your-browser-can-reach>\n\n"
		if got != want {
			t.Fatalf("got:\n%q\nwant:\n%q", got, want)
		}
	})
}

func TestDevOrigins(t *testing.T) {
	got := devOrigins([]string{"https://mybox:8686", "https://mybox.local:8686"}, 8686)
	want := []string{
		"https://mybox:8686", "https://mybox.local:8686",
		// Vite dev server origin and the daemon's own localhost origin — both
		// needed so WebAuthn RP origin checks and checkWSOrigin accept the
		// hot-reload UI.
		"http://localhost:5173", "https://localhost:8686",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("devOrigins = %v, want %v", got, want)
	}
}

func TestTmuxSocket(t *testing.T) {
	if got := tmuxSocket(false, "/tmp/multimux-dev-a"); got != "" {
		t.Fatalf("production tmux socket = %q, want default socket", got)
	}

	first := tmuxSocket(true, "/tmp/multimux-dev-a")
	if !strings.HasPrefix(first, "multimux-dev-") {
		t.Fatalf("dev tmux socket = %q, want multimux-dev-*", first)
	}
	if got := tmuxSocket(true, "/tmp/multimux-dev-a"); got != first {
		t.Fatalf("dev tmux socket changed for the same data dir: %q then %q", first, got)
	}
	if got := tmuxSocket(true, "/tmp/multimux-dev-b"); got == first {
		t.Fatalf("different dev data dirs share tmux socket %q", first)
	}

	root := t.TempDir()
	t.Chdir(root)
	if rel, abs := tmuxSocket(true, "dev"), tmuxSocket(true, filepath.Join(root, "dev")); rel != abs {
		t.Fatalf("relative and absolute paths select different sockets: %q and %q", rel, abs)
	}
}

// --dev must never run against a daemon that has real passkeys: it swaps the
// RP ID to localhost, which would strand them, and it loosens origin checks.
func TestRunServeDevRefusesWhenCredentialsExist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MULTIMUX_DATA_DIR", dir)
	st, err := store.Open(filepath.Join(dir, "multimux.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddCredential(store.Credential{
		ID: "cred1", Name: "key", Data: []byte("{}"),
		CreatedAt: time.Now(), LastUsedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	var out, errBuf strings.Builder
	code := runServe([]string{"--dev"}, "test", nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr: %s)", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "MULTIMUX_DATA_DIR") {
		t.Fatalf("stderr = %q, want throwaway-data-dir hint", errBuf.String())
	}
}

// Wiring test: a bad --hostname must fail fast, before any listener or tmux
// dependency, so it is safe to run in CI.
func TestRunServeRejectsDotlessHostname(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	var out, errBuf strings.Builder
	code := runServe([]string{"--hostname", "mybox"}, "test", nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr: %s)", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "dot") {
		t.Fatalf("stderr = %q, want dotless-name explanation", errBuf.String())
	}
}

func TestRunServeHostnameEnvVar(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", t.TempDir())
	t.Setenv("MULTIMUX_HOSTNAME", "nodots")
	var out, errBuf strings.Builder
	code := runServe(nil, "test", nil, &out, &errBuf)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr: %s)", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "dot") {
		t.Fatalf("stderr = %q, want dotless-name explanation", errBuf.String())
	}
}

func TestApplyHostname(t *testing.T) {
	t.Run("rejects dotless names", func(t *testing.T) {
		st := testStore(t)
		err := applyHostname(st, "mybox")
		if err == nil || !strings.Contains(err.Error(), "dot") {
			t.Fatalf("want dotless-name error, got %v", err)
		}
	})

	t.Run("rejects empty name", func(t *testing.T) {
		st := testStore(t)
		if err := applyHostname(st, ""); err == nil {
			t.Fatal("want error for empty name")
		}
	})

	t.Run("accepts localhost", func(t *testing.T) {
		st := testStore(t)
		if err := applyHostname(st, "localhost"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("persists to hostname setting", func(t *testing.T) {
		st := testStore(t)
		if err := applyHostname(st, "mux.example.com"); err != nil {
			t.Fatal(err)
		}
		got, err := st.GetSetting("hostname")
		if err != nil {
			t.Fatal(err)
		}
		if got != "mux.example.com" {
			t.Fatalf("hostname setting = %q, want %q", got, "mux.example.com")
		}
	})

	t.Run("refuses RP ID change when credentials exist", func(t *testing.T) {
		st := testStore(t)
		if err := st.SetSetting("hostname", "old.example.com"); err != nil {
			t.Fatal(err)
		}
		if err := st.AddCredential(store.Credential{
			ID: "cred1", Name: "key", Data: []byte("{}"),
			CreatedAt: time.Now(), LastUsedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		err := applyHostname(st, "new.example.com")
		if err == nil || !strings.Contains(err.Error(), "auth reset") {
			t.Fatalf("want refusal pointing at auth reset, got %v", err)
		}
		// Setting must be untouched after the refusal.
		got, _ := st.GetSetting("hostname")
		if got != "old.example.com" {
			t.Fatalf("hostname setting = %q, want unchanged %q", got, "old.example.com")
		}
	})

	t.Run("allows same-RP-ID change when credentials exist", func(t *testing.T) {
		st := testStore(t)
		if err := st.SetSetting("hostname", "mybox"); err != nil {
			t.Fatal(err)
		}
		if err := st.AddCredential(store.Credential{
			ID: "cred1", Name: "key", Data: []byte("{}"),
			CreatedAt: time.Now(), LastUsedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		// "mybox" and "mybox.local" share the RP ID "mybox.local".
		if err := applyHostname(st, "mybox.local"); err != nil {
			t.Fatal(err)
		}
		got, _ := st.GetSetting("hostname")
		if got != "mybox.local" {
			t.Fatalf("hostname setting = %q, want %q", got, "mybox.local")
		}
	})
}

func TestComputeOrigins(t *testing.T) {
	cases := []struct {
		name        string
		names       []string
		port        int
		behindProxy bool
		want        []string
	}{
		{
			name:  "direct TLS on custom port",
			names: []string{"mybox", "mybox.local"}, port: 8686,
			want: []string{"https://mybox:8686", "https://mybox.local:8686"},
		},
		{
			// Browsers omit a default ":443" from the Origin header.
			name:  "direct TLS on 443 uses portless origins",
			names: []string{"mux.example.com"}, port: 443,
			want: []string{"https://mux.example.com"},
		},
		{
			// Behind Caddy/Tailscale Serve the public origin is portless; the
			// explicit-port form stays for proxies published on odd ports.
			name:  "behind proxy adds portless origins",
			names: []string{"mux.example.com"}, port: 8686, behindProxy: true,
			want: []string{"https://mux.example.com:8686", "https://mux.example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeOrigins(tc.names, tc.port, tc.behindProxy)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("computeOrigins(%v, %d, %v) = %v, want %v",
					tc.names, tc.port, tc.behindProxy, got, tc.want)
			}
		})
	}
}
