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

// WebURL converts a git remote URL to a browsable https URL. Only GitHub and
// GitHub Enterprise remotes (hostname containing "github") are linked; other
// hosts return "".
func WebURL(remote string) string {
	host, path := splitRemote(remote)
	if host == "" || !strings.Contains(strings.ToLower(host), "github") {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if path == "" {
		return ""
	}
	return "https://" + host + "/" + path
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
