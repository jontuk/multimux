// Package cmd implements the multimux CLI.
package cmd

import (
	"fmt"
	"io"
	"io/fs"
)

const usage = `usage: multimux <command>

commands:
  serve                          run the daemon in the foreground
  service install|uninstall|status   manage the launchd/systemd user service
  auth reset                     wipe credentials and return to setup-pending
  ca trust                       install the local CA into the OS trust store
  --version                      print version
`

// Execute runs the CLI and returns a process exit code. webFS is the
// embedded SPA (web/dist subtree), used by the serve command.
func Execute(args []string, version string, webFS fs.FS, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "--version", "version":
		fmt.Fprintf(stdout, "multimux %s\n", version)
		return 0
	case "ca":
		return runCA(args[1:], stdout, stderr)
	case "auth":
		return runAuth(args[1:], stdout, stderr)
	case "serve":
		return runServe(args[1:], version, webFS, stdout, stderr)
	case "service":
		return runService(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
		return 2
	}
}
