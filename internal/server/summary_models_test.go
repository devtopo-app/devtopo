package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/summary"
)

// fakeTools is which summary tools this machine has, as a Selector sees them.
type fakeTools struct {
	claude, codex bool
	listCalls     int
	narrateErr    error
}

func (f *fakeTools) attach(t *testing.T, s *Server) *summary.Selector {
	t.Helper()
	sel := summary.NewSelector(s.store.GetSetting, summary.Backends{
		ProbeClaude: func(context.Context) bool { return f.claude },
		ProbeCodex:  func(context.Context) bool { return f.codex },
		ListCodex: func(context.Context) ([]summary.ModelOption, error) {
			f.listCalls++
			return []summary.ModelOption{
				{ID: "gpt-6-luna", Name: "6 Luna"},
				{ID: "gpt-6.1-sol", Name: "6.1 Sol", Multiple: 20},
			}, nil
		},
		NewClaude: func(m string) summary.Narrator { return &fakeNarrator{text: "claude/" + m, err: f.narrateErr} },
		NewCodex:  func(m string) summary.Narrator { return &fakeNarrator{text: "codex/" + m, err: f.narrateErr} },
	})
	sel.Probe(context.Background())
	s.narrator = sel
	s.SetSummaryModels(sel)
	return sel
}

func modelsServer(t *testing.T, f *fakeTools) (*Server, *summary.Selector) {
	t.Helper()
	s := newSeededServer(t, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), nil, nil)
	return s, f.attach(t, s)
}

func modelsView(t *testing.T, rec *httptest.ResponseRecorder) summaryModelsView {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var v summaryModelsView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func putModel(t *testing.T, s *Server, kind, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/summary-models/"+kind, strings.NewReader(body))
	r.Host = testHost
	s.Handler().ServeHTTP(rec, r)
	return rec
}

func TestSummaryModelsViewShowsAvailableTools(t *testing.T) {
	f := &fakeTools{claude: true}
	s, _ := modelsServer(t, f)
	v := modelsView(t, get(t, s, "/api/summary-models"))
	if len(v.Providers) != 2 {
		t.Fatalf("providers: %+v", v.Providers)
	}
	claude, codex := v.Providers[0], v.Providers[1]
	if claude.ID != "claude" || claude.Name != "Claude" || !claude.Available || claude.Cheapest != "haiku" || len(claude.Models) != 4 {
		t.Fatalf("claude: %+v", claude)
	}
	if codex.ID != "codex" || codex.Name != "Codex" || codex.Available || len(codex.Models) != 0 {
		t.Fatalf("codex: %+v", codex)
	}
	if f.listCalls != 0 {
		t.Fatal("must not run codex when it is not available")
	}
}

func TestSummaryModelsDefaultRows(t *testing.T) {
	s, _ := modelsServer(t, &fakeTools{claude: true, codex: true})
	rows := modelsView(t, get(t, s, "/api/summary-models")).Rows
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	if r := rows[0]; r.Kind != "claude" || r.Label != "Claude Code sessions" || r.Provider != "claude" || r.Model != "haiku" || !r.Listed {
		t.Fatalf("claude row: %+v", r)
	}
	if r := rows[1]; r.Kind != "codex" || r.Label != "Codex sessions" || r.Provider != "codex" || r.Model != "gpt-6-luna" || !r.Listed {
		t.Fatalf("codex row: %+v", r)
	}
}

func TestSummaryModelsSaveCrossProvider(t *testing.T) {
	s, sel := modelsServer(t, &fakeTools{claude: true, codex: true})
	v := modelsView(t, putModel(t, s, "codex", `{"provider":"claude","model":"sonnet"}`))
	if r := v.Rows[1]; r.Provider != "claude" || r.Model != "sonnet" || r.EffectiveProvider != "claude" || r.EffectiveModel != "sonnet" {
		t.Fatalf("row: %+v", r)
	}
	got, err := sel.For("codex").Narrate(context.Background(), "p")
	if err != nil || got != "claude/sonnet" {
		t.Fatalf("narrate: %q %v", got, err)
	}
	if v := modelsView(t, get(t, s, "/api/summary-models")); v.Rows[1].Model != "sonnet" {
		t.Fatalf("not stored: %+v", v.Rows[1])
	}
}

func TestSummaryModelsSaveClearsTheRowsError(t *testing.T) {
	f := &fakeTools{claude: true, narrateErr: errors.New("model retired")}
	s, sel := modelsServer(t, f)
	sel.For("claude").Narrate(context.Background(), "p")
	if v := modelsView(t, get(t, s, "/api/summary-models")); v.Rows[0].Error != "model retired" {
		t.Fatalf("error: %+v", v.Rows[0])
	}
	if v := modelsView(t, putModel(t, s, "claude", `{"provider":"claude","model":"sonnet"}`)); v.Rows[0].Error != "" {
		t.Fatalf("a new choice starts clean: %+v", v.Rows[0])
	}
}

func TestSummaryModelsSaveRejects(t *testing.T) {
	s, _ := modelsServer(t, &fakeTools{claude: true})
	for name, tc := range map[string]struct {
		kind, body string
		code       int
	}{
		"unknown kind":         {"gemini", `{"provider":"claude","model":"haiku"}`, http.StatusNotFound},
		"unknown provider":     {"claude", `{"provider":"gemini","model":"haiku"}`, http.StatusBadRequest},
		"unavailable provider": {"claude", `{"provider":"codex","model":"gpt-6.1-sol"}`, http.StatusBadRequest},
		"unlisted model":       {"claude", `{"provider":"claude","model":"made-up"}`, http.StatusBadRequest},
		"a flag":               {"claude", `{"provider":"claude","model":"--flag"}`, http.StatusBadRequest},
		"bad JSON":             {"claude", `{`, http.StatusBadRequest},
	} {
		if rec := putModel(t, s, tc.kind, tc.body); rec.Code != tc.code {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if _, ok, _ := s.store.GetSetting(summary.SettingKey("claude")); ok {
		t.Fatal("a rejected save must store nothing")
	}
}

func TestSummaryModelsSavedModelLeftTheList(t *testing.T) {
	s, sel := modelsServer(t, &fakeTools{codex: true})
	if err := s.store.SetSetting(summary.SettingKey("codex"), "codex/gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	r := modelsView(t, get(t, s, "/api/summary-models")).Rows[1]
	if r.Model != "gpt-5.5" || r.Listed {
		t.Fatalf("row: %+v", r)
	}
	if got, _ := sel.For("codex").Narrate(context.Background(), "p"); got != "codex/gpt-5.5" {
		t.Fatalf("it keeps working until changed: %s", got)
	}
	if rec := putModel(t, s, "codex", `{"provider":"codex","model":"gpt-5.5"}`); rec.Code != http.StatusOK {
		t.Fatalf("re-saving the saved pair: %d %s", rec.Code, rec.Body)
	}
}

func TestSummaryModelsCheapestIsListed(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), nil, nil)
	sel := summary.NewSelector(s.store.GetSetting, summary.Backends{
		ProbeClaude: func(context.Context) bool { return false },
		ProbeCodex:  func(context.Context) bool { return true },
		ListCodex: func(context.Context) ([]summary.ModelOption, error) {
			return []summary.ModelOption{{ID: "gpt-6.1-sol", Name: "Sol"}}, nil
		},
	})
	sel.Probe(context.Background())
	s.SetSummaryModels(sel)
	if c := modelsView(t, get(t, s, "/api/summary-models")).Providers[1].Cheapest; c != "gpt-6.1-sol" {
		t.Fatalf("a provider's reset must land on a model it offers: %q", c)
	}
	if modelsView(t, get(t, s, "/api/summary-models")).Providers[1].CheapestKnown {
		t.Fatal("a model of unknown cost is not known to be the cheapest")
	}
}

func TestSummaryModelsCheapestKnownForTheUsualModel(t *testing.T) {
	s, _ := modelsServer(t, &fakeTools{claude: true, codex: true})
	for _, p := range modelsView(t, get(t, s, "/api/summary-models")).Providers {
		if !p.CheapestKnown {
			t.Fatalf("%s: %+v", p.ID, p)
		}
	}
}

func TestSummaryModelsCheckReprobes(t *testing.T) {
	f := &fakeTools{claude: true}
	s, _ := modelsServer(t, f)
	f.codex = true
	if modelsView(t, get(t, s, "/api/summary-models")).Providers[1].Available {
		t.Fatal("cached until a check")
	}
	if !modelsView(t, post(t, s, "/api/summary-models/check")).Providers[1].Available {
		t.Fatal("a check sees the new tool")
	}
}

func TestSummaryModelsOff(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), nil, nil)
	if v := modelsView(t, get(t, s, "/api/summary-models")); !v.Off {
		t.Fatalf("off: %+v", v)
	}
	if v := modelsView(t, post(t, s, "/api/summary-models/check")); !v.Off {
		t.Fatalf("off check: %+v", v)
	}
	if rec := putModel(t, s, "claude", `{"provider":"claude","model":"haiku"}`); rec.Code != http.StatusConflict {
		t.Fatalf("save while off: %d", rec.Code)
	}
}

func TestSummaryModelsShowsFallbackAndError(t *testing.T) {
	f := &fakeTools{codex: true, narrateErr: errors.New("codex: logged out")}
	s, sel := modelsServer(t, f)
	sel.For("claude").Narrate(context.Background(), "p")
	r := modelsView(t, get(t, s, "/api/summary-models")).Rows[0]
	if !r.Fallback || r.Provider != "claude" || r.EffectiveProvider != "codex" || r.EffectiveModel != "gpt-6-luna" {
		t.Fatalf("fallback: %+v", r)
	}
	if r.Error != "codex: logged out" {
		t.Fatalf("error: %+v", r)
	}
}
