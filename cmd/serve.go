package cmd

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
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

func runServe(args []string, version string, webFS fs.FS, stdout, stderr io.Writer) int {
	fs2 := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs2.SetOutput(stderr)
	behindProxy := fs2.Bool("behind-proxy", false, "plain HTTP on localhost, trust X-Forwarded-*")
	port := fs2.Int("port", 0, "listen port (default from settings, else 8686)")
	if err := fs2.Parse(args); err != nil {
		return 2
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

	names, err := hostnames(st)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// go-webauthn's RPID validator rejects any value without a dot (except
	// the literal "localhost"), so a bare single-label hostname (the very
	// common case on Macs, e.g. "macmini") can't be used as-is. names[1] is
	// always the ".local" form when names[0] lacks a dot (see hostnames()).
	rpID := names[0]
	if !strings.Contains(rpID, ".") && rpID != "localhost" && len(names) > 1 {
		rpID = names[1]
	}
	var origins []string
	for _, n := range names {
		origins = append(origins, fmt.Sprintf("https://%s:%d", n, *port))
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

	// First-run setup URL.
	if pending, _ := am.SetupPending(); pending {
		code, err := am.NewSetupCode()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "\n=== multimux setup ===\nOpen: %s/setup?code=%s\n(code expires in 15 minutes; restart to regenerate)\n\n", origins[0], code)
	}

	if *behindProxy {
		addr := fmt.Sprintf("127.0.0.1:%d", *port)
		fmt.Fprintf(stdout, "multimux %s listening on http://%s (proxy mode)\n", version, addr)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
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
	fmt.Fprintf(stdout, "multimux %s listening on %s (%s)\n", version, addr, origins[0])
	if err := http.ListenAndServeTLS(addr, p.LeafCertPath(), p.LeafKeyPath(), srv.Handler()); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
