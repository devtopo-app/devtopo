package summary

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func modelIDs(ms []ModelOption) []string {
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	return ids
}

func TestClaudeModelsAreTheFixedAliases(t *testing.T) {
	ms := ClaudeModels()
	if got := modelIDs(ms); !slices.Equal(got, []string{"haiku", "sonnet", "opus", "fable"}) {
		t.Fatalf("ids: %v", got)
	}
	for _, m := range ms {
		if m.PaidCredits != (m.ID == "fable") {
			t.Fatalf("only fable can bill to paid credits: %+v", m)
		}
	}
}

func TestFallbackCodexModels(t *testing.T) {
	want := map[string]int{"gpt-6-luna": 0, "gpt-6.1-terra": 10, "gpt-6.1-sol": 20, "gpt-6-astra": 100}
	ms := FallbackCodexModels()
	if got := modelIDs(ms); !slices.Equal(got, []string{"gpt-6-luna", "gpt-6.1-terra", "gpt-6.1-sol", "gpt-6-astra"}) {
		t.Fatalf("ids: %v", got)
	}
	for _, m := range ms {
		if m.Multiple != want[m.ID] {
			t.Fatalf("%s multiple: %d", m.ID, m.Multiple)
		}
	}
}

func TestCheapestModel(t *testing.T) {
	if CheapestModel(ProviderClaude) != "haiku" || CheapestModel(ProviderCodex) != "gpt-6-luna" {
		t.Fatalf("cheapest: %q %q", CheapestModel(ProviderClaude), CheapestModel(ProviderCodex))
	}
}

// A switch of provider must not land on a costly model because the usual cheapest
// left the list: known costs decide, and an unknown cost is flagged as unknown.
func TestCheapestListed(t *testing.T) {
	for name, tc := range map[string]struct {
		models []ModelOption
		id     string
		known  bool
	}{
		"usual cheapest listed": {FallbackCodexModels(), "gpt-6-luna", true},
		"lowest known multiple": {[]ModelOption{{ID: "gpt-6-astra", Multiple: 100}, {ID: "gpt-6.1-terra", Multiple: 10}, {ID: "gpt-6.1-sol", Multiple: 20}}, "gpt-6.1-terra", false},
		"known beats unknown":   {[]ModelOption{{ID: "brand-new"}, {ID: "gpt-6.1-sol", Multiple: 20}}, "gpt-6.1-sol", false},
		"all unknown":           {[]ModelOption{{ID: "brand-new"}, {ID: "other"}}, "brand-new", false},
		"empty list":            {nil, "gpt-6-luna", false},
	} {
		id, known := CheapestListed(ProviderCodex, tc.models)
		if id != tc.id || known != tc.known {
			t.Fatalf("%s: %q %v", name, id, known)
		}
	}
}

func TestParseCodexModelsKeepsListedSortedByPriority(t *testing.T) {
	ms := parseCodexModels([]byte(`{"models":[
		{"slug":"gpt-6.1-sol","display_name":"6.1 Sol","visibility":"list","priority":2},
		{"slug":"gpt-6-luna","display_name":"6 Luna","visibility":"list","priority":1},
		{"slug":"secret","display_name":"x","visibility":"hide","priority":0}]}`))
	if got := modelIDs(ms); !slices.Equal(got, []string{"gpt-6-luna", "gpt-6.1-sol"}) {
		t.Fatalf("ids: %v", got)
	}
	if ms[0].Name != "6 Luna" || ms[1].Name != "6.1 Sol" {
		t.Fatalf("names: %+v", ms)
	}
	if ms[1].Multiple != 20 {
		t.Fatalf("a known model carries its multiple: %+v", ms[1])
	}
}

// A slug goes on the command line, so one that could read as a flag is dropped.
func TestParseCodexModelsDropsUnsafeSlugs(t *testing.T) {
	ms := parseCodexModels([]byte(`{"models":[
		{"slug":"-x","display_name":"a","visibility":"list"},
		{"slug":"a b","display_name":"b","visibility":"list"},
		{"slug":"","display_name":"c","visibility":"list"},
		{"slug":"ok-1.2","display_name":"d","visibility":"list"}]}`))
	if got := modelIDs(ms); !slices.Equal(got, []string{"ok-1.2"}) {
		t.Fatalf("ids: %v", got)
	}
}

func TestParseCodexModelsEmptyNameUsesSlug(t *testing.T) {
	ms := parseCodexModels([]byte(`{"models":[{"slug":"gpt-x","display_name":"","visibility":"list"}]}`))
	if len(ms) != 1 || ms[0].Name != "gpt-x" {
		t.Fatalf("models: %+v", ms)
	}
}

func TestParseCodexModelsBadInputIsEmpty(t *testing.T) {
	for _, in := range []string{
		"not json",
		"",
		`{"models":[]}`,
		`{"models":[{"slug":"x","display_name":"x","visibility":"hide"}]}`,
	} {
		if ms := parseCodexModels([]byte(in)); len(ms) != 0 {
			t.Fatalf("%q: %+v", in, ms)
		}
	}
}

func TestListModelsRunsDebugModels(t *testing.T) {
	c := NewCodexCLI()
	var got []string
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		got = args
		return []byte(`{"models":[{"slug":"gpt-6-luna","display_name":"6 Luna","visibility":"list"}]}`), nil
	}
	ms, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"debug", "models"}) {
		t.Fatalf("args: %v", got)
	}
	if len(ms) != 1 || ms[0].ID != "gpt-6-luna" {
		t.Fatalf("models: %+v", ms)
	}
}

func TestListModelsErrorsWhenNothingUsable(t *testing.T) {
	c := NewCodexCLI()
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		return nil, errors.New("unknown command")
	}
	if _, err := c.ListModels(context.Background()); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("run error: %v", err)
	}
	c.run = func(ctx context.Context, bin string, args []string, stdin string, env []string) ([]byte, error) {
		return []byte("not json"), nil
	}
	if _, err := c.ListModels(context.Background()); err == nil {
		t.Fatal("unusable output must be an error, so the caller uses its fallback")
	}
}
