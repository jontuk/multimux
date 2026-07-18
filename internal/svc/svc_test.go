package svc

import (
	"strings"
	"testing"
)

func TestUnitContentDarwin(t *testing.T) {
	path, content, err := UnitContent("darwin", "/usr/local/bin/multimux", "/tmp/multimux.log", "/opt/homebrew/bin:/usr/bin:/bin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "Library/LaunchAgents/com.jontuk.multimux.plist") {
		t.Fatalf("path = %s", path)
	}
	for _, want := range []string{"<key>Label</key>", "com.jontuk.multimux",
		"<string>/usr/local/bin/multimux</string>", "<string>serve</string>",
		"<key>KeepAlive</key>", "/tmp/multimux.log",
		"<key>EnvironmentVariables</key>", "<key>PATH</key><string>/opt/homebrew/bin:/usr/bin:/bin</string>"} {
		if !strings.Contains(content, want) {
			t.Fatalf("plist missing %q:\n%s", want, content)
		}
	}
}

func TestUnitContentDarwinEmptyPathFallsBack(t *testing.T) {
	_, content, err := UnitContent("darwin", "/usr/local/bin/multimux", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "<key>PATH</key><string>"+defaultPathEnv+"</string>") {
		t.Fatalf("plist missing default PATH:\n%s", content)
	}
}

func TestUnitContentDarwinEscapesPath(t *testing.T) {
	_, content, err := UnitContent("darwin", "/usr/local/bin/multimux", "", "/weird&dir:/bin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "/weird&amp;dir:/bin") {
		t.Fatalf("plist PATH not XML-escaped:\n%s", content)
	}
	if strings.Contains(content, "/weird&dir") {
		t.Fatalf("plist contains raw ampersand:\n%s", content)
	}
}

func TestUnitContentLinux(t *testing.T) {
	path, content, err := UnitContent("linux", "/usr/local/bin/multimux", "", "/usr/local/bin:/usr/bin:/bin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".config/systemd/user/multimux.service") {
		t.Fatalf("path = %s", path)
	}
	for _, want := range []string{`ExecStart="/usr/local/bin/multimux" serve`, "Restart=on-failure", "WantedBy=default.target",
		"Environment=\"PATH=/usr/local/bin:/usr/bin:/bin\"",
		// KillMode=process keeps the tmux server (same cgroup) alive across
		// service stop/upgrade — the core persistence promise.
		"KillMode=process"} {
		if !strings.Contains(content, want) {
			t.Fatalf("unit missing %q:\n%s", want, content)
		}
	}
}

func TestUnitContentUnsupported(t *testing.T) {
	if _, _, err := UnitContent("windows", "/x", "", ""); err == nil {
		t.Fatal("want error for unsupported OS")
	}
}

func TestUnitContentDarwinEscapesExecAndLogPaths(t *testing.T) {
	_, content, err := UnitContent("darwin", "/Users/a&b/multimux", "/tmp/log&dir/mm.log", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<string>/Users/a&amp;b/multimux</string>", "/tmp/log&amp;dir/mm.log"} {
		if !strings.Contains(content, want) {
			t.Fatalf("plist missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "a&b") || strings.Contains(content, "log&dir") {
		t.Fatalf("plist contains raw ampersand:\n%s", content)
	}
}

func TestLogsCommandDarwin(t *testing.T) {
	cmd, err := LogsCommand("darwin")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "less" {
		t.Fatalf("args = %v, want [less <logPath>]", cmd.Args)
	}
	if !strings.HasSuffix(cmd.Args[1], ".local/share/multimux/multimux.log") {
		t.Fatalf("log path = %s", cmd.Args[1])
	}
}

func TestLogsCommandLinux(t *testing.T) {
	cmd, err := LogsCommand("linux")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"journalctl", "--user", "-u", "multimux"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	for i, w := range want {
		if cmd.Args[i] != w {
			t.Fatalf("args = %v, want %v", cmd.Args, want)
		}
	}
}

func TestLogsCommandUnsupported(t *testing.T) {
	if _, err := LogsCommand("windows"); err == nil {
		t.Fatal("want error for unsupported OS")
	}
}

func TestUnitContentLinuxQuotesExecPath(t *testing.T) {
	_, content, err := UnitContent("linux", "/home/user/my tools/multimux", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `ExecStart="/home/user/my tools/multimux" serve`) {
		t.Fatalf("exec path not quoted:\n%s", content)
	}
}
