package summary

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

const claudeReply = `{"type":"system","subtype":"noise"}
{"type":"control_response","response":{"subtype":"success","request_id":"1","response":{"models":[{"value":"default","displayName":"Default (recommended)","description":"Opus"},{"value":"opus","displayName":"Opus 5.5","description":"For complex work"},{"value":"fable","displayName":"Fable 5.1","description":"For your toughest challenges"},{"value":"haiku","displayName":"Haiku 5.5","description":"Fastest"},{"value":"claude-opus-4-7","displayName":"Opus 4.7","description":"Older"},{"value":"--dangerous","displayName":"Flag","description":"Must not reach a command line"},{"value":"opus[1m]","displayName":"Brackets","description":"Not a plain ID"},{"value":"no-name"}]}}}`

func listingCLI(out string, err error) *ClaudeCLI {
	c := NewClaudeCLI()
	c.run = func(context.Context, string, []string, string, []string) ([]byte, error) {
		return []byte(out), err
	}
	return c
}

// askSelector has both tools working or not, each listing through the given CLI.
func askSelector(t *testing.T, claude *ClaudeCLI, codex []ModelOption) *Selector {
	t.Helper()
	b := Backends{
		ProbeClaude: func(context.Context) bool { return claude != nil },
		ProbeCodex:  func(context.Context) bool { return codex != nil },
		ListCodex:   func(context.Context) ([]ModelOption, error) { return codex, nil },
		NewClaude: func(model string) Narrator {
			c := NewClaudeCLI()
			c.Model = model
			return c
		},
		NewCodex: func(model string) Narrator {
			c := NewCodexCLI()
			c.Model = model
			return c
		},
	}
	if claude != nil {
		b.ListClaude = claude.ListModels
	}
	s := NewSelector(func(string) (string, bool, error) { return "", false, nil }, b)
	s.Probe(context.Background())
	return s
}

func askIDs(ms []Model) []string {
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestListModelsAsksWithoutAPromptOrASavedSession(t *testing.T) {
	c := NewClaudeCLI()
	var gotArgs []string
	var gotStdin string
	var gotEnv []string
	t.Setenv("ANTHROPIC_API_KEY", "stray")
	c.run = func(_ context.Context, _ string, args []string, stdin string, env []string) ([]byte, error) {
		gotArgs, gotStdin, gotEnv = args, stdin, env
		return []byte(claudeReply), nil
	}
	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotStdin != initializeRequest {
		t.Fatalf("stdin = %q, want only the initialize request", gotStdin)
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"--no-session-persistence", "--safe-mode", "--disallowedTools mcp__*", "--input-format stream-json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q lack %q", joined, want)
		}
	}
	if i := slices.Index(gotArgs, "--tools"); i < 0 || gotArgs[i+1] != "" {
		t.Fatalf("args %q must turn tools off", joined)
	}
	for _, e := range gotEnv {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Fatal("a stray API key must not reach the listing")
		}
	}
}

func TestListModelsReadsNamesAndDropsWhatCantBeUsed(t *testing.T) {
	ms, err := listingCLI(claudeReply, nil).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"opus", "fable", "haiku", "claude-opus-4-7", "no-name"}
	if got := modelIDs(ms); !slices.Equal(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if ms[0].Name != "Opus 5.5" || ms[0].Description != "For complex work" || ms[0].PaidCredits {
		t.Fatalf("first = %+v", ms[0])
	}
	if !ms[1].PaidCredits {
		t.Fatalf("Fable can bill to paid credits: %+v", ms[1])
	}
	if ms[4].Name != "no-name" {
		t.Fatalf("a model with no name shows its ID: %+v", ms[4])
	}
}

func TestListModelsFailsOnUnusableReplies(t *testing.T) {
	for name, c := range map[string]*ClaudeCLI{
		"run error":  listingCLI("", errors.New("boom")),
		"not JSON":   listingCLI("not json", nil),
		"no models":  listingCLI(`{"type":"control_response","response":{"subtype":"success","response":{"models":[]}}}`, nil),
		"an error":   listingCLI(`{"type":"control_response","response":{"subtype":"error","response":{"models":[{"value":"opus"}]}}}`, nil),
		"only alias": listingCLI(`{"type":"control_response","response":{"subtype":"success","response":{"models":[{"value":"default"}]}}}`, nil),
	} {
		if ms, err := c.ListModels(context.Background()); err == nil {
			t.Errorf("%s: want an error, got %v", name, ms)
		}
	}
}

func TestParseCodexModelsKeepsTheDescription(t *testing.T) {
	ms := parseCodexModels([]byte(`{"models":[{"slug":"gpt-6-luna","display_name":"Luna","description":"Fast","visibility":"list"}]}`))
	if len(ms) != 1 || ms[0].Description != "Fast" {
		t.Fatalf("models = %+v", ms)
	}
}

func TestAskModelsListsBothToolsAndAcceptsThem(t *testing.T) {
	s := askSelector(t, listingCLI(claudeReply, nil), []ModelOption{{ID: "gpt-6-luna", Name: "Luna", Description: "Fast"}})
	ctx := context.Background()
	ms := s.AskModels(ctx)
	want := []string{"claude:opus", "claude:fable", "claude:haiku", "claude:claude-opus-4-7", "claude:no-name", "codex:gpt-6-luna"}
	if got := askIDs(ms); !slices.Equal(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if ms[1].Description != "For your toughest challenges. "+paidCreditsNote {
		t.Fatalf("Fable's note = %q", ms[1].Description)
	}
	if ms[5].Tool != "codex" || ms[5].Label != "Luna" || ms[5].Description != "Fast" {
		t.Fatalf("codex model = %+v", ms[5])
	}

	n, ok := s.AskNarrator(ctx, "claude:claude-opus-4-7")
	if !ok {
		t.Fatal("a listed Claude model was refused")
	}
	c := n.(*ClaudeCLI)
	if c.Model != "claude-opus-4-7" || c.NarrateTimeout != AskTimeout {
		t.Fatalf("model %q timeout %v", c.Model, c.NarrateTimeout)
	}
	args := strings.Join(c.narrateArgs(), " ")
	for _, want := range []string{"--safe-mode", "--tools", "--no-session-persistence"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q lack %q", args, want)
		}
	}

	n, ok = s.AskNarrator(ctx, "codex:gpt-6-luna")
	if !ok || n.(*CodexCLI).Model != "gpt-6-luna" || n.(*CodexCLI).NarrateTimeout != AskTimeout {
		t.Fatalf("codex narrator = %+v, %v", n, ok)
	}
	for _, id := range []string{"claude:--dangerous", "claude:nope", "codex:nope", "other:opus", "opus", ""} {
		if _, ok := s.AskNarrator(ctx, id); ok {
			t.Fatalf("%q must be refused", id)
		}
	}
}

func TestAskModelsLeaveOutAToolThatIsNotAvailable(t *testing.T) {
	ctx := context.Background()
	s := askSelector(t, nil, []ModelOption{{ID: "gpt-6-luna", Name: "Luna"}})
	if got := askIDs(s.AskModels(ctx)); !slices.Equal(got, []string{"codex:gpt-6-luna"}) {
		t.Fatalf("codex only = %v", got)
	}
	if _, ok := s.AskNarrator(ctx, "claude:haiku"); ok {
		t.Fatal("a model of a missing tool must be refused")
	}
	if got := askSelector(t, nil, nil).AskModels(ctx); len(got) != 0 {
		t.Fatalf("no tools = %v", askIDs(got))
	}
}

func TestAskModelsFallBackWhenClaudeCantSay(t *testing.T) {
	s := askSelector(t, listingCLI("not json", nil), nil)
	got := s.AskModels(context.Background())
	if ids := askIDs(got); !slices.Equal(ids, []string{"claude:haiku", "claude:sonnet", "claude:opus", "claude:fable"}) {
		t.Fatalf("ids = %v", ids)
	}
	if got[3].Description != paidCreditsNote {
		t.Fatalf("the fixed Fable keeps its note: %q", got[3].Description)
	}
}

func TestProbeAsksClaudeForItsListAgain(t *testing.T) {
	calls := 0
	c := listingCLI(claudeReply, nil)
	c.run = func(context.Context, string, []string, string, []string) ([]byte, error) {
		calls++
		return []byte(claudeReply), nil
	}
	s := askSelector(t, c, nil)
	ctx := context.Background()
	s.AskModels(ctx)
	s.AskModels(ctx)
	if calls != 1 {
		t.Fatalf("calls = %d, want the list kept", calls)
	}
	s.Probe(ctx)
	s.AskModels(ctx)
	if calls != 2 {
		t.Fatalf("calls = %d, want Check again to ask afresh", calls)
	}
}

func TestDefaultIsTheCheapestWeCanRecognize(t *testing.T) {
	defaultOf := func(ms []Model) string {
		t.Helper()
		var got []string
		for _, m := range ms {
			if m.Default {
				got = append(got, m.ID)
			}
		}
		if len(got) != 1 {
			t.Fatalf("want exactly one default, got %v", got)
		}
		return got[0]
	}
	ctx := context.Background()
	luna := []ModelOption{{ID: "gpt-6.1-sol", Name: "Sol"}, {ID: "gpt-6-luna", Name: "Luna"}}
	if got := defaultOf(askSelector(t, listingCLI(claudeReply, nil), luna).AskModels(ctx)); got != "claude:haiku" {
		t.Fatalf("listed default = %s", got)
	}
	noHaiku := `{"type":"control_response","response":{"subtype":"success","response":{"models":[{"value":"opus"},{"value":"sonnet"}]}}}`
	if got := defaultOf(askSelector(t, listingCLI(noHaiku, nil), nil).AskModels(ctx)); got != "claude:sonnet" {
		t.Fatalf("must not land on Opus when Sonnet is listed: %s", got)
	}
	if got := defaultOf(askSelector(t, nil, luna).AskModels(ctx)); got != "codex:gpt-6-luna" {
		t.Fatalf("codex alone = %s", got)
	}
}

func TestModelCacheKeepsTheListAndRetriesAFailureSooner(t *testing.T) {
	calls := 0
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	cache := &modelCache{now: func() time.Time { return now }}
	good := []ModelOption{{ID: "good"}}
	var result []ModelOption
	var failure error = errors.New("boom")
	lister := func(context.Context) ([]ModelOption, error) {
		calls++
		return result, failure
	}
	get := func() []string { return modelIDs(cache.get(lister, ClaudeModels)) }

	if got := get(); !slices.Equal(got, modelIDs(ClaudeModels())) {
		t.Fatalf("a failed listing must offer the fixed list: %v", got)
	}
	get()
	if calls != 1 {
		t.Fatalf("calls = %d, want one while the retry wait lasts", calls)
	}

	now = now.Add(modelListRetryTTL + time.Second)
	result, failure = good, nil
	if got := get(); !slices.Equal(got, []string{"good"}) {
		t.Fatalf("the retry should pick up the real list: %v", got)
	}
	get()
	if calls != 2 {
		t.Fatalf("calls = %d, want the good list kept", calls)
	}

	now = now.Add(modelListTTL + time.Second)
	result, failure = nil, errors.New("boom")
	if got := get(); !slices.Equal(got, []string{"good"}) || calls != 3 {
		t.Fatalf("a later failure keeps the last good list: %v, calls %d", got, calls)
	}
}
