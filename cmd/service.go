package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/jontuk/multimux/internal/svc"
)

const serviceUsage = `usage: multimux service install|uninstall|upgrade|status|logs

Manage the multimux background service (launchd on macOS, systemd --user on Linux).

  install     write the unit, enable it, and start the daemon
  uninstall   stop and remove the unit (leaves data and tmux sessions intact)
  upgrade     download the latest release binary, then re-run "service install"
  status      print the service manager's status for the daemon
  logs        follow the daemon's logs

The installed unit runs a bare "multimux serve" with no flags, but "install"
captures MULTIMUX_DATA_DIR and MULTIMUX_HOSTNAME from the installing shell into
the unit, so the service uses the same data directory. Change either variable
later and you must re-run "multimux service install". To set a port, persist it
first (run "multimux serve --port <n>" once, then Ctrl-C) or use the Settings
page. --behind-proxy is runtime-only and NOT persisted, so a service install
always runs in direct-TLS mode.

"upgrade" pipes the project's install.sh into sh and then runs "multimux service
install" from PATH, so the reinstalled unit points at the freshly downloaded
binary. It needs network access and may prompt for sudo if the install
directory is not writable.
`

// upgradeScript is run by "service upgrade": fetch the latest release binary,
// then reinstall the unit so it points at it. The second command resolves
// multimux from PATH on purpose — that is the binary install.sh just wrote.
const upgradeScript = "curl -fsSL https://raw.githubusercontent.com/jontuk/multimux/main/install.sh | sh && multimux service install"

// runUpgradeScript is a variable so tests can stub the shell-out.
var runUpgradeScript = func(script string) error {
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

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
		// The unit file is removed even when stopping fails, so say so rather
		// than leaving the user thinking nothing happened.
		if err := svc.Uninstall(runtime.GOOS); err != nil {
			fmt.Fprintln(stderr, err)
			fmt.Fprintln(stderr, "the unit file was removed if it existed; the daemon may still be running — check `multimux service status`")
			return 1
		}
		fmt.Fprintln(stdout, "service removed")
		return 0
	case "upgrade":
		if err := runUpgradeScript(upgradeScript); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "upgraded and service reinstalled — check `multimux service status`")
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
