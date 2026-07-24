// Package gitinfo inspects git repositories on the daemon's machine. It owns
// all git introspection so future features (status, commit commands) land here
// rather than in HTTP handlers.
package gitinfo

import (
	"net/url"
	"os/exec"
	"strings"
)

// RepoWebURL returns the web URL for dir's origin remote, or "" when dir is
// not a git repo, has no origin, or the remote is not GitHub/GHE. git being
// absent is treated the same as no repo.
func RepoWebURL(dir string) string {
	out, err := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	return WebURL(strings.TrimSpace(string(out)))
}

// BranchStatus reports dir's checked-out branch and working-tree state:
// "untracked" when untracked files exist (regardless of other changes),
// "modified" when only tracked files have changes, "clean" otherwise. Both
// results are empty when dir is not a git repo or git is absent. On a
// detached HEAD the branch is empty but the state is still reported.
func BranchStatus(dir string) (branch, state string) {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return "", ""
	}
	state = "clean"
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "??") {
			state = "untracked"
			break
		}
		if line != "" {
			state = "modified"
		}
	}
	// symbolic-ref works on an unborn branch (fresh init); it fails on a
	// detached HEAD, where we leave the branch empty.
	if b, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "-q", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(b))
	}
	return branch, state
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
