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
	for _, want := range []string{"ExecStart=/usr/local/bin/multimux serve", "Restart=on-failure", "WantedBy=default.target",
		"Environment=\"PATH=/usr/local/bin:/usr/bin:/bin\""} {
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
