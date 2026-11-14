package gitinfo

import (
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

func TestWebURL(t *testing.T) {
	cases := []struct {
		remote string
		want   string
	}{
		{"git@github.com:org/repo.git", "https://github.com/org/repo"},
		{"git@github.com:org/repo", "https://github.com/org/repo"},
		{"https://github.com/org/repo.git", "https://github.com/org/repo"},
		{"https://github.com/org/repo", "https://github.com/org/repo"},
		{"ssh://git@github.com/org/repo.git", "https://github.com/org/repo"},
		// GitHub Enterprise: any host containing "github".
		{"git@github.example.com:org/repo.git", "https://github.example.com/org/repo"},
		{"https://github.corp.net/org/repo.git", "https://github.corp.net/org/repo"},
		// GitHub Enterprise: hosts with a "ghe" label, either position, and
		// with a non-git ssh user.
		{"git@ghe.example.net:org/repo.git", "https://ghe.example.net/org/repo"},
		{"acme@example.ghe.com:org/repo.git", "https://example.ghe.com/org/repo"},
		{"https://ghe.example.net/org/repo.git", "https://ghe.example.net/org/repo"},
		// Non-GitHub hosts are not linked.
		{"git@gitlab.com:org/repo.git", ""},
		{"https://bitbucket.org/org/repo.git", ""},
		// "ghe" only counts as a whole label.
		{"git@hughes.com:org/repo.git", ""},
		// Garbage.
		{"", ""},
		{"not a url", ""},
	}
	for _, c := range cases {
		if got := WebURL(c.remote); got != c.want {
			t.Errorf("WebURL(%q) = %q, want %q", c.remote, got, c.want)
		}
	}
}

func TestBranchStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	check := func(label, wantBranch, wantState string) {
		t.Helper()
		st, cost := BranchStatus(dir)
		if st.Branch != wantBranch || st.State != wantState {
			t.Errorf("%s: BranchStatus = (%q, %q), want (%q, %q)", label, st.Branch, st.State, wantBranch, wantState)
		}
		if cost <= 0 {
			t.Errorf("%s: BranchStatus cost = %v, want > 0", label, cost)
		}
	}

	check("non-repo", "", "")

	run("init")
	run("checkout", "-b", "feat")
	check("clean repo", "feat", "clean")

	if err := os.WriteFile(dir+"/a.txt", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("untracked file", "feat", "untracked")

	run("add", "a.txt")
	check("tracked change", "feat", "modified")

	// Untracked outranks tracked changes when both are present.
	if err := os.WriteFile(dir+"/b.txt", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("both", "feat", "untracked")
}

func TestParseBranchHeader(t *testing.T) {
	cases := []struct {
		line          string
		branch        string
		ahead, behind int
		noUpstream    bool
	}{
		{"## main...origin/main [ahead 2, behind 1]", "main", 2, 1, false},
		{"## main...origin/main [behind 3]", "main", 0, 3, false},
		{"## feat/x...origin/feat/x", "feat/x", 0, 0, false},
		{"## main...origin/main [gone]", "main", 0, 0, false},
		{"## main", "main", 0, 0, true},
		{"## No commits yet on main", "main", 0, 0, false},
		{"## Initial commit on main", "main", 0, 0, false},
		{"## HEAD (no branch)", "", 0, 0, false},
	}
	for _, c := range cases {
		branch, ahead, behind, noUpstream := parseBranchHeader(c.line)
		if branch != c.branch || ahead != c.ahead || behind != c.behind || noUpstream != c.noUpstream {
			t.Errorf("parseBranchHeader(%q) = (%q, %d, %d, %v), want (%q, %d, %d, %v)",
				c.line, branch, ahead, behind, noUpstream, c.branch, c.ahead, c.behind, c.noUpstream)
		}
	}
}

func TestBranchStatusUpstream(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote := root + "/remote.git"
	work := root + "/work"
	other := root + "/other"

	run := func(dir string, args ...string) {
		t.Helper()
		// Identity and default branch are set per-command so the test does not
		// depend on the machine's global git config. The default branch matters:
		// the bare remote's HEAD symref is created from it, and if it points at a
		// branch that is never pushed, the later clone checks out that unborn
		// branch instead of main and pushes its commit to the wrong ref.
		full := []string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=test", "-c", "init.defaultBranch=main"}
		cmd := exec.Command("git", append(full, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commit := func(dir, name string) {
		t.Helper()
		if err := os.WriteFile(dir+"/"+name, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		run(dir, "add", name)
		run(dir, "commit", "-m", name)
	}
	check := func(label string, wantAhead, wantBehind int, wantNoUpstream bool) {
		t.Helper()
		st, _ := BranchStatus(work)
		if st.Ahead != wantAhead || st.Behind != wantBehind || st.NoUpstream != wantNoUpstream {
			t.Errorf("%s: ahead=%d behind=%d noUpstream=%v, want %d/%d/%v",
				label, st.Ahead, st.Behind, st.NoUpstream, wantAhead, wantBehind, wantNoUpstream)
		}
	}

	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(root, "init", "--bare", remote)
	run(root, "init", work)
	run(work, "checkout", "-b", "main")

	// Unborn branch: nothing has been committed, so there is nothing unpushed.
	check("unborn", 0, 0, false)

	commit(work, "a.txt")
	// Committed but never pushed, and no tracking branch configured.
	check("no upstream", 0, 0, true)

	run(work, "remote", "add", "origin", remote)
	run(work, "push", "-u", "origin", "main")
	check("pushed", 0, 0, false)

	commit(work, "b.txt")
	check("ahead", 1, 0, false)

	// A second clone pushes a commit the work tree has not seen yet.
	run(root, "clone", remote, other)
	commit(other, "c.txt")
	run(other, "push")
	run(work, "fetch")
	check("diverged", 1, 1, false)
}

func TestRepoWebURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Not a repo yet.
	if got := RepoWebURL(dir); got != "" {
		t.Errorf("non-repo: got %q, want empty", got)
	}

	run("init")
	// Repo without origin.
	if got := RepoWebURL(dir); got != "" {
		t.Errorf("no origin: got %q, want empty", got)
	}

	run("remote", "add", "origin", "git@github.com:org/repo.git")
	if got, want := RepoWebURL(dir), "https://github.com/org/repo"; got != want {
		t.Errorf("with origin: got %q, want %q", got, want)
	}
}

func TestGitTimeout(t *testing.T) {
	dir := t.TempDir()
	// A fake git that hangs for 2 seconds in a background child, so the sleep
	// survives the kill still holding the inherited output pipe -- the shape a
	// real git leaves behind when it has started an fsmonitor daemon. Killing
	// the git process alone does not unblock Output; only WaitDelay does.
	script := "#!/bin/sh\nsleep 2 &\nwait\n"
	if err := os.WriteFile(dir+"/git", []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	origTimeout, origDelay := gitTimeout, gitWaitDelay
	gitTimeout, gitWaitDelay = 50*time.Millisecond, 50*time.Millisecond
	defer func() { gitTimeout, gitWaitDelay = origTimeout, origDelay }()

	start := time.Now()
	_, cost, err := gitOutput(dir, "status")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on timed out git call, got nil")
	}
	// Well under the script's 2s: the call must not wait out the orphan.
	if elapsed >= time.Second {
		t.Fatalf("gitOutput took %v, want under 1s", elapsed)
	}
	// A call that ran into the timeout still reports what it cost: that is
	// what stops a poller retrying a hopeless repo at full rate.
	if cost < gitTimeout {
		t.Fatalf("gitOutput cost = %v, want at least the %v timeout", cost, gitTimeout)
	}
}

func TestMissingSpeedups(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// The machine's own git config must not leak in: a developer who has
	// these settings on globally would otherwise see every case pass or fail
	// for the wrong reason.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	orig := fsmonitorSupported
	defer func() { fsmonitorSupported = orig }()

	cases := []struct {
		name      string
		config    [][2]string
		supported bool
		want      Speedups
	}{
		{"nothing set", nil, true, Speedups{FSMonitor: true, UntrackedCache: true}},
		{"both on", [][2]string{{"core.fsmonitor", "true"}, {"core.untrackedCache", "true"}}, true, Speedups{}},
		{"fsmonitor on", [][2]string{{"core.fsmonitor", "true"}}, true, Speedups{UntrackedCache: true}},
		{"fsmonitor off", [][2]string{{"core.fsmonitor", "false"}}, true, Speedups{FSMonitor: true, UntrackedCache: true}},
		// A hook path (Watchman) is fsmonitor too, just not git's own daemon.
		{"fsmonitor hook", [][2]string{{"core.fsmonitor", "/usr/local/bin/watchman-hook"}}, true, Speedups{UntrackedCache: true}},
		{"untracked cache on", [][2]string{{"core.untrackedCache", "true"}}, true, Speedups{FSMonitor: true}},
		// "keep" is git's default: it leaves the cache as it finds it, which
		// on a repo nobody has tuned means off.
		{"untracked cache keep", [][2]string{{"core.untrackedCache", "keep"}}, true, Speedups{FSMonitor: true, UntrackedCache: true}},
		// feature.manyFiles turns the untracked cache on unless it is set itself.
		{"manyFiles", [][2]string{{"feature.manyFiles", "true"}}, true, Speedups{FSMonitor: true}},
		{"manyFiles overridden", [][2]string{{"feature.manyFiles", "true"}, {"core.untrackedCache", "false"}}, true, Speedups{FSMonitor: true, UntrackedCache: true}},
		// A git built without the fsmonitor daemon cannot act on the advice.
		{"no fsmonitor in this git", nil, false, Speedups{UntrackedCache: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			run := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			run("init")
			for _, kv := range c.config {
				run("config", kv[0], kv[1])
			}
			fsmonitorSupported = func() bool { return c.supported }
			if got := MissingSpeedups(dir); got != c.want {
				t.Errorf("MissingSpeedups = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestSpeedupsSettings(t *testing.T) {
	cases := []struct {
		in   Speedups
		want []string
	}{
		{Speedups{}, nil},
		{Speedups{FSMonitor: true}, []string{"core.fsmonitor"}},
		{Speedups{UntrackedCache: true}, []string{"core.untrackedCache"}},
		{Speedups{FSMonitor: true, UntrackedCache: true}, []string{"core.fsmonitor", "core.untrackedCache"}},
	}
	for _, c := range cases {
		if got := c.in.Settings(); !slices.Equal(got, c.want) {
			t.Errorf("%+v.Settings() = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDetectFSMonitor(t *testing.T) {
	// A fake git stands in for builds with and without the daemon: Apple's
	// and git-for-windows have it, most Linux distro builds do not.
	for _, c := range []struct {
		name, options string
		want          bool
	}{
		{"with daemon", "git version 2.54.0\ncpu: arm64\nfeature: fsmonitor--daemon\n", true},
		{"without daemon", "git version 2.43.0\ncpu: x86_64\nsizeof-long: 8\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\ncat <<'OPTIONS'\n" + c.options + "OPTIONS\n"
			if err := os.WriteFile(dir+"/git", []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if got := detectFSMonitor(); got != c.want {
				t.Errorf("detectFSMonitor = %v, want %v", got, c.want)
			}
		})
	}
}
