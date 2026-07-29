package gitinfo

import (
	"os"
	"os/exec"
	"testing"
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
		st := BranchStatus(dir)
		if st.Branch != wantBranch || st.State != wantState {
			t.Errorf("%s: BranchStatus = (%q, %q), want (%q, %q)", label, st.Branch, st.State, wantBranch, wantState)
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
		st := BranchStatus(work)
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
