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
		branch, state := BranchStatus(dir)
		if branch != wantBranch || state != wantState {
			t.Errorf("%s: BranchStatus = (%q, %q), want (%q, %q)", label, branch, state, wantBranch, wantState)
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
