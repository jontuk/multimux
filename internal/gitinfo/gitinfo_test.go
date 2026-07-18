package gitinfo

import (
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
		// Non-GitHub hosts are not linked.
		{"git@gitlab.com:org/repo.git", ""},
		{"https://bitbucket.org/org/repo.git", ""},
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
