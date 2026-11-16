package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/devtopo-app/devtopo/internal/proc"
	"strings"
	"time"
)

// Narrator turns a session's condensed activity prompt into a short narrative.
// It is an interface so the backend is swappable: the default drives the user's
// own claude -p under their subscription; an API-key backend can replace it if
// DevTopo is ever shared (see docs/v1-scope.md).
type Narrator interface {
	Narrate(ctx context.Context, prompt string) (string, error)
}

// ClaudeCLI narrates by shelling out to the local claude binary in headless
// mode. run is injectable so tests never exec a real binary.
type ClaudeCLI struct {
	Bin            string
	Model          string
	NarrateTimeout time.Duration // per-call bound on a claude -p run (0 = none)
	ProbeTimeout   time.Duration // bound on the auth-status probe (0 = none)
	run            func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error)
}

// NewClaudeCLI builds the production narrator: the claude binary on PATH, a
// cheap model, the real subprocess runner, and deadlines so a single hung
// claude call can never stall the whole summarize run.
func NewClaudeCLI() *ClaudeCLI {
	return &ClaudeCLI{
		Bin:            "claude",
		Model:          "haiku",
		NarrateTimeout: 90 * time.Second,
		ProbeTimeout:   10 * time.Second,
		run:            execRun,
	}
}

// narrateArgs is the fixed headless invocation. --safe-mode disables the user's
// global CLAUDE.md/auto-memory, hooks, skills, plugins and MCP servers while
// keeping subscription OAuth working; --tools "" plus --disallowedTools "mcp__*"
// leave no tool for the model to reach for, which is what makes --max-turns 1
// safe for a pure text summary.
func (c *ClaudeCLI) narrateArgs() []string {
	return []string{
		"-p",
		"--model", c.Model,
		"--safe-mode",
		"--tools", "",
		"--disallowedTools", "mcp__*",
		"--no-session-persistence",
		"--output-format", "json",
		"--max-turns", "1",
	}
}

// Narrate runs claude -p with the prompt on stdin and returns the parsed
// result, under NarrateTimeout so a hung child cannot stall the run.
func (c *ClaudeCLI) Narrate(ctx context.Context, prompt string) (string, error) {
	if c.NarrateTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.NarrateTimeout)
		defer cancel()
	}
	out, err := c.run(ctx, c.Bin, c.narrateArgs(), prompt, subscriptionEnv())
	if err != nil {
		return "", err
	}
	var r struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return "", fmt.Errorf("parse claude output: %w", err)
	}
	if r.IsError {
		return "", fmt.Errorf("claude: %s", strings.TrimSpace(r.Result))
	}
	return r.Result, nil
}

// Available reports whether narration can run: claude auth status exits 0 when
// logged in, non-zero (hence a run error) when the binary is missing or logged
// out. Not claude --version, which succeeds even while logged out.
func (c *ClaudeCLI) Available(ctx context.Context) bool {
	if c.ProbeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.ProbeTimeout)
		defer cancel()
	}
	_, err := c.run(ctx, c.Bin, []string{"auth", "status"}, "", subscriptionEnv())
	return err == nil
}

// CodexCLI narrates by shelling out to the local codex binary, so DevTopo
// writes summaries for someone who uses Codex and not Claude.
type CodexCLI struct {
	Bin            string
	Model          string
	NarrateTimeout time.Duration
	ProbeTimeout   time.Duration
	run            func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error)
}

// NewCodexCLI builds the production Codex narrator, with deadlines like Claude's.
func NewCodexCLI() *CodexCLI {
	return &CodexCLI{Bin: "codex", Model: "gpt-6-luna", NarrateTimeout: 120 * time.Second, ProbeTimeout: 10 * time.Second, run: execRun}
}

// codexArgs is the fixed headless invocation. --ephemeral writes no session
// file (DevTopo would read one as a session of the user's), the read-only
// sandbox keeps the model off the disk, and the user's config and rules (their
// MCP servers, model, hooks) are skipped. The prompt comes on stdin ("-");
// stdout carries only the final message.
//
// The transcripts it reads may carry instructions planted in a web page or a
// repository, so the model also gets no tool that could act on them: its
// shell (read-only still lets a command read any file, and put it in the
// summary), computer and browser use, and apps are off. They are set as
// config (-c features.x=false), which an older codex without one of them
// ignores, where --disable would refuse to run.
func (c *CodexCLI) codexArgs() []string {
	args := []string{
		"exec", "--ephemeral", "--skip-git-repo-check",
		"-m", c.Model,
		"--sandbox", "read-only",
		"--ignore-user-config", "--ignore-rules",
	}
	for _, f := range codexToolsOff {
		args = append(args, "-c", "features."+f+"=false")
	}
	return append(args, "--color", "never", "-")
}

// codexToolsOff are the Codex features a summary or a question never needs.
var codexToolsOff = []string{"shell_tool", "computer_use", "browser_use", "browser_use_external", "in_app_browser", "apps"}

// Narrate runs codex exec with the prompt on stdin and returns its final
// message, under NarrateTimeout.
func (c *CodexCLI) Narrate(ctx context.Context, prompt string) (string, error) {
	if c.NarrateTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.NarrateTimeout)
		defer cancel()
	}
	out, err := c.run(ctx, c.Bin, c.codexArgs(), prompt, os.Environ())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Available reports whether codex is installed and logged in.
func (c *CodexCLI) Available(ctx context.Context) bool {
	if c.ProbeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.ProbeTimeout)
		defer cancel()
	}
	_, err := c.run(ctx, c.Bin, []string{"login", "status"}, "", os.Environ())
	return err == nil
}

// ListModels asks codex which models it offers. An error, or output with no
// usable model, is returned as an error so the caller uses its fallback list.
func (c *CodexCLI) ListModels(ctx context.Context) ([]ModelOption, error) {
	if c.ProbeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.ProbeTimeout)
		defer cancel()
	}
	out, err := c.run(ctx, c.Bin, []string{"debug", "models"}, "", os.Environ())
	if err != nil {
		return nil, err
	}
	ms := parseCodexModels(out)
	if len(ms) == 0 {
		return nil, fmt.Errorf("codex debug models: no usable models")
	}
	return ms, nil
}

// forAgent picks the narrator for a session of agent when n chooses per session.
// It is nil when there is no tool to narrate with.
func forAgent(n Narrator, agent string) Narrator {
	if r, ok := n.(interface{ For(string) Narrator }); ok {
		return r.For(agent)
	}
	return n
}

// subscriptionEnv is the current environment with the two API-key variables
// removed, so a stray key is never billed in place of the user's subscription.
func subscriptionEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, e := range src {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") || strings.HasPrefix(e, "ANTHROPIC_AUTH_TOKEN=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// execRun runs the binary with stdin piped in, capturing stdout and stderr in
// separate buffers so diagnostics on stderr never corrupt the JSON on stdout.
// It runs in a dedicated empty temp dir and returns stdout; a non-zero exit
// returns an error carrying the stderr tail.
func execRun(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "devtopo-narrate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	cmd := proc.KillTreeOnCancel(proc.Hidden(exec.CommandContext(ctx, bin, args...)))
	cmd.Dir = dir
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(errb.String())
		if tail == "" {
			// Some CLIs report their failure (a usage limit, say) on stdout.
			tail = strings.TrimSpace(out.String())
		}
		if len(tail) > 500 {
			tail = tail[len(tail)-500:]
		}
		if tail != "" {
			return nil, fmt.Errorf("%s: %w: %s", bin, err, tail)
		}
		return nil, fmt.Errorf("%s: %w", bin, err)
	}
	return out.Bytes(), nil
}
