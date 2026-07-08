package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/jontuk/multimux/internal/store"
)

func runAuth(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "reset" {
		fmt.Fprintln(stderr, "usage: multimux auth reset --yes")
		return 2
	}
	if !slices.Contains(args, "--yes") {
		fmt.Fprintln(stderr, "auth reset wipes ALL passkeys and login sessions.\nRe-run with --yes to confirm.")
		return 2
	}
	st, err := store.Open(filepath.Join(dataDir(), "multimux.db"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()
	if err := st.DeleteAllCredentials(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := st.DeleteAllAuthSessions(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "credentials wiped — daemon is setup-pending.\nRestart it (or it will notice on next request) and open the setup URL it prints.")
	return 0
}
