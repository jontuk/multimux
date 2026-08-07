package svc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestUnitContentDarwin(t *testing.T) {
	path, content, err := UnitContent("darwin", Options{ExecPath: "/usr/local/bin/multimux", LogPath: "/tmp/multimux.log", PathEnv: "/opt/homebrew/bin:/usr/bin:/bin"})
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
	_, content, err := UnitContent("darwin", Options{ExecPath: "/usr/local/bin/multimux"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "<key>PATH</key><string>"+defaultPathEnv+"</string>") {
		t.Fatalf("plist missing default PATH:\n%s", content)
	}
}

func TestUnitContentDarwinEscapesPath(t *testing.T) {
	_, content, err := UnitContent("darwin", Options{ExecPath: "/usr/local/bin/multimux", PathEnv: "/weird&dir:/bin"})
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
	path, content, err := UnitContent("linux", Options{ExecPath: "/usr/local/bin/multimux", PathEnv: "/usr/local/bin:/usr/bin:/bin"})
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
	if _, _, err := UnitContent("windows", Options{ExecPath: "/x"}); err == nil {
		t.Fatal("want error for unsupported OS")
	}
}

func TestUnitContentDarwinEscapesExecAndLogPaths(t *testing.T) {
	_, content, err := UnitContent("darwin", Options{ExecPath: "/Users/a&b/multimux", LogPath: "/tmp/log&dir/mm.log"})
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
	_, content, err := UnitContent("linux", Options{ExecPath: "/home/user/my tools/multimux"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `ExecStart="/home/user/my tools/multimux" serve`) {
		t.Fatalf("exec path not quoted:\n%s", content)
	}
}

// A custom MULTIMUX_DATA_DIR must survive into the unit: without it the
// service resolves the default data dir, i.e. a fresh database, a fresh CA and
// a setup-pending daemon.
func TestUnitContentBakesCapturedEnv(t *testing.T) {
	env := []EnvVar{
		{Key: "MULTIMUX_DATA_DIR", Value: "/srv/multimux/data"},
		{Key: "MULTIMUX_HOSTNAME", Value: "box.example.ts.net"},
	}
	for _, tc := range []struct {
		goos string
		want []string
	}{
		{"darwin", []string{
			"<key>MULTIMUX_DATA_DIR</key><string>/srv/multimux/data</string>",
			"<key>MULTIMUX_HOSTNAME</key><string>box.example.ts.net</string>",
		}},
		{"linux", []string{
			`Environment="MULTIMUX_DATA_DIR=/srv/multimux/data"`,
			`Environment="MULTIMUX_HOSTNAME=box.example.ts.net"`,
		}},
	} {
		_, content, err := UnitContent(tc.goos, Options{ExecPath: "/usr/local/bin/multimux", Env: env})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range tc.want {
			if !strings.Contains(content, w) {
				t.Fatalf("%s unit missing %q:\n%s", tc.goos, w, content)
			}
		}
		// PATH stays first, and the captured entries keep their given order.
		iPath := strings.Index(content, "PATH")
		iDir := strings.Index(content, "MULTIMUX_DATA_DIR")
		iHost := strings.Index(content, "MULTIMUX_HOSTNAME")
		if !(iPath < iDir && iDir < iHost) {
			t.Fatalf("%s env order = PATH:%d DATA_DIR:%d HOSTNAME:%d, want ascending", tc.goos, iPath, iDir, iHost)
		}
	}
}

// An unset variable must leave the unit exactly as it was: default resolution
// at runtime, no empty Environment entry.
func TestUnitContentOmitsUnsetEnv(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		_, content, err := UnitContent(goos, Options{ExecPath: "/usr/local/bin/multimux"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(content, "MULTIMUX_") {
			t.Fatalf("%s unit should carry no MULTIMUX_ env when none is set:\n%s", goos, content)
		}
	}
}

func TestUnitContentEnvDeterministic(t *testing.T) {
	opts := Options{ExecPath: "/usr/local/bin/multimux", Env: []EnvVar{
		{Key: "MULTIMUX_DATA_DIR", Value: "/a"},
		{Key: "MULTIMUX_HOSTNAME", Value: "b"},
	}}
	for _, goos := range []string{"darwin", "linux"} {
		_, first, err := UnitContent(goos, opts)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			_, again, err := UnitContent(goos, opts)
			if err != nil {
				t.Fatal(err)
			}
			if again != first {
				t.Fatalf("%s unit content not reproducible:\n%s\n---\n%s", goos, first, again)
			}
		}
	}
}

func TestUnitContentLinuxEscapesEnv(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"space", "/srv/my data", `Environment="MULTIMUX_DATA_DIR=/srv/my data"`},
		// systemd expands %X specifiers in the value, so a literal % is %%.
		{"percent", "/srv/100%data", `Environment="MULTIMUX_DATA_DIR=/srv/100%%data"`},
		{"quote", `/srv/a"b`, `Environment="MULTIMUX_DATA_DIR=/srv/a\"b"`},
		{"backslash", `/srv/a\b`, `Environment="MULTIMUX_DATA_DIR=/srv/a\\b"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, content, err := UnitContent("linux", Options{
				ExecPath: "/usr/local/bin/multimux",
				Env:      []EnvVar{{Key: "MULTIMUX_DATA_DIR", Value: tc.value}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(content, tc.want) {
				t.Fatalf("unit missing %q:\n%s", tc.want, content)
			}
		})
	}
}

func TestUnitContentDarwinEscapesEnv(t *testing.T) {
	_, content, err := UnitContent("darwin", Options{
		ExecPath: "/usr/local/bin/multimux",
		Env:      []EnvVar{{Key: "MULTIMUX_DATA_DIR", Value: `/srv/a&b"c<d`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "<key>MULTIMUX_DATA_DIR</key><string>/srv/a&amp;b&#34;c&lt;d</string>") {
		t.Fatalf("plist env not XML-escaped:\n%s", content)
	}
	if strings.Contains(content, "a&b") {
		t.Fatalf("plist contains raw ampersand:\n%s", content)
	}
}

// A newline would end the systemd assignment mid-value; refuse rather than
// write a unit that parses as something else.
func TestUnitContentRejectsUnrepresentableEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  EnvVar
	}{
		{"newline", EnvVar{Key: "MULTIMUX_DATA_DIR", Value: "/srv/a\nExecStart=/bin/sh"}},
		{"carriage return", EnvVar{Key: "MULTIMUX_DATA_DIR", Value: "/srv/a\rb"}},
		{"nul", EnvVar{Key: "MULTIMUX_DATA_DIR", Value: "/srv/a\x00b"}},
		{"bad name", EnvVar{Key: "BAD NAME", Value: "x"}},
		{"empty name", EnvVar{Key: "", Value: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, goos := range []string{"darwin", "linux"} {
				if _, _, err := UnitContent(goos, Options{ExecPath: "/x", Env: []EnvVar{tc.env}}); err == nil {
					t.Fatalf("%s: want error for %q=%q", goos, tc.env.Key, tc.env.Value)
				}
			}
		})
	}
}

func TestCaptureEnv(t *testing.T) {
	t.Setenv("MULTIMUX_DATA_DIR", "")
	t.Setenv("MULTIMUX_HOSTNAME", "")
	if got := CaptureEnv(); len(got) != 0 {
		t.Fatalf("CaptureEnv with nothing set = %v, want empty", got)
	}

	t.Setenv("MULTIMUX_DATA_DIR", "/srv/mm")
	got := CaptureEnv()
	want := []EnvVar{{Key: "MULTIMUX_DATA_DIR", Value: "/srv/mm"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("CaptureEnv = %v, want %v", got, want)
	}

	t.Setenv("MULTIMUX_HOSTNAME", "box.example.ts.net")
	got = CaptureEnv()
	if len(got) != 2 || got[0].Key != "MULTIMUX_DATA_DIR" || got[1].Key != "MULTIMUX_HOSTNAME" {
		t.Fatalf("CaptureEnv = %v, want DATA_DIR then HOSTNAME", got)
	}
}

// The service has its own working directory, so a relative data dir has to be
// made absolute at capture time or it resolves elsewhere under the service.
func TestCaptureEnvMakesDataDirAbsolute(t *testing.T) {
	t.Setenv("MULTIMUX_HOSTNAME", "")
	t.Setenv("MULTIMUX_DATA_DIR", "relative/dir")
	got := CaptureEnv()
	if len(got) != 1 {
		t.Fatalf("CaptureEnv = %v, want one entry", got)
	}
	if !filepath.IsAbs(got[0].Value) || !strings.HasSuffix(got[0].Value, "relative/dir") {
		t.Fatalf("data dir = %q, want an absolute path ending in relative/dir", got[0].Value)
	}
}

// stubStop replaces the service-manager call for the duration of the test.
func stubStop(t *testing.T, out string, err error) {
	t.Helper()
	orig := runStopCmd
	t.Cleanup(func() { runStopCmd = orig })
	runStopCmd = func(*exec.Cmd) ([]byte, error) { return []byte(out), err }
}

// writeFakeUnit points HOME at a temp dir and creates the unit file there.
func writeFakeUnit(t *testing.T, goos string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	path, _, err := UnitContent(goos, Options{ExecPath: "/usr/local/bin/multimux"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A real stop failure must not be swallowed — the user needs to know the
// daemon is still running.
func TestUninstallPropagatesStopError(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		t.Run(goos, func(t *testing.T) {
			path := writeFakeUnit(t, goos)
			stubStop(t, "Failed to disable unit: Access denied", errors.New("exit status 1"))
			err := Uninstall(goos)
			if err == nil {
				t.Fatal("want the stop error propagated")
			}
			if !strings.Contains(err.Error(), "Access denied") {
				t.Fatalf("err = %v, want the service manager's output", err)
			}
			// The unit file goes regardless, so uninstall always makes progress.
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("unit file still present after uninstall: %v", err)
			}
		})
	}
}

// Uninstalling something that was never installed is not an error.
func TestUninstallIgnoresNotInstalled(t *testing.T) {
	for _, tc := range []struct{ name, goos, out string }{
		{"launchd not loaded", "darwin", "Boot-out failed: 3: No such process"},
		{"systemd unit absent", "linux", "Failed to disable unit: Unit file multimux.service does not exist."},
		{"no service manager", "linux", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFakeUnit(t, tc.goos)
			err := errors.New("exit status 1")
			if tc.out == "" {
				err = errors.New(`exec: "systemctl": executable file not found in $PATH`)
			}
			stubStop(t, tc.out, err)
			if err := Uninstall(tc.goos); err != nil {
				t.Fatalf("Uninstall = %v, want nil", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("unit file still present after uninstall: %v", err)
			}
		})
	}
}

func TestUninstallMissingUnitFileIsNotAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubStop(t, "", nil)
	if err := Uninstall("linux"); err != nil {
		t.Fatalf("Uninstall = %v, want nil", err)
	}
}

// stubRunCmd records the service-manager commands Install issues instead of
// running them.
func stubRunCmd(t *testing.T) *[]string {
	t.Helper()
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	var got []string
	runCmd = func(name string, args ...string) error {
		got = append(got, strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	return &got
}

// A reinstall over a running daemon has to *restart* it: "enable --now" only
// starts an inactive unit, so an upgraded binary would otherwise never replace
// the old serve process.
func TestInstallLinuxRestartsUnit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := stubRunCmd(t)
	if err := Install("linux", "/usr/local/bin/multimux"); err != nil {
		t.Fatalf("Install = %v", err)
	}
	want := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable multimux",
		"systemctl --user restart multimux",
	}
	if !slices.Equal(*got, want) {
		t.Fatalf("commands = %v, want %v", *got, want)
	}
}

// On macOS the equivalent restart is bootout-then-bootstrap.
func TestInstallDarwinBootsOutBeforeBootstrap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fastPolls(t)
	stubJobLoaded(t, func(string) bool { return false })
	got := stubRunCmd(t)
	if err := Install("darwin", "/usr/local/bin/multimux"); err != nil {
		t.Fatalf("Install = %v", err)
	}
	if len(*got) != 2 || !strings.Contains((*got)[0], "launchctl bootout") || !strings.Contains((*got)[1], "launchctl bootstrap") {
		t.Fatalf("commands = %v", *got)
	}
}

// bootout is async, so bootstrap can transiently fail with "Bootstrap failed:
// 5: Input/output error" when it races the teardown; Install must retry it
// rather than surface a benign timing race as an upgrade failure.
func TestInstallDarwinRetriesBootstrap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fastPolls(t)
	stubJobLoaded(t, func(string) bool { return false })
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	var got []string
	bootstraps := 0
	runCmd = func(name string, args ...string) error {
		cmd := strings.Join(append([]string{name}, args...), " ")
		got = append(got, cmd)
		if strings.Contains(cmd, "bootstrap") {
			bootstraps++
			if bootstraps == 1 {
				return errBootstrapEIO
			}
		}
		return nil
	}
	if err := Install("darwin", "/usr/local/bin/multimux"); err != nil {
		t.Fatalf("Install = %v after retry", err)
	}
	if bootstraps < 2 {
		t.Fatalf("bootstrap attempts = %d, want a retry after the transient failure", bootstraps)
	}
}

// The error launchctl returns both when bootstrap races a teardown and when
// the label is already loaded — the ambiguity this whole dance exists for.
var errBootstrapEIO = errors.New("launchctl bootstrap: exit status 5: Bootstrap failed: 5: Input/output error")

// fastPolls shrinks the launchd poll timings so state-waiting tests do not
// spend real seconds asleep.
func fastPolls(t *testing.T) {
	t.Helper()
	origPoll, origTimeout := pollInterval, bootoutTimeout
	t.Cleanup(func() { pollInterval, bootoutTimeout = origPoll, origTimeout })
	pollInterval, bootoutTimeout = time.Millisecond, 50*time.Millisecond
}

// stubJobLoaded replaces the launchctl domain query with a fake.
func stubJobLoaded(t *testing.T, fn func(target string) bool) {
	t.Helper()
	orig := jobLoaded
	t.Cleanup(func() { jobLoaded = orig })
	jobLoaded = fn
}

// The bug behind "upgrade needs a service install afterwards": launchctl exits
// 5 "Input/output error" when the label is *already* loaded, so once a
// bootstrap has taken effect every retry reports that same error. Install must
// believe the domain, not the exit status — the daemon is up.
func TestInstallDarwinSucceedsWhenTheJobIsLoadedDespiteBootstrapError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fastPolls(t)
	loaded := false
	stubJobLoaded(t, func(string) bool { return loaded })
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	bootstraps := 0
	runCmd = func(name string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "bootstrap") {
			bootstraps++
			// Mimics launchd: the job lands in the domain, launchctl still
			// exits 5 for every attempt.
			loaded = true
			return errBootstrapEIO
		}
		return nil
	}
	if err := Install("darwin", "/usr/local/bin/multimux"); err != nil {
		t.Fatalf("Install = %v, want nil: the job is loaded, so the upgrade succeeded", err)
	}
	if bootstraps != 1 {
		t.Fatalf("bootstrap attempts = %d, want 1 once the job is loaded", bootstraps)
	}
}

// bootout returns while launchd is still tearing the old job down, and
// bootstrapping into a domain that still holds the label is what triggers the
// EIO in the first place. Wait for the label to actually go.
func TestInstallDarwinWaitsForBootoutBeforeBootstrapping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fastPolls(t)
	polls := 0
	stubJobLoaded(t, func(string) bool {
		polls++
		return polls < 3 // still tearing down for the first two checks
	})
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	runCmd = func(name string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "bootstrap") && polls < 3 {
			return fmt.Errorf("bootstrapped while the old job was still loaded")
		}
		return nil
	}
	if err := Install("darwin", "/usr/local/bin/multimux"); err != nil {
		t.Fatalf("Install = %v", err)
	}
	if polls < 3 {
		t.Fatalf("domain polls = %d, want Install to wait for the old job to leave", polls)
	}
}

// The flip side of trusting the domain over the exit status: a job that never
// loads is a real failure and must not be reported as a successful upgrade.
func TestInstallDarwinReportsBootstrapFailureWhenNothingLoads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fastPolls(t)
	stubJobLoaded(t, func(string) bool { return false })
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	runCmd = func(name string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "bootstrap") {
			return errBootstrapEIO
		}
		return nil
	}
	err := Install("darwin", "/usr/local/bin/multimux")
	if err == nil {
		t.Fatal("Install = nil, want the bootstrap error when no job is loaded")
	}
	if !errors.Is(err, errBootstrapEIO) {
		t.Fatalf("Install = %v, want it to carry the launchctl error", err)
	}
}

// An old daemon that refuses to leave the domain is also a real failure: the
// label being loaded then means the *previous* binary is still serving, so
// "already loaded" must not be read as success.
func TestInstallDarwinReportsFailureWhenTheOldJobNeverStops(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fastPolls(t)
	stubJobLoaded(t, func(string) bool { return true }) // never tears down
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	runCmd = func(name string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "bootstrap") {
			return errBootstrapEIO
		}
		return nil
	}
	err := Install("darwin", "/usr/local/bin/multimux")
	if err == nil {
		t.Fatal("Install = nil, want an error while the old daemon is still loaded")
	}
	if !strings.Contains(err.Error(), "did not stop") {
		t.Fatalf("Install = %v, want it to say the previous daemon did not stop", err)
	}
}

func TestInstalledReportsUnitPresence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if Installed("linux") {
		t.Fatal("Installed = true with no unit file")
	}
	stubRunCmd(t)
	if err := Install("linux", "/usr/local/bin/multimux"); err != nil {
		t.Fatalf("Install = %v", err)
	}
	if !Installed("linux") {
		t.Fatal("Installed = false after Install")
	}
}
