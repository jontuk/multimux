// Package gitinfo inspects git repositories on the daemon's machine. It owns
// all git introspection so future features (status, commit commands) land here
// rather than in HTTP handlers.
package gitinfo

import (
	"context"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// gitTimeout bounds every git subprocess so a hung repo on a stale NFS mount
// or slow network filesystem cannot wedge the daemon indefinitely. A var so
// tests can verify timeout behaviour without waiting out the full duration.
var gitTimeout = 10 * time.Second

// gitOutput executes a read-only git command against dir with a bounded timeout.
// --no-optional-locks stops git taking .git/index.lock to write back a refreshed
// index: this package polls every few seconds, and on a large repo that lock is
// held long enough to make a user's concurrent commit or stash fail with
// "index.lock exists".
func gitOutput(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...).Output()
}

// RepoWebURL returns the web URL for dir's origin remote, or "" when dir is
// not a git repo, has no origin, or the remote is not GitHub/GHE. git being
// absent is treated the same as no repo.
func RepoWebURL(dir string) string {
	out, err := gitOutput(dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return WebURL(strings.TrimSpace(string(out)))
}

// Status is a directory's git state as of one inspection. The zero value means
// "not a git repo" — State is empty in that case and never otherwise.
type Status struct {
	// Branch is the checked-out branch, empty on a detached HEAD.
	Branch string
	// State is "untracked", "modified" or "clean".
	State string
	// Ahead and Behind count commits relative to the upstream branch; both
	// are zero when there is no upstream.
	Ahead, Behind int
	// NoUpstream marks a branch with commits but no tracking branch — its
	// history has never been pushed anywhere.
	NoUpstream bool
}

// BranchStatus reports dir's branch, working-tree state and position relative
// to its upstream. State is "untracked" when untracked files exist (regardless
// of other changes), "modified" when only tracked files have changes, "clean"
// otherwise. The zero Status is returned when dir is not a git repo or git is
// absent. On a detached HEAD the branch is empty but the rest is still
// reported.
func BranchStatus(dir string) Status {
	// --branch adds a "## " header carrying the upstream and the ahead/behind
	// counts, so divergence costs no extra git process.
	out, err := gitOutput(dir, "status", "--porcelain", "--branch")
	if err != nil {
		return Status{}
	}
	st := Status{State: "clean"}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "## ") {
			st.Ahead, st.Behind, st.NoUpstream = parseBranchHeader(line)
			continue
		}
		if strings.HasPrefix(line, "??") {
			st.State = "untracked"
			break
		}
		if line != "" {
			st.State = "modified"
		}
	}
	// symbolic-ref works on an unborn branch (fresh init); it fails on a
	// detached HEAD, where we leave the branch empty.
	if b, err := gitOutput(dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(string(b))
	}
	return st
}

// parseBranchHeader reads the "## " line of `git status --porcelain --branch`.
// The forms are:
//
//	## main...origin/main [ahead 2, behind 1]
//	## main...origin/main
//	## main                      (no upstream configured)
//	## No commits yet on main    (unborn branch)
//	## HEAD (no branch)          (detached HEAD)
//
// Only a branch that has commits but no upstream counts as never-pushed: an
// unborn branch has nothing to push, and a detached HEAD is not a branch.
func parseBranchHeader(line string) (ahead, behind int, noUpstream bool) {
	rest := strings.TrimPrefix(line, "## ")
	if strings.HasPrefix(rest, "No commits yet on ") || strings.HasPrefix(rest, "HEAD (no branch)") {
		return 0, 0, false
	}
	name, tracking, found := strings.Cut(rest, "...")
	if !found {
		// Bare branch name: no tracking branch configured. Guard against a
		// branch literally named "No commits yet on x" being misread above by
		// requiring a non-empty name here.
		return 0, 0, strings.TrimSpace(name) != ""
	}
	open := strings.Index(tracking, " [")
	if open < 0 || !strings.HasSuffix(tracking, "]") {
		return 0, 0, false
	}
	for _, part := range strings.Split(tracking[open+2:len(tracking)-1], ", ") {
		kind, num, ok := strings.Cut(part, " ")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		switch kind {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind, false
}

// WebURL converts a git remote URL to a browsable https URL. Only GitHub and
// GitHub Enterprise remotes are linked; other hosts return "".
func WebURL(remote string) string {
	host, path := splitRemote(remote)
	if host == "" || !isGitHubHost(host) {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if path == "" {
		return ""
	}
	return "https://" + host + "/" + path
}

// isGitHubHost reports whether host looks like GitHub or a GitHub Enterprise
// install. GHE deployments are commonly named either after github itself
// (github.corp.net) or with a "ghe" label (ghe.corp.net, corp.ghe.com). "ghe"
// must be a whole label: a substring match would also hit hosts like
// hughes.com.
func isGitHubHost(host string) bool {
	host = strings.ToLower(host)
	if strings.Contains(host, "github") {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "ghe" {
			return true
		}
	}
	return false
}

func splitRemote(remote string) (host, path string) {
	// scp-like syntax: git@host:org/repo.git
	if !strings.Contains(remote, "://") {
		at := strings.Index(remote, "@")
		colon := strings.Index(remote, ":")
		if at < 0 || colon < at {
			return "", ""
		}
		return remote[at+1 : colon], remote[colon+1:]
	}
	u, err := url.Parse(remote)
	if err != nil || u.Host == "" {
		return "", ""
	}
	return u.Hostname(), u.Path
}
