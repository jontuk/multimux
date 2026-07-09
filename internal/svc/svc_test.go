package svc

import (
	"strings"
	"testing"
)

func TestUnitContentDarwin(t *testing.T) {
	path, content, err := UnitContent("darwin", "/usr/local/bin/multimux", "/tmp/multimux.log")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "Library/LaunchAgents/com.jontuk.multimux.plist") {
		t.Fatalf("path = %s", path)
	}
	for _, want := range []string{"<key>Label</key>", "com.jontuk.multimux",
		"<string>/usr/local/bin/multimux</string>", "<string>serve</string>",
		"<key>KeepAlive</key>", "/tmp/multimux.log"} {
		if !strings.Contains(content, want) {
			t.Fatalf("plist missing %q:\n%s", want, content)
		}
	}
}

func TestUnitContentLinux(t *testing.T) {
	path, content, err := UnitContent("linux", "/usr/local/bin/multimux", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".config/systemd/user/multimux.service") {
		t.Fatalf("path = %s", path)
	}
	for _, want := range []string{"ExecStart=/usr/local/bin/multimux serve", "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(content, want) {
			t.Fatalf("unit missing %q:\n%s", want, content)
		}
	}
}

func TestUnitContentUnsupported(t *testing.T) {
	if _, _, err := UnitContent("windows", "/x", ""); err == nil {
		t.Fatal("want error for unsupported OS")
	}
}
