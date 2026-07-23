// Package cmd implements the multimux CLI.
package cmd

import (
	"fmt"
	"io"
	"io/fs"
)

const usage = `usage: multimux <command> [flags]

multimux runs a daemon that serves a browser-based tmux terminal grid over HTTPS.

commands:
  serve [flags]                  run the daemon in the foreground
  service install|uninstall|status|logs
                                 manage the launchd (macOS) / systemd (Linux) user service
  ca trust [flags]               install a multimux CA into the OS trust store
                                 (this host's own, or a remote host's via --remote)
  auth reset --yes               wipe credentials and return to setup-pending
  help [command]                 show detailed help for a command
  --version                      print version

Run "multimux <command> --help" for command-specific flags.

Examples:
  multimux serve --hostname mux.example.com   set a stable hostname (WebAuthn RP ID) and run
  multimux service install                    install and start the background service
  multimux ca trust                           trust this host's own CA
  multimux ca trust --remote user@host        trust a remote host's multimux CA from this machine
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
	case "help", "-h", "--help":
		if len(args) > 1 {
			return helpFor(args[1], stdout, stderr)
		}
		fmt.Fprint(stdout, usage)
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

// helpFor prints the detailed usage for a single command.
func helpFor(cmd string, stdout, stderr io.Writer) int {
	switch cmd {
	case "serve":
		fmt.Fprint(stdout, serveUsage)
	case "service":
		fmt.Fprint(stdout, serviceUsage)
	case "ca":
		fmt.Fprint(stdout, caUsage)
	case "auth":
		fmt.Fprint(stdout, authUsage)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s", cmd, usage)
		return 2
	}
	return 0
}
