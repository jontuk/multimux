package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/jontuk/multimux/internal/store"
)

const authUsage = `usage: multimux auth reset --yes

Wipe ALL passkeys and login sessions, returning the daemon to setup-pending.
Requires local shell access (the same root of trust as first-run setup). Restart
the daemon afterward and open the setup URL it prints to register a new passkey.

  --yes    confirm; without it, auth reset refuses and explains what it would do.
`

func runAuth(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "reset" {
		fmt.Fprint(stderr, authUsage)
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
	if err := st.ResetAuth(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "credentials wiped — daemon is setup-pending.\nNow restart the daemon and open the setup URL it prints.")
	return 0
}
