package update

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultEvery is how often the background loop asks GitHub.
const DefaultEvery = 12 * time.Hour

// Status is what GET /api/update returns. PublishedAt and CheckedAt are nil
// until known.
type Status struct {
	Current     string
	Latest      string
	Available   bool
	URL         string
	Notes       string
	PublishedAt *time.Time
	CheckedAt   *time.Time
	Error       string
	Enabled     bool
	// Supported is false in a build that names no repository or carries no
	// release version ("dev"): there is nothing to compare, so no check runs.
	Supported bool
	Install   string
	Hint      string
}

// Checker keeps the result of the last check. Its fields are set before use
// (tests replace Fetch, Now and Install); the result is guarded by mu.
type Checker struct {
	Current string
	Repo    string
	// Fetch reads the latest release of a repo; GitHub's API by default.
	Fetch   func(ctx context.Context, repo string) (Release, error)
	Install Install
	Now     func() time.Time
	// StartDelay is the wait before the loop's first check, so it stays out of
	// the way of startup.
	StartDelay time.Duration
	enabled    func() bool

	mu        sync.Mutex
	latest    Release
	checkedAt time.Time
	lastErr   string
	logged    string
}

// NewChecker checks repo (owner/name) for releases newer than current while
// enabled() says so; enabled is read on every pass, so turning the check off
// or on needs no restart. The install method comes from this executable.
func NewChecker(current, repo string, enabled func() bool) *Checker {
	gh := NewGitHub("DevTopo/" + current + " (update check)")
	if enabled == nil {
		enabled = func() bool { return true }
	}
	return &Checker{
		Current:    current,
		Repo:       repo,
		Fetch:      gh.Latest,
		Install:    DetectInstall(executable()),
		Now:        time.Now,
		StartDelay: 30 * time.Second,
		enabled:    enabled,
	}
}

func executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

// Supported reports whether this build can check: it names a repository and
// runs a release version.
func (c *Checker) Supported() bool {
	return c != nil && c.Repo != "" && Valid(c.Current)
}

// Enabled reports whether the user leaves the check on.
func (c *Checker) Enabled() bool {
	return c != nil && c.enabled()
}

// Status is the last check's result, without checking.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{
		Current:   c.Current,
		Enabled:   c.enabled(),
		Supported: c.Supported(),
		Install:   c.Install.Method,
		Hint:      c.Install.Hint,
		Error:     c.lastErr,
	}
	if !st.Supported {
		return st
	}
	if !c.checkedAt.IsZero() {
		t := c.checkedAt
		st.CheckedAt = &t
	}
	if c.latest.Tag != "" {
		st.Latest = c.latest.Tag
		st.Available = Newer(c.latest.Tag, c.Current)
		st.URL = c.latest.URL
		st.Notes = c.latest.Notes
		if !c.latest.PublishedAt.IsZero() {
			t := c.latest.PublishedAt
			st.PublishedAt = &t
		}
	}
	return st
}

// CheckNow asks GitHub now (whether or not the loop is enabled: the user
// asked) and returns the new status. A failed check keeps the last good
// result and records a short error.
func (c *Checker) CheckNow(ctx context.Context) Status {
	if !c.Supported() {
		return c.Status()
	}
	rel, err := c.Fetch(ctx, c.Repo)
	c.mu.Lock()
	c.checkedAt = c.Now()
	if err != nil {
		c.lastErr = shortErr(err)
	} else {
		if !strings.HasPrefix(rel.URL, "https://") {
			rel.URL = "https://github.com/" + c.Repo + "/releases/latest"
		}
		c.latest, c.lastErr = rel, ""
		if Newer(rel.Tag, c.Current) && c.logged != rel.Tag {
			c.logged = rel.Tag
			log.Printf("update available: %s (running %s) - %s", rel.Tag, c.Current, rel.URL)
		}
	}
	c.mu.Unlock()
	return c.Status()
}

func shortErr(err error) string {
	s := err.Error()
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120])
	}
	return s
}

// RunLoop checks after StartDelay and then every interval while enabled, until
// ctx ends. It wakes at least once a minute to read the enabled flag, so a
// check turned back on runs soon, and a check missed while asleep catches up.
// In a build that cannot check it just waits for ctx, since a daemon worker
// that returns stops the daemon.
func (c *Checker) RunLoop(ctx context.Context, every time.Duration) error {
	if every <= 0 {
		every = DefaultEvery
	}
	if !c.Supported() {
		<-ctx.Done()
		return nil
	}
	tick := every
	if tick > time.Minute {
		tick = time.Minute
	}
	var last time.Time
	wait := c.StartDelay
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		wait = tick
		if c.enabled() && (last.IsZero() || time.Since(last) >= every) {
			last = time.Now()
			c.CheckNow(ctx)
		}
	}
}
