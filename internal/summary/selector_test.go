package summary

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// namedNarrator answers with the tool and model it was built for, so a test
// sees what would have run without looking inside the Selector.
type namedNarrator struct {
	name string
	err  *error
}

func (n namedNarrator) Narrate(ctx context.Context, prompt string) (string, error) {
	if n.err != nil && *n.err != nil {
		return "", *n.err
	}
	return n.name, nil
}

// fakeTools is the world a Selector sees: which tools work, what is saved.
type fakeTools struct {
	claude, codex bool
	saved         map[string]string
	listCalls     int
	listErr       error
	narrateErr    error
}

func (f *fakeTools) selector(t *testing.T) *Selector {
	t.Helper()
	if f.saved == nil {
		f.saved = map[string]string{}
	}
	s := NewSelector(func(k string) (string, bool, error) {
		v, ok := f.saved[k]
		return v, ok, nil
	}, Backends{
		ProbeClaude: func(context.Context) bool { return f.claude },
		ProbeCodex:  func(context.Context) bool { return f.codex },
		ListCodex: func(context.Context) ([]ModelOption, error) {
			f.listCalls++
			if f.listErr != nil {
				return nil, f.listErr
			}
			return []ModelOption{{ID: "live-model", Name: "Live"}}, nil
		},
		NewClaude: func(m string) Narrator { return namedNarrator{"claude/" + m, &f.narrateErr} },
		NewCodex:  func(m string) Narrator { return namedNarrator{"codex/" + m, &f.narrateErr} },
	})
	s.Probe(context.Background())
	return s
}

func ran(t *testing.T, s *Selector, agent string) string {
	t.Helper()
	n := s.For(agent)
	if n == nil {
		t.Fatalf("no narrator for %q", agent)
	}
	got, err := n.Narrate(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSelectorDefaultsToOwnToolAndCheapest(t *testing.T) {
	s := (&fakeTools{claude: true, codex: true}).selector(t)
	if got := ran(t, s, "claude"); got != "claude/haiku" {
		t.Fatalf("claude session: %s", got)
	}
	if got := ran(t, s, "codex"); got != "codex/gpt-6-luna" {
		t.Fatalf("codex session: %s", got)
	}
}

func TestSelectorEmptyAgentIsClaudeKind(t *testing.T) {
	f := &fakeTools{claude: true, codex: true, saved: map[string]string{SettingKey("claude"): "claude/opus"}}
	if got := ran(t, f.selector(t), ""); got != "claude/opus" {
		t.Fatalf("empty agent: %s", got)
	}
}

func TestSelectorUsesClaudeForCodexSessions(t *testing.T) {
	f := &fakeTools{claude: true, codex: true, saved: map[string]string{
		SettingKey("codex"): EncodeChoice(Choice{Provider: ProviderClaude, Model: "sonnet"}),
	}}
	s := f.selector(t)
	if got := ran(t, s, "codex"); got != "claude/sonnet" {
		t.Fatalf("codex session: %s", got)
	}
	if got := ran(t, s, "claude"); got != "claude/haiku" {
		t.Fatalf("claude session is unaffected: %s", got)
	}
}

func TestSelectorChangeAppliesWithoutRestart(t *testing.T) {
	f := &fakeTools{claude: true}
	s := f.selector(t)
	if got := ran(t, s, "claude"); got != "claude/haiku" {
		t.Fatal(got)
	}
	f.saved[SettingKey("claude")] = "claude/opus"
	if got := ran(t, s, "claude"); got != "claude/opus" {
		t.Fatalf("after a change: %s", got)
	}
}

func TestSelectorFallsBackToOtherToolsCheapest(t *testing.T) {
	f := &fakeTools{codex: true, saved: map[string]string{SettingKey("claude"): "claude/opus"}}
	s := f.selector(t)
	if got := ran(t, s, "claude"); got != "codex/gpt-6-luna" {
		t.Fatalf("fallback: %s", got)
	}
	row := s.Rows()[0]
	if row.Kind != "claude" || !row.Fallback || row.Saved.Model != "opus" || row.Effective != (Choice{ProviderCodex, "gpt-6-luna"}) {
		t.Fatalf("row: %+v", row)
	}
}

func TestSelectorRowsAreClaudeThenCodexWithoutFallback(t *testing.T) {
	rows := (&fakeTools{claude: true, codex: true}).selector(t).Rows()
	if len(rows) != 2 || rows[0].Kind != "claude" || rows[1].Kind != "codex" || rows[0].Fallback || rows[1].Fallback {
		t.Fatalf("rows: %+v", rows)
	}
}

func TestSelectorNoToolsIsNil(t *testing.T) {
	s := (&fakeTools{}).selector(t)
	if s.For("claude") != nil || s.For("codex") != nil {
		t.Fatal("no tools means no narrator")
	}
	if _, err := s.Narrate(context.Background(), "p"); !errors.Is(err, ErrNoNarrator) {
		t.Fatalf("narrate: %v", err)
	}
	if Ready(s) || Ready(nil) {
		t.Fatal("not ready without a tool")
	}
	if !Ready((&fakeTools{codex: true}).selector(t)) || !Ready(&fakeNarrator{}) {
		t.Fatal("ready with a tool")
	}
	if row := s.Rows()[0]; row.Effective != (Choice{}) || row.Fallback {
		t.Fatalf("row with no tool: %+v", row)
	}
}

func TestSelectorIgnoresBadSavedValues(t *testing.T) {
	for _, bad := range []string{"", "garbage", "gemini/x", "claude/", "claude/-rm", "codex/a b", "/haiku"} {
		f := &fakeTools{claude: true, codex: true, saved: map[string]string{SettingKey("claude"): bad}}
		if got := ran(t, f.selector(t), "claude"); got != "claude/haiku" {
			t.Fatalf("%q: %s", bad, got)
		}
	}
}

func TestSelectorRecordsAndClearsErrors(t *testing.T) {
	f := &fakeTools{claude: true, codex: true}
	s := f.selector(t)
	f.narrateErr = errors.New("claude: logged out")
	if _, err := s.For("claude").Narrate(context.Background(), "p"); err == nil {
		t.Fatal("the error still reaches the caller")
	}
	rows := s.Rows()
	if rows[0].LastError != "claude: logged out" || rows[1].LastError != "" {
		t.Fatalf("rows: %+v", rows)
	}
	f.narrateErr = nil
	ran(t, s, "claude")
	if got := s.Rows()[0].LastError; got != "" {
		t.Fatalf("a success clears it: %q", got)
	}
}

// Closing a detail or quitting the app is not a failed summary, but a timeout is.
func TestSelectorIgnoresCancelledNarrate(t *testing.T) {
	f := &fakeTools{claude: true}
	s := f.selector(t)
	f.narrateErr = context.Canceled
	s.For("claude").Narrate(context.Background(), "p")
	if got := s.Rows()[0].LastError; got != "" {
		t.Fatalf("cancel error: %q", got)
	}
	// A killed child reports its own exit error, not the context's.
	f.narrateErr = errors.New("claude: signal: killed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.For("claude").Narrate(ctx, "p")
	if got := s.Rows()[0].LastError; got != "" {
		t.Fatalf("cancelled context: %q", got)
	}
	f.narrateErr = context.DeadlineExceeded
	s.For("claude").Narrate(context.Background(), "p")
	if s.Rows()[0].LastError == "" {
		t.Fatal("a timeout is a failure")
	}
}

func TestSelectorClearError(t *testing.T) {
	f := &fakeTools{claude: true}
	s := f.selector(t)
	f.narrateErr = errors.New("model retired")
	s.For("claude").Narrate(context.Background(), "p")
	s.ClearError("claude")
	if got := s.Rows()[0].LastError; got != "" {
		t.Fatalf("a new choice starts clean: %q", got)
	}
}

func TestSelectorProbeSeesNewTool(t *testing.T) {
	f := &fakeTools{claude: true}
	s := f.selector(t)
	if got := ran(t, s, "codex"); got != "claude/haiku" {
		t.Fatal(got)
	}
	f.codex = true
	if got := ran(t, s, "codex"); got != "claude/haiku" {
		t.Fatalf("availability is cached until a probe: %s", got)
	}
	s.Probe(context.Background())
	if got := ran(t, s, "codex"); got != "codex/gpt-6-luna" {
		t.Fatalf("after a probe: %s", got)
	}
	if a := s.Availability(); !a[ProviderClaude] || !a[ProviderCodex] {
		t.Fatalf("availability: %v", a)
	}
}

func TestSelectorProbeClearsErrors(t *testing.T) {
	f := &fakeTools{claude: true}
	s := f.selector(t)
	f.narrateErr = errors.New("logged out")
	s.For("claude").Narrate(context.Background(), "p")
	s.Probe(context.Background())
	if got := s.Rows()[0].LastError; got != "" {
		t.Fatalf("a new check starts clean: %q", got)
	}
}

// A check cut short (the tab closed) says nothing about the tools.
func TestSelectorCancelledProbeKeepsAvailability(t *testing.T) {
	f := &fakeTools{claude: true}
	s := f.selector(t)
	f.claude = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Probe(ctx)
	if !s.Availability()[ProviderClaude] {
		t.Fatal("a cancelled probe must not change availability")
	}
}

func TestSelectorCodexModelsCachedAndFallsBack(t *testing.T) {
	f := &fakeTools{codex: true}
	s := f.selector(t)
	ctx := context.Background()
	s.CodexModels(ctx)
	if got := modelIDs(s.CodexModels(ctx)); !slices.Equal(got, []string{"live-model"}) {
		t.Fatalf("live: %v", got)
	}
	if f.listCalls != 1 {
		t.Fatalf("cached: %d calls", f.listCalls)
	}
	f.listErr = errors.New("unknown command")
	s.Probe(ctx)
	if got := modelIDs(s.CodexModels(ctx)); !slices.Equal(got, modelIDs(FallbackCodexModels())) {
		t.Fatalf("fallback: %v", got)
	}
	if f.listCalls != 2 {
		t.Fatalf("a probe refreshes: %d calls", f.listCalls)
	}
}

func TestSelectorCancelledListIsNotCached(t *testing.T) {
	f := &fakeTools{codex: true, listErr: context.Canceled}
	s := f.selector(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.CodexModels(ctx)
	f.listErr = nil
	if got := modelIDs(s.CodexModels(context.Background())); !slices.Equal(got, []string{"live-model"}) {
		t.Fatalf("after a cancelled list: %v", got)
	}
}

func TestDefaultBackendsNameTheChosenModel(t *testing.T) {
	b := DefaultBackends()
	if c, ok := b.NewClaude("opus").(*ClaudeCLI); !ok || c.Model != "opus" {
		t.Fatalf("claude: %+v", c)
	}
	if c, ok := b.NewCodex("gpt-6.1-sol").(*CodexCLI); !ok || c.Model != "gpt-6.1-sol" {
		t.Fatalf("codex: %+v", c)
	}
}
