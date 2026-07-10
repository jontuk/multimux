// Package svc installs multimux as a user-level service (launchd LaunchAgent
// on macOS, systemd user unit on Linux).
package svc

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		<key>PATH</key><string>%s</string>
	</dict>
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
ExecStart=%s serve
Environment="PATH=%s"
Restart=on-failure

[Install]
WantedBy=default.target
`

// UnitContent renders the service definition for goos. Pure: no filesystem
// writes, so tests cover the exact artefact installed. pathEnv is baked into
// the unit as the service PATH — launchd and systemd don't inherit the user's
// shell PATH, so tmux from Homebrew is otherwise invisible to the daemon.
func UnitContent(goos, execPath, logPath, pathEnv string) (path, content string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	if pathEnv == "" {
		pathEnv = defaultPathEnv
	}
	switch goos {
	case "darwin":
		if logPath == "" {
			logPath = filepath.Join(home, ".local", "share", "multimux", "multimux.log")
		}
		path = filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(pathEnv)); err != nil {
			return "", "", err
		}
		return path, fmt.Sprintf(plistTemplate, label, execPath, escaped.String(), logPath, logPath), nil
	case "linux":
		path = filepath.Join(home, ".config", "systemd", "user", "multimux.service")
		return path, fmt.Sprintf(systemdTemplate, execPath, pathEnv), nil
	default:
		return "", "", fmt.Errorf("svc: unsupported OS %q", goos)
	}
}

func Install(goos, execPath string) error {
	path, content, err := UnitContent(goos, execPath, "", os.Getenv("PATH"))
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
		logPath := filepath.Join(home, ".local", "share", "multimux", "multimux.log")
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

func Uninstall(goos string) error {
	path, _, err := UnitContent(goos, "/unused", "", "")
	if err != nil {
		return err
	}
	switch goos {
	case "darwin":
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run()
	default:
		_ = exec.Command("systemctl", "--user", "disable", "--now", "multimux").Run()
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
