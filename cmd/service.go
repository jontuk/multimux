package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/jontuk/multimux/internal/svc"
)

const serviceUsage = `usage: multimux service install|uninstall|upgrade|status|logs

Manage the multimux background service (launchd on macOS, systemd --user on Linux).

  install     write the unit, enable it, and start the daemon
  uninstall   stop and remove the unit (leaves data and tmux sessions intact)
  upgrade     download the latest release binary and restart the service on it
  status      print the service manager's status for the daemon
  logs        follow the daemon's logs

The installed unit runs a bare "multimux serve" with no flags, but "install"
captures MULTIMUX_DATA_DIR and MULTIMUX_HOSTNAME from the installing shell into
the unit, so the service uses the same data directory. Change either variable
later and you must re-run "multimux service install". A rewrite of the unit
("install" again, or "upgrade") keeps whatever the installed unit already
captured unless the current shell sets that variable, so an upgrade run from
an ordinary shell cannot move the daemon onto the default data directory; to
clear a captured variable, run "uninstall" first. To set a port, persist it
first (run "multimux serve --port <n>" once, then Ctrl-C) or use the Settings
page.

"upgrade" pipes the project's install.sh into sh and then, if a unit is already
installed, reinstalls it so it points at the freshly downloaded binary and the
daemon is restarted onto it. It needs network access and may prompt for sudo if
the install directory is not writable. MULTIMUX_INSTALL_DIR is honoured, exactly
as install.sh honours it.
`

// upgradeScript is run by "service upgrade": fetch the latest release binary.
// Reinstalling the unit is done in-process afterwards rather than by chaining
// "multimux service install" here — a PATH lookup can resolve a *different,
// older* multimux than the one install.sh just wrote.
const upgradeScript = "curl -fsSL https://raw.githubusercontent.com/jontuk/multimux/main/install.sh | sh"

// defaultInstallDir mirrors install.sh's INSTALL_DIR default.
const defaultInstallDir = "/usr/local/bin"

// upgradedBinary locates the binary install.sh just wrote. os.Executable() is
// deliberately not used: the running process may be a dev build somewhere else
// entirely, and pointing the unit at that would undo the upgrade.
func upgradedBinary() (string, error) {
	dir := os.Getenv("MULTIMUX_INSTALL_DIR")
	if dir == "" {
		dir = defaultInstallDir
	}
	path := filepath.Join(dir, "multimux")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	// install.sh reported success, so an absent binary at the expected path
	// means a non-default install location; PATH is the remaining clue.
	path, err := exec.LookPath("multimux")
	if err != nil {
		return "", fmt.Errorf("cannot find the upgraded multimux binary: %w", err)
	}
	return path, nil
}

// serviceUnitInstalled and installService wrap the svc package so tests can
// exercise the command without touching the real launchd/systemd unit.
var serviceUnitInstalled = func() bool { return svc.Installed(runtime.GOOS) }

var installService = func(execPath string) error { return svc.Install(runtime.GOOS, execPath) }

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
		if err := installService(exe); err != nil {
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
		// Checked before the download: install.sh does not touch the unit, and
		// after a failed download there is nothing to reinstall anyway.
		hadUnit := serviceUnitInstalled()
		if err := runUpgradeScript(upgradeScript); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !hadUnit {
			fmt.Fprintln(stdout, "binary upgraded; no service unit is installed — run `multimux service install` to run it as a service")
			return 0
		}
		exe, err := upgradedBinary()
		if err != nil {
			fmt.Fprintln(stderr, err)
			fmt.Fprintln(stderr, "the new binary is installed; re-run `multimux service install` yourself to point the service at it")
			return 1
		}
		if err := installService(exe); err != nil {
			fmt.Fprintln(stderr, err)
			// The binary and the unit are already in place, so say what is left
			// undone rather than leaving "upgrade failed" to imply the whole
			// thing has to be redone.
			fmt.Fprintln(stderr, "the new binary and unit are installed but the daemon was not restarted onto it — check `multimux service status`")
			return 1
		}
		fmt.Fprintln(stdout, "upgraded and service restarted on the new binary — check `multimux service status`")
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
