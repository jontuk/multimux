package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/jontuk/multimux/internal/config"
	"github.com/jontuk/multimux/internal/store"
)

const configUsage = `usage: multimux config <list|get|set> [key] [value]

Read and change user-configurable settings. Values are stored in the daemon's
database, so they survive restarts and are shared with the web Settings page.

  list             print every setting, its effective value, and whether it is
                   still at its default
  get <key>        print one setting's value, with no decoration (for scripts)
  set <key> <val>  change a setting

Examples:
  multimux config list
  multimux config get confirm-terminate
  multimux config set confirm-terminate true
`

// openStore opens the daemon's database for a CLI subcommand. The daemon may
// be running; SQLite handles the two processes.
func openStore() (*store.Store, error) {
	return store.Open(filepath.Join(dataDir(), "multimux.db"))
}

func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, configUsage)
		return 2
	}
	switch args[0] {
	case "list":
		return configList(stdout, stderr)
	case "get":
		if len(args) != 2 {
			fmt.Fprint(stderr, configUsage)
			return 2
		}
		return configGet(args[1], stdout, stderr)
	case "set":
		if len(args) != 3 {
			fmt.Fprint(stderr, configUsage)
			return 2
		}
		return configSet(args[1], args[2], stdout, stderr)
	default:
		fmt.Fprint(stderr, configUsage)
		return 2
	}
}

func configList(stdout, stderr io.Writer) int {
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, k := range config.Keys {
		v, err := config.Get(st, k.Name)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		isDefault, err := config.IsDefault(st, k.Name)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		suffix := ""
		if isDefault {
			suffix = "(default)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", k.Name, v, suffix, k.Help)
	}
	tw.Flush()
	return 0
}

func configGet(name string, stdout, stderr io.Writer) int {
	if _, ok := config.Lookup(name); !ok {
		fmt.Fprintf(stderr, "unknown setting %q — run \"multimux config list\" to see them all\n", name)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()

	v, err := config.Get(st, name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, v)
	return 0
}

func configSet(name, value string, stdout, stderr io.Writer) int {
	k, ok := config.Lookup(name)
	if !ok {
		fmt.Fprintf(stderr, "unknown setting %q — run \"multimux config list\" to see them all\n", name)
		return 2
	}
	// Validate before opening the database so a typo cannot create one.
	if _, err := config.Normalize(k, value); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	st, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()

	if err := config.Set(st, name, value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s = %s\n", name, value)
	// This process is not the daemon, so it cannot push the change to open
	// tabs; say what actually happens instead of implying it is instant.
	fmt.Fprintln(stdout, "open browser tabs pick this up on reload.")
	return 0
}
