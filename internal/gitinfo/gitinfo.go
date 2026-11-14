// Package gitinfo inspects git repositories on the daemon's machine. It owns
// all git introspection so future features (status, commit commands) land here
// rather than in HTTP handlers.
package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gitTimeout bounds every git subprocess so a hung repo on a stale NFS mount
// or slow network filesystem cannot wedge the daemon indefinitely. A var so
// tests can verify timeout behaviour without waiting out the full duration.
var gitTimeout = 10 * time.Second

// gitWaitDelay bounds the wait after the timeout kills git. Killing git does not
// kill its children, and one of them may hold the output pipe open -- git spawns
// an fsmonitor daemon on repos configured for it, and credential helpers outlive
// the parent too. Output only returns once that pipe closes, so without a delay
// the timeout above buys nothing. Also a var so tests need not wait it out.
var gitWaitDelay = time.Second

// gitOutput executes a read-only git command against dir with a bounded timeout.
// --no-optional-locks stops git taking .git/index.lock to write back a refreshed
// index: this package polls every few seconds, and on a large repo that lock is
// held long enough to make a user's concurrent commit or stash fail with
// "index.lock exists".
//
// cost is what the call took out of the machine: the larger of its elapsed time
// and the CPU time git used. The two differ widely -- git stats a big work tree
// on many threads, burning several CPU-seconds per elapsed second, while a hung
// network mount burns none -- and a caller pacing itself needs whichever is
// worse. It is reported for a failed call too.
func gitOutput(dir string, args ...string) (out []byte, cost time.Duration, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir}, args...)...)
	cmd.WaitDelay = gitWaitDelay
	start := time.Now()
	out, err = cmd.Output()
	cost = time.Since(start)
	if ps := cmd.ProcessState; ps != nil {
		cost = max(cost, ps.UserTime()+ps.SystemTime())
	}
	return out, cost, err
}

// RepoWebURL returns the web URL for dir's origin remote, or "" when dir is
// not a git repo, has no origin, or the remote is not GitHub/GHE. git being
// absent is treated the same as no repo.
func RepoWebURL(dir string) string {
	out, _, err := gitOutput(dir, "config", "--get", "remote.origin.url")
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
//
// The duration is what the inspection cost (see gitOutput). git status walks
// the whole work tree, so it scales with the repo: milliseconds for most,
// seconds of CPU for a monorepo. Anything calling this on a timer must pace
// itself by it.
func BranchStatus(dir string) (Status, time.Duration) {
	// --branch adds a "## " header carrying the branch name, the upstream and
	// the ahead/behind counts, so none of them costs an extra git process.
	out, cost, err := gitOutput(dir, "status", "--porcelain", "--branch")
	if err != nil {
		return Status{}, cost
	}
	st := Status{State: "clean"}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "## ") {
			st.Branch, st.Ahead, st.Behind, st.NoUpstream = parseBranchHeader(line)
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
	return st, cost
}

// Speedups are the two git settings that stop status walking the whole work
// tree, which is what makes it slow on a monorepo. As MissingSpeedups returns
// it, a set field is a setting worth turning on.
type Speedups struct {
	// FSMonitor is core.fsmonitor: git keeps a daemon watching the repo, and
	// status asks it what changed instead of stat-ing every tracked file.
	FSMonitor bool
	// UntrackedCache is core.untrackedCache: git remembers each directory's
	// untracked files and skips the directories whose mtime has not moved.
	UntrackedCache bool
}

// Settings lists the set fields as git config keys, each of which wants
// setting to true.
func (s Speedups) Settings() []string {
	var keys []string
	if s.FSMonitor {
		keys = append(keys, "core.fsmonitor")
	}
	if s.UntrackedCache {
		keys = append(keys, "core.untrackedCache")
	}
	return keys
}

// MissingSpeedups reports which of the Speedups dir's repo could turn on but
// has not. It is advice for a repo already known to be slow, so it errs
// towards silence: nothing is reported when the config cannot be read, and
// fsmonitor is left out for a git that does not ship the daemon.
func MissingSpeedups(dir string) Speedups {
	// -z frames each entry as "key\nvalue\0", so a value holding a newline
	// cannot be mistaken for the next key.
	out, _, err := gitOutput(dir, "config", "-z", "--get-regexp", `^(core\.(fsmonitor|untrackedcache)|feature\.manyfiles)$`)
	if err != nil {
		// Exit status 1 is git config's "no such key": none of them is set.
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return Speedups{}
		}
	}
	// git prints every scope's value, lowest precedence first, so the last
	// one seen for a key is the one in effect.
	set := map[string]string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		key, value, hasValue := strings.Cut(entry, "\n")
		if !hasValue {
			value = "true" // a bare key in a config file means true
		}
		set[key] = value
	}

	fsmonitor := false
	if v, ok := set["core.fsmonitor"]; ok {
		// Anything that is not a boolean is the path of a hook, such as
		// Watchman's: fsmonitor by another route.
		on, isBool := gitBool(v)
		fsmonitor = on || !isBool
	}
	// "keep", the default, is not a boolean and so counts as off: it leaves
	// the cache as it finds it, which on an untuned repo means absent.
	// feature.manyFiles turns the cache on only where it is not set itself.
	untracked := false
	if v, ok := set["core.untrackedcache"]; ok {
		untracked, _ = gitBool(v)
	} else if v, ok := set["feature.manyfiles"]; ok {
		untracked, _ = gitBool(v)
	}
	return Speedups{
		FSMonitor:      !fsmonitor && fsmonitorSupported(),
		UntrackedCache: !untracked,
	}
}

// gitBool reads v the way git reads a boolean config value. ok is false when v
// is not one.
func gitBool(v string) (value, ok bool) {
	switch strings.ToLower(v) {
	case "true", "yes", "on":
		return true, true
	case "false", "no", "off", "":
		return false, true
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n != 0, true
	}
	return false, false
}

// fsmonitorSupported reports whether the installed git ships the fsmonitor
// daemon. Asked once: the answer only changes when git is reinstalled. A var
// so tests can stand in for either kind of build.
var fsmonitorSupported = sync.OnceValue(detectFSMonitor)

// detectFSMonitor looks for the daemon among git's compiled-in features.
// Apple's git and Git for Windows have it; most Linux distro builds do not,
// and there core.fsmonitor=true does nothing.
func detectFSMonitor() bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "version", "--build-options")
	cmd.WaitDelay = gitWaitDelay
	out, err := cmd.Output()
	return err == nil && bytes.Contains(out, []byte("feature: fsmonitor--daemon"))
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
// git before 2.15 wrote "Initial commit on" for the unborn form. Branch names
// cannot contain spaces or "..", so neither prefix nor the "..." separator can
// be part of a name.
//
// Only a branch that has commits but no upstream counts as never-pushed: an
// unborn branch has nothing to push, and a detached HEAD is not a branch, so
// its name is empty.
func parseBranchHeader(line string) (branch string, ahead, behind int, noUpstream bool) {
	rest := strings.TrimPrefix(line, "## ")
	if strings.HasPrefix(rest, "HEAD (no branch)") {
		return "", 0, 0, false
	}
	for _, prefix := range []string{"No commits yet on ", "Initial commit on "} {
		if name, ok := strings.CutPrefix(rest, prefix); ok {
			return strings.TrimSpace(name), 0, 0, false
		}
	}
	name, tracking, found := strings.Cut(rest, "...")
	branch = strings.TrimSpace(name)
	if !found {
		// Bare branch name: no tracking branch configured.
		return branch, 0, 0, branch != ""
	}
	open := strings.Index(tracking, " [")
	if open < 0 || !strings.HasSuffix(tracking, "]") {
		return branch, 0, 0, false
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
	return branch, ahead, behind, false
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
