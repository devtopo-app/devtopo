// Package usage reads the user's Claude subscription usage limits (the 5-hour
// window, the week, and per-model weeks) from Anthropic's OAuth usage endpoint,
// the one Claude Code's /usage and Orca read. It authenticates with Claude
// Code's own OAuth access token, read from its credentials file (on macOS its
// login Keychain item first) at call time and sent only to api.anthropic.com. The token is never stored, logged or
// returned, and an expired one is never refreshed here: refreshing would
// rewrite Claude Code's credentials. Codex's limits are read from its own
// session rollouts instead (ReadCodex).
package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"

	"github.com/devtopo-app/devtopo/internal/proc"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	endpoint = "https://api.anthropic.com/api/oauth/usage"
	// cacheTTL keeps calls to one per 5 minutes; the endpoint rate limits polling.
	cacheTTL = 5 * time.Minute
)

// Statuses shown instead of the windows when they cannot be read.
const (
	StatusSignedOut   = "Claude Code not signed in"
	StatusExpired     = "sign in to Claude Code again"
	StatusUnavailable = "usage unavailable"
)

// Window is one usage limit: Percent used (0-100) until ResetsAt (zero when
// the API gives none).
type Window struct {
	Label    string
	Percent  float64
	ResetsAt time.Time
}

// Result is the windows, or a Status saying why there are none.
type Result struct {
	Windows []Window
	Status  string
}

// Client fetches and caches the usage. httpDo, run and clock are injectable so
// tests never hit the network or the Keychain.
type Client struct {
	Endpoint        string
	CredentialsPath string
	// Keychain is the login Keychain item Claude Code keeps its credentials in
	// on macOS, read before CredentialsPath; "" for none.
	Keychain string
	httpDo   func(*http.Request) (*http.Response, error)
	run      func(ctx context.Context, bin string, args []string) ([]byte, error)
	clock    func() time.Time

	mu   sync.Mutex
	at   time.Time
	last Result
}

// NewClient builds the production client over Claude Code's credentials file,
// in $CLAUDE_CONFIG_DIR or else ~/.claude, and on macOS its Keychain item.
func NewClient() *Client {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	hc := &http.Client{Timeout: 10 * time.Second}
	c := &Client{Endpoint: endpoint, CredentialsPath: filepath.Join(dir, ".credentials.json"), httpDo: hc.Do, run: execRun, clock: time.Now}
	if runtime.GOOS == "darwin" {
		c.Keychain = "Claude Code-credentials"
	}
	return c
}

// execRun runs a command with a timeout and returns its stdout, which may be a
// secret: neither it nor the command's stderr goes into the error.
func execRun(ctx context.Context, bin string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return proc.KillTreeOnCancel(proc.Hidden(exec.CommandContext(ctx, bin, args...))).Output()
}

// Get returns the usage, fetching it at most once per cacheTTL whatever the
// outcome, so a failing endpoint is not hammered either.
func (c *Client) Get(ctx context.Context) Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && c.clock().Sub(c.at) < cacheTTL {
		return c.last
	}
	c.last, c.at = c.fetch(ctx), c.clock()
	return c.last
}

// credentials reads Claude Code's credentials JSON from the Keychain, else
// from its file.
func (c *Client) credentials(ctx context.Context) ([]byte, error) {
	if c.Keychain != "" {
		if b, err := c.run(ctx, "security", []string{"find-generic-password", "-s", c.Keychain, "-w"}); err == nil {
			return b, nil
		}
	}
	return os.ReadFile(c.CredentialsPath)
}

func (c *Client) fetch(ctx context.Context) Result {
	b, err := c.credentials(ctx)
	if err != nil {
		return Result{Status: StatusSignedOut}
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"` // epoch milliseconds
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &creds) != nil || creds.ClaudeAiOauth.AccessToken == "" {
		return Result{Status: StatusSignedOut}
	}
	if exp := creds.ClaudeAiOauth.ExpiresAt; exp != 0 && !c.clock().Before(time.UnixMilli(exp)) {
		return Result{Status: StatusExpired}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return Result{Status: StatusUnavailable}
	}
	req.Header.Set("Authorization", "Bearer "+creds.ClaudeAiOauth.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := c.httpDo(req)
	if err != nil {
		return Result{Status: StatusUnavailable}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return Result{Status: StatusExpired}
	}
	var body map[string]json.RawMessage
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&body) != nil {
		return Result{Status: StatusUnavailable}
	}
	return Result{Windows: windows(body)}
}

type rawWindow struct {
	Utilization *float64        `json:"utilization"`
	ResetsAt    json.RawMessage `json:"resets_at"`
}

// windows maps the response: five_hour and seven_day, then the per-model weeks,
// from the limits list (weekly_scoped, named by the model's display name) or a
// seven_day_<model> key. Null windows are skipped.
func windows(body map[string]json.RawMessage) []Window {
	var out []Window
	seen := map[string]bool{}
	add := func(label string, pct *float64, reset json.RawMessage) {
		if pct == nil || seen[strings.ToLower(label)] {
			return
		}
		seen[strings.ToLower(label)] = true
		out = append(out, Window{Label: label, Percent: min(100, max(0, *pct)), ResetsAt: resetTime(reset)})
	}
	for _, k := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "week"}} {
		var w rawWindow
		if json.Unmarshal(body[k.key], &w) == nil {
			add(k.label, w.Utilization, w.ResetsAt)
		}
	}

	var limits []struct {
		Kind     string          `json:"kind"`
		Percent  *float64        `json:"percent"`
		ResetsAt json.RawMessage `json:"resets_at"`
		Scope    struct {
			Model struct {
				DisplayName string `json:"display_name"`
			} `json:"model"`
		} `json:"scope"`
	}
	json.Unmarshal(body["limits"], &limits)
	for _, l := range limits {
		if name := strings.TrimSpace(l.Scope.Model.DisplayName); l.Kind == "weekly_scoped" && name != "" {
			add(name, l.Percent, l.ResetsAt)
		}
	}

	var keys []string
	for k := range body {
		// one-word models only: seven_day_oauth_apps is not a model
		if m, ok := strings.CutPrefix(k, "seven_day_"); ok && m != "" && !strings.Contains(m, "_") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		var w rawWindow
		if json.Unmarshal(body[k], &w) == nil {
			m := strings.TrimPrefix(k, "seven_day_")
			add(strings.ToUpper(m[:1])+m[1:], w.Utilization, w.ResetsAt)
		}
	}
	return out
}

// resetTime reads resets_at as an ISO time or as epoch seconds (or
// milliseconds); anything else is the zero time.
func resetTime(raw json.RawMessage) time.Time {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		t, _ := time.Parse(time.RFC3339Nano, s)
		return t
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		if n > 1e10 {
			return time.UnixMilli(int64(n))
		}
		return time.Unix(int64(n), 0)
	}
	return time.Time{}
}
