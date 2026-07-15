package cmd

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jontuk/multimux/internal/auth"
	"github.com/jontuk/multimux/internal/pki"
	"github.com/jontuk/multimux/internal/server"
	"github.com/jontuk/multimux/internal/store"
	"github.com/jontuk/multimux/internal/tmuxmgr"
)

// hostnames returns [hostname, hostname.local?, extraSANs...] from settings,
// seeding the hostname setting from os.Hostname() on first run. The order is
// deterministic across restarts (hostname first, then sorted extra SANs) so
// pki.Ensure — which compares hostname slices for equality — never sees a
// spurious change and regenerates the CA unnecessarily.
func hostnames(st *store.Store) ([]string, error) {
	host, err := st.GetSetting("hostname")
	if err != nil {
		return nil, err
	}
	if host == "" {
		h, err := os.Hostname()
		if err != nil {
			return nil, err
		}
		host = strings.TrimSuffix(h, ".local")
		if err := st.SetSetting("hostname", host); err != nil {
			return nil, err
		}
	}
	names := []string{host}
	if !strings.Contains(host, ".") {
		names = append(names, host+".local")
	}
	if extra, _ := st.GetSetting("extra_sans"); extra != "" {
		var extras []string
		for _, s := range strings.Split(extra, ",") {
			if s = strings.TrimSpace(s); s != "" {
				extras = append(extras, s)
			}
		}
		slices.Sort(extras)
		names = append(names, extras...)
	}
	return names, nil
}

// rpIDForHost returns the WebAuthn RP ID for a configured hostname.
// go-webauthn's RPID validator rejects any value without a dot (except the
// literal "localhost"), so a bare single-label hostname (the very common case
// on Macs, e.g. "macmini") falls back to its ".local" form — which hostnames()
// always includes as names[1] for dotless names.
func rpIDForHost(host string) string {
	if strings.Contains(host, ".") || host == "localhost" {
		return host
	}
	return host + ".local"
}

// applyHostname validates and persists a hostname supplied via --hostname (or
// MULTIMUX_HOSTNAME). The hostname is the WebAuthn RP ID: changing the RP ID
// silently strands every registered passkey, so if credentials exist and the
// new name derives a different RP ID, refuse and point at `auth reset`.
func applyHostname(st *store.Store, host string) error {
	if host == "" {
		return fmt.Errorf("--hostname: name must not be empty")
	}
	if !strings.Contains(host, ".") && host != "localhost" {
		return fmt.Errorf("--hostname %q: name must contain a dot (or be \"localhost\") — WebAuthn rejects other dotless RP IDs; try %q", host, host+".local")
	}
	prev, err := st.GetSetting("hostname")
	if err != nil {
		return err
	}
	if prev != "" && rpIDForHost(prev) != rpIDForHost(host) {
		n, err := st.CountCredentials()
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("--hostname %q would change the WebAuthn RP ID from %q to %q, which invalidates all %d registered passkey(s).\nRun `multimux auth reset --yes` first, then retry with --hostname", host, rpIDForHost(prev), rpIDForHost(host), n)
		}
	}
	return st.SetSetting("hostname", host)
}

// computeOrigins returns the browser origins allowed to authenticate against
// this daemon (WebAuthn RP origins and the cookie-auth WebSocket origin
// check), one or two per hostname. Browsers omit a default ":443" from the
// Origin, so the portless form is what arrives when the daemon listens on 443
// or sits behind a TLS-terminating proxy (Caddy, Tailscale Serve — see
// docs/proxy.md); without it, login and terminal sockets fail behind a proxy.
// The explicit-port form is kept in proxy mode for proxies that publish a
// non-default port.
func computeOrigins(names []string, port int, behindProxy bool) []string {
	var origins []string
	for _, n := range names {
		if port == 443 {
			origins = append(origins, "https://"+n)
			continue
		}
		origins = append(origins, fmt.Sprintf("https://%s:%d", n, port))
		if behindProxy {
			origins = append(origins, "https://"+n)
		}
	}
	return origins
}

// displayOrigins reorders origins for printing: names likely to resolve from
// another device (dotted, or localhost) first, bare single-label hostnames
// last. A stable partition — relative order within each group is kept.
func displayOrigins(origins []string) []string {
	resolvable := func(o string) bool {
		u, err := url.Parse(o)
		if err != nil {
			return false
		}
		h := u.Hostname()
		return strings.Contains(h, ".") || h == "localhost"
	}
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if resolvable(o) {
			out = append(out, o)
		}
	}
	for _, o := range origins {
		if !resolvable(o) {
			out = append(out, o)
		}
	}
	return out
}

// devOrigins appends the origins the Vite hot-reload loop needs: the dev
// server itself and the daemon's own localhost origin. Listed in both the
// WebAuthn RP origins and the WS origin check, so register/login and the
// events/PTY sockets all work from http://localhost:5173.
func devOrigins(origins []string, port int) []string {
	return append(origins, "http://localhost:5173", fmt.Sprintf("https://localhost:%d", port))
}

// setupBanner renders the first-run banner: one setup URL per origin
// (most-resolvable first — pass origins through displayOrigins) and a pointer
// at --hostname for when none of the names resolve from the user's browser.
func setupBanner(display []string, code string) string {
	var b strings.Builder
	b.WriteString("\n=== multimux setup ===\n")
	label := "Open:"
	for _, o := range display {
		fmt.Fprintf(&b, "%s %s/setup?code=%s\n", label, o, code)
		label = "  or:"
	}
	hint := "If none of these resolve"
	if len(display) == 1 {
		hint = "If this doesn't resolve"
	}
	fmt.Fprintf(&b, "(code expires in 15 minutes; restart to regenerate)\n%s from your browser, restart with: multimux serve --hostname <name-your-browser-can-reach>\n\n", hint)
	return b.String()
}

func runServe(args []string, version string, webFS fs.FS, stdout, stderr io.Writer) int {
	fs2 := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs2.SetOutput(stderr)
	behindProxy := fs2.Bool("behind-proxy", false, "plain HTTP on localhost, trust X-Forwarded-*")
	port := fs2.Int("port", 0, "listen port (default from settings, else 8686)")
	hostname := fs2.String("hostname", "", "hostname browsers reach this daemon at; must contain a dot or be \"localhost\" (persisted; default from settings, else os.Hostname; env MULTIMUX_HOSTNAME)")
	dev := fs2.Bool("dev", false, "DEV MODE: RP ID localhost, allow the Vite dev origin http://localhost:5173 (throwaway MULTIMUX_DATA_DIR only)")
	if err := fs2.Parse(args); err != nil {
		return 2
	}
	if *hostname == "" {
		*hostname = os.Getenv("MULTIMUX_HOSTNAME")
	}

	dir := dataDir()
	st, err := store.Open(filepath.Join(dir, "multimux.db"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()
	if err := st.SeedDefaults(runtime.GOOS); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if *port == 0 {
		*port = 8686
		if p, _ := st.GetSetting("port"); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				*port = n
			}
		}
	}

	if *hostname != "" {
		if err := applyHostname(st, *hostname); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	// --dev swaps the RP ID to localhost, which strands any real passkeys,
	// and loosens the origin checks — never allow it on a real install.
	if *dev {
		n, err := st.CountCredentials()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if n > 0 {
			fmt.Fprintln(stderr, "--dev refused: this data dir has registered passkeys.\nDev mode is for throwaway installs only — point MULTIMUX_DATA_DIR at a scratch directory, e.g.\n  MULTIMUX_DATA_DIR=$(mktemp -d) multimux serve --dev")
			return 1
		}
	}

	names, err := hostnames(st)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rpID := rpIDForHost(names[0])
	origins := computeOrigins(names, *port, *behindProxy)
	if *dev {
		rpID = "localhost"
		origins = devOrigins(origins, *port)
		fmt.Fprintf(stdout, "\n=== DEV MODE ===\nRP ID forced to \"localhost\"; origins http://localhost:5173 and https://localhost:%d allowed.\nRegister/login at http://localhost:5173 (Chrome/Firefox — Safari won't send Secure cookies over http://localhost).\nDo not use this data dir for a real install.\n\n", *port)
	}

	am, err := auth.New(st, rpID, origins)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	tm := tmuxmgr.New("mm", "")
	if err := tm.Available(); err != nil {
		fmt.Fprintf(stderr, "startup check failed: %v\ninstall tmux and retry\n", err)
		return 1
	}

	srv := server.New(server.Config{
		Store: st, Auth: am, Tmux: tm, Arbiter: tmuxmgr.NewArbiter(),
		WebFS: webFS, Origins: origins, Version: version,
	})
	srv.StartBackground() // reconcile + tickers, Task 17

	display := displayOrigins(origins)

	// First-run setup URL — one line per name, most-resolvable first, since
	// the bare kernel hostname often doesn't resolve from another device.
	if pending, _ := am.SetupPending(); pending {
		code, err := am.NewSetupCode()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprint(stdout, setupBanner(display, code))
	}

	if *behindProxy {
		addr := fmt.Sprintf("127.0.0.1:%d", *port)
		fmt.Fprintf(stdout, "multimux %s listening on http://%s (proxy mode)\n", version, addr)
		httpSrv := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
		if err := httpSrv.ListenAndServe(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}

	p := pki.New(filepath.Join(dir, "pki"))
	regen, err := p.Ensure(names)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if regen {
		fmt.Fprintln(stdout, "WARNING: local CA regenerated (hostname set changed) — re-run `multimux ca trust`")
	}
	// Daily leaf-rotation check for long-running daemons.
	go func() {
		for range time.Tick(24 * time.Hour) {
			if _, err := p.Ensure(names); err != nil {
				slog.Error("cert rotation", "err", err)
			}
		}
	}()

	addr := fmt.Sprintf(":%d", *port)
	fmt.Fprintf(stdout, "multimux %s listening on %s (%s)\n", version, addr, display[0])
	// p.TLSConfig() re-reads the leaf from disk when it changes, so the
	// rotation ticker above takes effect without a restart.
	httpsSrv := &http.Server{
		Addr: addr, Handler: srv.Handler(),
		TLSConfig:         p.TLSConfig(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := httpsSrv.ListenAndServeTLS("", ""); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
