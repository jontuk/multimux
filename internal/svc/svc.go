// Package svc installs multimux as a user-level service (launchd LaunchAgent
// on macOS, systemd user unit on Linux).
package svc

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const label = "com.jontuk.multimux"

// defaultPathEnv covers the common tmux install locations (Homebrew on both
// architectures, system paths) when the installing shell's PATH is unavailable.
const defaultPathEnv = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>serve</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
%s	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`

const systemdTemplate = `[Unit]
Description=multimux terminal session daemon

[Service]
ExecStart="%s" serve
%sRestart=on-failure
# Signal only the daemon on stop; the tmux server lives in this cgroup and
# must survive service stop/restart and binary upgrades.
KillMode=process

[Install]
WantedBy=default.target
`

// defaultLogPath is where launchd sends the daemon's stdout/stderr on macOS.
// On Linux output goes to journald instead.
func defaultLogPath(home string) string {
	return filepath.Join(home, ".local", "share", "multimux", "multimux.log")
}

// EnvVar is one environment entry baked into the generated unit. A slice
// rather than a map: order is part of the rendered artefact, and unit content
// must be byte-reproducible across installs.
type EnvVar struct{ Key, Value string }

// Options describes the unit to render. The zero value is valid apart from
// ExecPath; empty LogPath and PathEnv fall back to the built-in defaults.
type Options struct {
	ExecPath string
	// LogPath is where launchd sends stdout/stderr (darwin only).
	LogPath string
	// PathEnv becomes the service PATH — launchd and systemd don't inherit the
	// user's shell PATH, so tmux from Homebrew is otherwise invisible to the
	// daemon.
	PathEnv string
	// Env is baked into the unit after PATH, in the given order.
	Env []EnvVar
}

// capturedEnvVars are the serve settings that live *only* in the environment,
// so a service install has to snapshot them: everything else (port, hostname
// after first run, extra SANs) is persisted in the settings table and resolved
// at runtime. Order fixes the order of the rendered Environment entries.
var capturedEnvVars = []string{"MULTIMUX_DATA_DIR", "MULTIMUX_HOSTNAME"}

// CaptureEnv snapshots the installing shell's multimux environment. Without
// this the service starts with an empty environment and resolves the *default*
// data dir — a fresh database, a fresh CA and a setup-pending daemon for anyone
// running under a custom MULTIMUX_DATA_DIR. Unset variables are skipped, so a
// plain install still gets runtime default resolution.
func CaptureEnv() []EnvVar {
	var out []EnvVar
	for _, k := range capturedEnvVars {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		if k == "MULTIMUX_DATA_DIR" {
			// The service has its own working directory, so a relative dir
			// would resolve somewhere else entirely.
			if abs, err := filepath.Abs(v); err == nil {
				v = abs
			}
		}
		out = append(out, EnvVar{Key: k, Value: v})
	}
	return out
}

// unitPath is where the unit for goos is installed.
func unitPath(goos string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", "multimux.service"), nil
	default:
		return "", fmt.Errorf("svc: unsupported OS %q", goos)
	}
}

// UnitContent renders the service definition for goos. Pure: no filesystem
// writes, so tests cover the exact artefact installed.
func UnitContent(goos string, opts Options) (path, content string, err error) {
	path, err = unitPath(goos)
	if err != nil {
		return "", "", err
	}
	pathEnv := opts.PathEnv
	if pathEnv == "" {
		pathEnv = defaultPathEnv
	}
	// PATH goes through the same rendering as the captured variables so both
	// get the same escaping; it stays first for a stable diff against units
	// written by earlier versions.
	env := append([]EnvVar{{Key: "PATH", Value: pathEnv}}, opts.Env...)
	envBlock, err := renderEnv(goos, env)
	if err != nil {
		return "", "", err
	}
	switch goos {
	case "darwin":
		logPath := opts.LogPath
		if logPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", "", err
			}
			logPath = defaultLogPath(home)
		}
		// Every interpolated <string> needs escaping, not just PATH — an exec
		// or log path containing & or < would otherwise corrupt the plist.
		exeXML, err := xmlEscape(opts.ExecPath)
		if err != nil {
			return "", "", err
		}
		logXML, err := xmlEscape(logPath)
		if err != nil {
			return "", "", err
		}
		return path, fmt.Sprintf(plistTemplate, label, exeXML, envBlock, logXML, logXML), nil
	default:
		// Quote the exec path so a binary living under a directory with
		// spaces still parses as a single systemd argument.
		return path, fmt.Sprintf(systemdTemplate, opts.ExecPath, envBlock), nil
	}
}

// renderEnv renders env as the OS-native environment block: plist <key>/<string>
// pairs (already inside <dict>) or systemd Environment= lines.
func renderEnv(goos string, env []EnvVar) (string, error) {
	var b strings.Builder
	for _, e := range env {
		if err := validateEnv(e); err != nil {
			return "", err
		}
		switch goos {
		case "darwin":
			k, err := xmlEscape(e.Key)
			if err != nil {
				return "", err
			}
			v, err := xmlEscape(e.Value)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "\t\t<key>%s</key><string>%s</string>\n", k, v)
		default:
			fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", e.Key, systemdEscape(e.Value))
		}
	}
	return b.String(), nil
}

// systemdEscapeValue escapes a value for the inside of a systemd
// Environment="KEY=value" assignment. Two independent layers, both applied in
// a single pass so neither re-escapes the other's output:
//   - unit-file quoting: \ and " terminate or escape within the double-quoted
//     word, so they need a backslash;
//   - specifier expansion: systemd resolves %X specifiers in the value after
//     unquoting, so a literal % must be written %%.
var systemdEscapeValue = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`)

func systemdEscape(s string) string { return systemdEscapeValue.Replace(s) }

// validateEnv rejects entries that cannot be represented in a unit file rather
// than emitting something that silently parses as garbage: a newline ends the
// systemd assignment mid-value, and a NUL truncates the environment block.
func validateEnv(e EnvVar) error {
	if e.Key == "" {
		return errors.New("svc: empty environment variable name")
	}
	for _, r := range e.Key {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return fmt.Errorf("svc: invalid environment variable name %q", e.Key)
		}
	}
	if e.Key[0] >= '0' && e.Key[0] <= '9' {
		return fmt.Errorf("svc: invalid environment variable name %q", e.Key)
	}
	if i := strings.IndexAny(e.Value, "\n\r\x00"); i >= 0 {
		return fmt.Errorf("svc: %s contains a control character (offset %d) and cannot be written to a service unit", e.Key, i)
	}
	return nil
}

func xmlEscape(s string) (string, error) {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return "", err
	}
	return b.String(), nil
}

func Install(goos, execPath string) error {
	path, content, err := UnitContent(goos, Options{
		ExecPath: execPath,
		PathEnv:  os.Getenv("PATH"),
		Env:      CaptureEnv(),
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	switch goos {
	case "darwin":
		// Create log directory before bootstrap since launchd opens StandardOutPath at spawn.
		// On a clean machine, the first bootstrap fails if ~/.local/share/multimux doesn't exist.
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		logPath := defaultLogPath(home)
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return err
		}

		uid := os.Getuid()
		// bootout first so re-install is idempotent; ignore its error.
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, label)).Run()
		return runCmd("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), path)
	default:
		if err := runCmd("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return runCmd("systemctl", "--user", "enable", "--now", "multimux")
	}
}

// runStopCmd runs the service manager's stop command. Package var so tests can
// exercise the error policy without a real launchd/systemd.
var runStopCmd = func(c *exec.Cmd) ([]byte, error) { return c.CombinedOutput() }

// stopCmd is the command that stops (and, on Linux, disables) the service.
func stopCmd(goos string) *exec.Cmd {
	if goos == "darwin" {
		return exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
	}
	return exec.Command("systemctl", "--user", "disable", "--now", "multimux")
}

// notInstalledMarkers identify a stop failure that only means "there was
// nothing to stop" — the service was never installed, is already stopped, or
// the service manager itself is absent. Those must not turn `service
// uninstall` into an error, but every other failure (a daemon that refuses to
// stop, a permission problem) has to reach the user.
var notInstalledMarkers = []string{
	"no such process",     // launchctl bootout, not loaded
	"could not find",      // launchctl bootout, unknown label
	"not loaded",          // launchctl
	"does not exist",      // systemctl, unit file absent
	"not found",           // systemctl "Unit multimux.service not found", exec lookup
	"no such file",        // exec lookup, systemctl
	"file does not exist", // systemctl variants
}

// stopService stops the service, treating "nothing to stop" as success.
func stopService(goos string) error {
	c := stopCmd(goos)
	out, err := runStopCmd(c)
	if err == nil {
		return nil
	}
	hay := strings.ToLower(string(out) + " " + err.Error())
	for _, m := range notInstalledMarkers {
		if strings.Contains(hay, m) {
			return nil
		}
	}
	return fmt.Errorf("stopping service (%s): %w: %s", strings.Join(c.Args, " "), err, bytes.TrimSpace(out))
}

// Uninstall stops the service and removes its unit file. The unit file is
// removed even when the stop fails, so a broken service can always be
// uninstalled; the stop error is still returned so the failure is visible
// rather than silently swallowed.
func Uninstall(goos string) error {
	path, err := unitPath(goos)
	if err != nil {
		return err
	}
	stopErr := stopService(goos)
	var rmErr error
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		rmErr = fmt.Errorf("removing %s: %w", path, err)
	}
	return errors.Join(stopErr, rmErr)
}

// LogsCommand returns the command that shows the daemon's logs: less on the
// log file on macOS, journalctl on Linux (systemd sends output to journald,
// so there is no file to open).
func LogsCommand(goos string) (*exec.Cmd, error) {
	switch goos {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return exec.Command("less", defaultLogPath(home)), nil
	case "linux":
		return exec.Command("journalctl", "--user", "-u", "multimux"), nil
	default:
		return nil, fmt.Errorf("svc: unsupported OS %q", goos)
	}
}

func Status(goos string) (string, error) {
	switch goos {
	case "darwin":
		out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).CombinedOutput()
		return string(out), err
	default:
		out, err := exec.Command("systemctl", "--user", "status", "multimux").CombinedOutput()
		return string(out), err
	}
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, out)
	}
	return nil
}
