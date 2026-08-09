package cmd

import (
	"crypto/sha256"
	"errors"
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
	"github.com/jontuk/multimux/internal/identity"
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

// rpIDForHost returns the WebAuthn RP ID for a configured hostname; the
// shared rules live in internal/identity (also used by the settings API).
func rpIDForHost(host string) string { return identity.RPIDForHost(host) }

// applyHostname validates and persists a hostname supplied via --hostname (or
// MULTIMUX_HOSTNAME) through the shared identity path. The hostname is the
// WebAuthn RP ID: changing the RP ID silently strands every registered
// passkey, so if credentials exist and the new name derives a different RP
// ID, refuse and point at `auth reset`.
func applyHostname(st *store.Store, host string) error {
	_, err := identity.Apply(st, map[string]string{"hostname": host}, false)
	var rpErr *identity.RPChangeError
	if errors.As(err, &rpErr) {
		return fmt.Errorf("--hostname %s.\nRun `multimux auth reset --yes` first, then retry with --hostname", rpErr.Error())
	}
	if err != nil {
		return fmt.Errorf("--hostname: %w", err)
	}
	return nil
}

// computeOrigins returns the browser origins allowed to authenticate against
// this daemon (WebAuthn RP origins and the cookie-auth WebSocket origin
// check), one per hostname. Browsers omit a default ":443" from the Origin, so
// the portless form is what arrives when the daemon listens on 443.
func computeOrigins(names []string, port int) []string {
	var origins []string
	for _, n := range names {
		if port == 443 {
			origins = append(origins, "https://"+n)
			continue
		}
		origins = append(origins, fmt.Sprintf("https://%s:%d", n, port))
	}
	return origins
}

// displayableOrigins returns the origins worth printing as setup/login URLs:
// only those that can pass WebAuthn RP-ID validation for rpID (others are
// dead ends in a browser), most-resolvable first.
func displayableOrigins(origins []string, rpID string) []string {
	return displayOrigins(identity.LoginOrigins(origins, rpID))
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

// tmuxSocket keeps multimux off the user's default tmux server. Production
// uses one stable private socket ("multimux") so sessions persist across
// daemon restarts; dev isolates each throwaway data dir on its own socket.
func tmuxSocket(dev bool, dataDir string) string {
	if !dev {
		return "multimux"
	}
	absDir, err := filepath.Abs(dataDir)
	if err != nil {
		absDir = filepath.Clean(dataDir)
	}
	sum := sha256.Sum256([]byte(absDir))
	return fmt.Sprintf("multimux-dev-%x", sum[:8])
}

// setupBanner renders the first-run banner: one setup URL per origin
// (most-resolvable first — pass origins through displayOrigins) and a pointer
// at --hostname for when none of the names resolve from the user's browser.
func setupBanner(display []string, code string, info pki.CAInfo) string {
	var b strings.Builder
	b.WriteString("\n=== multimux setup ===\n")
	label := "Open:"
	for _, o := range display {
		fmt.Fprintf(&b, "%s %s/setup?code=%s\n", label, o, code)
		label = "  or:"
	}
	returnTarget := url.QueryEscape("/setup?code=" + code)
	fmt.Fprintf(&b, "Android trust: %s/trust?return=%s\n", display[0], returnTarget)
	fmt.Fprintf(&b, "CA SHA-256: %s\n", info.SHA256Fingerprint)
	hint := "If none of these resolve"
	if len(display) == 1 {
		hint = "If this doesn't resolve"
	}
	fmt.Fprintf(&b, "(code expires in 15 minutes; restart to regenerate)\n%s from your browser, restart with: multimux serve --hostname <name-your-browser-can-reach>\n\n", hint)
	return b.String()
}

// caRegenBanner renders the re-trust warning shown whenever the local CA is
// replaced. It names the cause, because "hostname set changed" and "the old CA
// expired" call for the same fix but mean very different things.
func caRegenBanner(reason pki.CARegen, primaryOrigin string, info pki.CAInfo) string {
	return fmt.Sprintf("\n!! WARNING: local CA regenerated (%s)\n"+
		"!! Browsers will refuse this daemon until you re-run `multimux ca trust`\n"+
		"!! Android trust: %s/trust\n"+
		"!! CA SHA-256: %s\n\n", reason, primaryOrigin, info.SHA256Fingerprint)
}

// serveUsage documents `multimux serve` for `multimux help serve`.
const serveUsage = `usage: multimux serve [flags]

Run the multimux daemon in the foreground. By default it terminates TLS itself
using a name-constrained local CA and listens on all interfaces.

flags:
  --hostname <name>   hostname browsers reach this daemon at; must contain a dot
                      or be "localhost". Becomes the WebAuthn RP ID and a TLS SAN.
                      Persisted; also settable via MULTIMUX_HOSTNAME. Choose a
                      stable name BEFORE registering the first passkey — changing
                      it later invalidates all passkeys.
  --trust-ca          after ensuring the local CA, install it into this
                      machine's OS trust store (same as "multimux ca trust").
                      Convenience for first-run setup; failure is non-fatal.
  --port <n>          listen port (persisted; default from settings, else 8686)
  --dev               DEV MODE for throwaway installs only (see the README).
                      DISABLES AUTHENTICATION: every route is served to anyone
                      who can reach the port. Requires an explicit, non-default
                      MULTIMUX_DATA_DIR and refuses a data dir with passkeys.

Environment:
  MULTIMUX_HOSTNAME   default for --hostname
  MULTIMUX_DATA_DIR   data directory (default ~/.local/share/multimux)
`

// devDataDirErr rejects --dev data dirs that could be a real install. --dev
// disables authentication entirely, so the daemon must be pointed at a
// throwaway directory deliberately: an unset MULTIMUX_DATA_DIR (which falls
// back to the install path) or the install path itself is always refused.
// This is independent of the "data dir has passkeys" check — a fresh default
// dir passes that one. env is resolved to an absolute path before comparing,
// so a relative MULTIMUX_DATA_DIR that happens to resolve to the install path
// (e.g. run from $HOME) cannot slip past this guard.
func devDataDirErr(env, home string) error {
	const hint = "--dev refused: it disables authentication entirely, so it must be pointed at a throwaway data dir.\nSet one explicitly, e.g.\n  export MULTIMUX_DATA_DIR=\"/tmp/multimux-dev-$(date +%s)\""
	if strings.TrimSpace(env) == "" {
		return errors.New(hint)
	}
	abs, err := filepath.Abs(env)
	if err != nil {
		return errors.New(hint)
	}
	def := filepath.Join(home, ".local", "share", "multimux")
	if abs == filepath.Clean(def) {
		return errors.New(hint)
	}
	return nil
}

// devBanner warns that the daemon is serving every route unauthenticated. It
// is deliberately loud: anyone who can reach this port gets a shell.
func devBanner(port int) string {
	return fmt.Sprintf("\n!! === DEV MODE: NO AUTHENTICATION === !!\n"+
		"!! Every route on :%d is served to anyone who can reach it — that is a\n"+
		"!! shell as your user, with no passkey and no login.\n"+
		"!! Use this only on a network you control, and only with a throwaway data dir.\n"+
		"!! RP ID forced to \"localhost\"; Vite dev origin http://localhost:5173 allowed.\n\n", port)
}

func runServe(args []string, version string, webFS fs.FS, stdout, stderr io.Writer) int {
	fs2 := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs2.SetOutput(stderr)
	fs2.Usage = func() { fmt.Fprint(stderr, serveUsage) }
	port := fs2.Int("port", 0, "listen port (default from settings, else 8686)")
	hostname := fs2.String("hostname", "", "hostname browsers reach this daemon at; must contain a dot or be \"localhost\" (persisted; default from settings, else os.Hostname; env MULTIMUX_HOSTNAME)")
	dev := fs2.Bool("dev", false, "DEV MODE: NO AUTHENTICATION, RP ID localhost, allow the Vite dev origin http://localhost:5173 (throwaway MULTIMUX_DATA_DIR only)")
	trustCAFlag := fs2.Bool("trust-ca", false, "install the local CA into the OS trust store after ensuring it (like `multimux ca trust`; non-fatal on failure)")
	if err := fs2.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *hostname == "" {
		*hostname = os.Getenv("MULTIMUX_HOSTNAME")
	}

	// --dev disables authentication entirely. Refuse before touching any
	// database: with MULTIMUX_DATA_DIR unset, dataDir() is the real install.
	if *dev {
		devHome, _ := os.UserHomeDir()
		if err := devDataDirErr(os.Getenv("MULTIMUX_DATA_DIR"), devHome); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}

	dir := dataDir()
	st, err := store.Open(filepath.Join(dir, "multimux.db"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()
	// No home directory just means no seeded directory; not worth failing on.
	home, _ := os.UserHomeDir()
	if err := st.SeedDefaults(runtime.GOOS, home); err != nil {
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

	// --dev disables authentication, swaps the RP ID to localhost (which would
	// strand real passkeys), and loosens the origin checks — never allow it on
	// a real install. Second of two independent refusals; the first (an
	// explicit, non-default data dir) ran before the store was opened.
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
	origins := computeOrigins(names, *port)
	if *dev {
		rpID = "localhost"
		origins = devOrigins(origins, *port)
		fmt.Fprint(stdout, devBanner(*port))
		slog.Warn("dev mode: authentication disabled", "port", *port)
		// Dev data dirs start empty; seed the daemon's working directory so
		// the first session can launch without a trip through Settings.
		if dirs, err := st.ListDirs(); err == nil && len(dirs) == 0 {
			if wd, err := os.Getwd(); err == nil {
				if _, err := st.CreateDir("cwd", wd); err == nil {
					fmt.Fprintf(stdout, "dev: seeded default dir %q\n", wd)
				}
			}
		}
	}

	am, err := auth.New(st, rpID, origins)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	p := pki.New(filepath.Join(dir, "pki"))
	regen, err := p.Ensure(names)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	readCA := func() ([]byte, error) { return os.ReadFile(p.CACertPath()) }
	caRaw, err := readCA()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	caInfo, err := pki.InspectCA(caRaw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	tm := tmuxmgr.New("mm", tmuxSocket(*dev, dir))
	if err := tm.Available(); err != nil {
		fmt.Fprintf(stderr, "startup check failed: %v\ninstall tmux and retry\n", err)
		return 1
	}

	srv := server.New(server.Config{
		Store: st, Auth: am, Tmux: tm, Arbiter: tmuxmgr.NewArbiter(),
		WebFS: webFS, Origins: origins, Version: version, ReadCA: readCA,
		NoAuth: *dev,
	})
	srv.StartBackground() // reconcile + tickers, Task 17

	display := displayableOrigins(origins, rpID)

	// First-run setup URL — one line per name, most-resolvable first, since
	// the bare kernel hostname often doesn't resolve from another device.
	// Skipped under --dev: nothing authenticates, so there is no code to use.
	if pending, _ := am.SetupPending(); pending && !*dev {
		code, err := am.NewSetupCode()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprint(stdout, setupBanner(display, code, caInfo))
	}

	if regen != pki.CARegenNone {
		fmt.Fprint(stdout, caRegenBanner(regen, display[0], caInfo))
	}
	if *trustCAFlag {
		if err := trustCA(p.CACertPath(), stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "WARNING: auto-trust failed: %v — run `multimux ca trust` manually\n", err)
		}
	}
	// Daily cert-rotation check for long-running daemons: leaves rotate on
	// their own, but a CA renewal needs the user to re-trust, so say so loudly
	// rather than letting it pass unnoticed months into a run.
	go func() {
		for range time.Tick(24 * time.Hour) {
			regen, err := p.Ensure(names)
			if err != nil {
				slog.Error("cert rotation", "err", err)
				continue
			}
			if regen != pki.CARegenNone {
				caRaw, err := readCA()
				if err != nil {
					slog.Error("read regenerated CA", "err", err)
					continue
				}
				caInfo, err := pki.InspectCA(caRaw)
				if err != nil {
					slog.Error("inspect regenerated CA", "err", err)
					continue
				}
				slog.Warn("local CA regenerated — re-run `multimux ca trust` to restore browser trust",
					"reason", regen.String())
				fmt.Fprint(stdout, caRegenBanner(regen, display[0], caInfo))
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
