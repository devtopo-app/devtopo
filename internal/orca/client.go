// Package orca finds the Orca terminal running a Claude Code session and brings
// it to the front, through the user's own orca CLI. Orca does not expose the
// Claude session id, so a session is matched to a terminal by its folder.
package orca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/devtopo-app/devtopo/internal/proc"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/window"
)

// ErrUnavailable means no orca CLI was found on this machine.
var ErrUnavailable = errors.New("orca not available")

// Terminal is one live Orca terminal, mapped from terminal list --json.
type Terminal struct {
	Handle       string
	WorktreePath string
	Branch       string // without refs/heads/
	Agent        string // "claude" for Claude Code, "" for a plain shell
	Title        string
	LastOutputAt time.Time
}

// Client shells out to orca. run and restore are injectable so tests never call
// real orca or touch its window.
type Client struct {
	Bin     string // "" when no CLI was found
	OS      string // runtime.GOOS; on "darwin" Orca is raised by name
	Timeout time.Duration
	run     func(ctx context.Context, bin string, args []string) ([]byte, error)
	restore func() error
}

// NewClient builds the production client: the installed CLI, else orca on
// PATH, else no binary (every call answers ErrUnavailable).
func NewClient() *Client {
	bin := ""
	// where the Orca installer puts its CLI on Windows, and where the app
	// ships it on macOS
	installedBin := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "orca", "resources", "bin", "orca.cmd")
	if runtime.GOOS == "darwin" {
		installedBin = "/Applications/Orca.app/Contents/Resources/bin/orca"
	}
	if _, err := os.Stat(installedBin); err == nil {
		bin = installedBin
	} else if p, err := exec.LookPath("orca"); err == nil {
		bin = p
	}
	return &Client{Bin: bin, OS: runtime.GOOS, Timeout: 10 * time.Second, run: execRun, restore: func() error { return window.RestoreTitle("Orca") }}
}

func (c *Client) exec(ctx context.Context, bin string, args ...string) ([]byte, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	return c.run(ctx, bin, args)
}

// List returns the live terminals.
func (c *Client) List(ctx context.Context) ([]Terminal, error) {
	if c.Bin == "" {
		return nil, ErrUnavailable
	}
	out, err := c.exec(ctx, c.Bin, "terminal", "list", "--json")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Terminals []struct {
				Handle        string `json:"handle"`
				WorktreePath  string `json:"worktreePath"`
				Branch        string `json:"branch"`
				AgentIdentity string `json:"agentIdentity"`
				Title         string `json:"title"`
				LastOutputAt  int64  `json:"lastOutputAt"`
			} `json:"terminals"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse orca json: %w", err)
	}
	ts := make([]Terminal, 0, len(resp.Result.Terminals))
	for _, t := range resp.Result.Terminals {
		ts = append(ts, Terminal{
			Handle: t.Handle, WorktreePath: t.WorktreePath, Branch: strings.TrimPrefix(t.Branch, "refs/heads/"),
			Agent: t.AgentIdentity, Title: t.Title, LastOutputAt: time.UnixMilli(t.LastOutputAt),
		})
	}
	return ts, nil
}

// Switch brings the terminal's tab forward in Orca, restores the Orca window if
// it is minimized, then brings it to the front, since Windows may only flash the
// taskbar otherwise; macOS raises it by name. A failed restore or raise is not
// an error: the tab has switched.
func (c *Client) Switch(ctx context.Context, handle string) error {
	if c.Bin == "" {
		return ErrUnavailable
	}
	if _, err := c.exec(ctx, c.Bin, "terminal", "switch", "--terminal", handle); err != nil {
		return err
	}
	if c.OS == "darwin" {
		c.exec(ctx, "osascript", "-e", `tell application "Orca" to activate`)
		return nil
	}
	if c.restore != nil {
		c.restore()
	}
	c.exec(ctx, "powershell", "-NoProfile", "-Command", "(New-Object -ComObject WScript.Shell).AppActivate('Orca')")
	return nil
}

// Match picks the terminal whose worktree is the session's folder (or holds it).
// A terminal Orca labels with the session's agent ("" means claude) comes first,
// then an unlabelled one, since Orca doesn't always label a terminal; another
// agent's never matches. Among those it prefers the session's branch, then the
// terminal whose last output is closest to the session's last activity. miss says
// why nothing matched, and is "" on a match.
func Match(ts []Terminal, cwd, branch, agent string, lastActivity time.Time) (t Terminal, miss string) {
	if agent == "" {
		agent = "claude"
	}
	dir := cleanPath(cwd)
	var own, plain Terminal
	var foundOwn, foundPlain, inFolder bool
	pick := func(best *Terminal, found *bool, c Terminal) {
		if !*found || better(c, *best, branch, lastActivity) {
			*best, *found = c, true
		}
	}
	for _, c := range ts {
		wt := cleanPath(c.WorktreePath)
		if dir != wt && !strings.HasPrefix(dir, wt+"/") {
			continue
		}
		inFolder = true
		switch c.Agent {
		case agent:
			pick(&own, &foundOwn, c)
		case "":
			pick(&plain, &foundPlain, c)
		}
	}
	switch {
	case foundOwn:
		return own, ""
	case foundPlain:
		return plain, ""
	case inFolder:
		return Terminal{}, fmt.Sprintf("no %s%s terminal in %s", strings.ToUpper(agent[:1]), agent[1:], cwd)
	}
	return Terminal{}, "no Orca terminal in " + cwd
}

func better(a, b Terminal, branch string, at time.Time) bool {
	if (a.Branch == branch) != (b.Branch == branch) {
		return a.Branch == branch
	}
	return absDur(a.LastOutputAt.Sub(at)) < absDur(b.LastOutputAt.Sub(at))
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// cleanPath makes Windows and slash paths comparable, ignoring case on Windows.
func cleanPath(p string) string {
	p = path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// execRun runs a command and returns stdout. The error names the command only;
// orca's own output is never passed on.
func execRun(ctx context.Context, bin string, args []string) ([]byte, error) {
	out, err := proc.KillTreeOnCancel(proc.Hidden(exec.CommandContext(ctx, bin, args...))).Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", path.Base(strings.ReplaceAll(bin, `\`, "/")), args[0], err)
	}
	return out, nil
}
