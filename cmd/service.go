package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/jontuk/multimux/internal/svc"
)

const serviceUsage = `usage: multimux service install|uninstall|status|logs

Manage the multimux background service (launchd on macOS, systemd --user on Linux).

  install     write the unit, enable it, and start the daemon
  uninstall   stop and remove the unit (leaves data and tmux sessions intact)
  status      print the service manager's status for the daemon
  logs        follow the daemon's logs

The installed unit runs a bare "multimux serve" with no flags. To set a hostname
or port, persist it first (run "multimux serve --hostname <name>" once, then
Ctrl-C), or edit the unit after install. --behind-proxy is runtime-only and NOT
persisted, so a service install always runs in direct-TLS mode.
`

func runService(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, serviceUsage)
		return 2
	}
	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := svc.Install(runtime.GOOS, exe); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "service installed and started — check `multimux service status`")
		return 0
	case "uninstall":
		if err := svc.Uninstall(runtime.GOOS); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "service removed")
		return 0
	case "status":
		out, err := svc.Status(runtime.GOOS)
		fmt.Fprint(stdout, out)
		if err != nil {
			return 1
		}
		return 0
	case "logs":
		cmd, err := svc.LogsCommand(runtime.GOOS)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprint(stderr, serviceUsage)
		return 2
	}
}
