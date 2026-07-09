// Package svc installs multimux as a user-level service (launchd LaunchAgent
// on macOS, systemd user unit on Linux).
package svc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const label = "com.jontuk.multimux"

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
Restart=on-failure

[Install]
WantedBy=default.target
`

// UnitContent renders the service definition for goos. Pure: no filesystem
// writes, so tests cover the exact artefact installed.
func UnitContent(goos, execPath, logPath string) (path, content string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	switch goos {
	case "darwin":
		if logPath == "" {
			logPath = filepath.Join(home, ".local", "share", "multimux", "multimux.log")
		}
		path = filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		return path, fmt.Sprintf(plistTemplate, label, execPath, logPath, logPath), nil
	case "linux":
		path = filepath.Join(home, ".config", "systemd", "user", "multimux.service")
		return path, fmt.Sprintf(systemdTemplate, execPath), nil
	default:
		return "", "", fmt.Errorf("svc: unsupported OS %q", goos)
	}
}

func Install(goos, execPath string) error {
	path, content, err := UnitContent(goos, execPath, "")
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
	path, _, err := UnitContent(goos, "/unused", "")
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
