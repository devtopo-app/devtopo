package summary

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func argsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func argsAny(args []string, pred func(string) bool) bool {
	for _, a := range args {
		if pred(a) {
			return true
		}
	}
	return false
}

func TestClaudeCLINarrateParsesResult(t *testing.T) {
	// If the stripping works, these must never reach the child env.
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-be-stripped")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "tok-should-be-stripped")

	c := &ClaudeCLI{Bin: "claude", Model: "haiku"}
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		for _, want := range []string{"-p", "--output-format", "json", "--tools", "--safe-mode", "--disallowedTools", "--no-session-persistence"} {
			if !argsContain(args, want) {
				t.Fatalf("args missing %q: %v", want, args)
			}
		}
		if !argsAny(args, func(a string) bool { return strings.Contains(a, "mcp__*") }) {
			t.Fatalf("args must deny mcp tools: %v", args)
		}
		if stdin != "the prompt" {
			t.Fatalf("stdin: %q", stdin)
		}
		for _, e := range env {
			if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") || strings.HasPrefix(e, "ANTHROPIC_AUTH_TOKEN=") {
				t.Fatalf("child env must not carry API-key vars: %q", e)
			}
		}
		return []byte(`{"result":"did X"}`), nil
	}
	got, err := c.Narrate(context.Background(), "the prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "did X" {
		t.Fatalf("narrate result: %q", got)
	}
}

// Codex runs headless, leaves no session file behind (DevTopo would read it
// as a session), cannot touch the disk, and skips the user's own config.
func TestCodexCLINarrateReturnsTheLastMessage(t *testing.T) {
	c := &CodexCLI{Bin: "codex"}
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		for _, want := range []string{"exec", "--ephemeral", "--skip-git-repo-check", "--ignore-user-config", "--ignore-rules", "read-only", "features.shell_tool=false", "features.computer_use=false", "-"} {
			if !argsContain(args, want) {
				t.Fatalf("args missing %q: %v", want, args)
			}
		}
		if stdin != "the prompt" {
			t.Fatalf("stdin: %q", stdin)
		}
		return []byte("- Fixed X.\n- Added Y.\n"), nil
	}
	got, err := c.Narrate(context.Background(), "the prompt")
	if err != nil || got != "- Fixed X.\n- Added Y." {
		t.Fatalf("narrate: %q %v", got, err)
	}
}

// The model is named, not left to Codex's default, which can change under us.
func TestCodexCLIUsesGPT6Luna(t *testing.T) {
	c := NewCodexCLI()
	var got []string
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		got = args
		return []byte("- x"), nil
	}
	if _, err := c.Narrate(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	for i, a := range got {
		if a == "-m" && i+1 < len(got) && got[i+1] == "gpt-6-luna" {
			return
		}
	}
	t.Fatalf("args must name the model: %v", got)
}

func TestClaudeCLINarrateSurfacesError(t *testing.T) {
	c := &ClaudeCLI{Bin: "claude", Model: "haiku"}
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		return nil, fmt.Errorf("boom")
	}
	got, err := c.Narrate(context.Background(), "p")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got != "" {
		t.Fatalf("result must be empty on error, got %q", got)
	}
}

func TestClaudeCLINarrateAppliesTimeout(t *testing.T) {
	c := &ClaudeCLI{Bin: "claude", Model: "haiku", NarrateTimeout: 20 * time.Millisecond}
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		<-ctx.Done() // a hung child: blocks until the deadline fires
		return nil, ctx.Err()
	}
	start := time.Now()
	if _, err := c.Narrate(context.Background(), "p"); err == nil {
		t.Fatal("a hung narrate must time out with an error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("narrate deadline not enforced: took %s", elapsed)
	}
}

func TestClaudeCLINarrateHonorsContext(t *testing.T) {
	c := &ClaudeCLI{Bin: "claude", Model: "haiku"}
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		return nil, ctx.Err() // a cancelled ctx surfaces as the run error
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Narrate(ctx, "p"); err == nil {
		t.Fatal("cancelled context must surface an error")
	}
}

func TestNarrateReturnsAnErrorWhenClaudeReportsIsError(t *testing.T) {
	c := &ClaudeCLI{Bin: "claude", Model: "haiku"}
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		return []byte(`{"is_error":true,"result":"Claude usage limit reached"}`), nil
	}
	_, err := c.Narrate(context.Background(), "p")
	if err == nil || !strings.Contains(err.Error(), "usage limit reached") {
		t.Fatalf("err: %v", err)
	}
}

func TestExecRunHelperChild(t *testing.T) {
	if os.Getenv("DEVTOPO_TEST_CHILD") != "1" {
		t.Skip("helper child only")
	}
	fmt.Print("usage limit reached")
	os.Exit(1)
}

func TestExecRunIncludesStdoutWhenStderrIsEmpty(t *testing.T) {
	env := append(os.Environ(), "DEVTOPO_TEST_CHILD=1")
	_, err := execRun(context.Background(), os.Args[0], []string{"-test.run=^TestExecRunHelperChild$"}, "", env)
	if err == nil || !strings.Contains(err.Error(), "usage limit reached") {
		t.Fatalf("err: %v", err)
	}
}
