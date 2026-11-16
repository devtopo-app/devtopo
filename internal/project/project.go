// Package project resolves a session's working directory to a stable repo
// identity, so worktrees and parallel sessions of one repo group together.
package project

import (
	"os/exec"

	"github.com/devtopo-app/devtopo/internal/proc"
	"strings"
)

// GitInfo is what a Lookup returns for a directory.
type GitInfo struct {
	RemoteURL string
	Root      string
}

// Lookup resolves a working directory to its git identity.
type Lookup interface {
	Info(cwd string) (GitInfo, error)
}

// Key is the stable identity a project groups by: the normalized origin remote
// if there is one (so worktrees of a repo share it), else the repo root, else
// the working directory itself (a non-git folder is its own project).
func Key(info GitInfo, cwd string) string {
	if n := normalizeRemote(info.RemoteURL); n != "" {
		return n
	}
	if info.Root != "" {
		return info.Root
	}
	return cwd
}

// Name is the display name: the last path segment of a key, splitting on either
// separator so Windows paths work.
func Name(key string) string {
	k := strings.TrimRight(key, `/\`)
	if i := strings.LastIndexAny(k, `/\`); i >= 0 {
		return k[i+1:]
	}
	return k
}

// normalizeRemote reduces a remote URL to host/owner/repo: it drops the scheme
// or the scp-form user@host: prefix, any userinfo, a trailing slash, and a
// trailing .git. An empty or unparseable remote yields "".
func normalizeRemote(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	} else if at := strings.Index(u, "@"); at >= 0 {
		// scp-like form: user@host:owner/repo.git
		u = u[at+1:]
		u = strings.Replace(u, ":", "/", 1)
	}
	// Strip any remaining leading userinfo before the host.
	if slash := strings.Index(u, "/"); slash >= 0 {
		if at := strings.Index(u[:slash], "@"); at >= 0 {
			u = u[at+1:]
		}
	}
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return u
}

// GitLookup is the real Lookup; it shells out to git. A directory that is not a
// repo yields an empty GitInfo (and a nil error), so Key falls back to the cwd.
type GitLookup struct{}

func (GitLookup) Info(cwd string) (GitInfo, error) {
	root, _ := runGit(cwd, "rev-parse", "--show-toplevel")
	remote, _ := runGit(cwd, "remote", "get-url", "origin")
	return GitInfo{RemoteURL: strings.TrimSpace(remote), Root: strings.TrimSpace(root)}, nil
}

func runGit(cwd string, args ...string) (string, error) {
	out, err := proc.Hidden(exec.Command("git", append([]string{"-C", cwd}, args...)...)).Output()
	return string(out), err
}
