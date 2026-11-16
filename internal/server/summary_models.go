package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/devtopo-app/devtopo/internal/summary"
)

type summaryProviderView struct {
	ID        string
	Name      string
	Available bool
	Cheapest  string // the model a change to this provider lands on
	// CheapestKnown is false when Cheapest may not be the lowest cost, so the card warns.
	CheapestKnown bool
	Models        []summary.ModelOption
}

type summaryRowView struct {
	Kind              string
	Label             string
	Provider          string
	Model             string
	Listed            bool // false when the saved model has left its provider's list
	EffectiveProvider string
	EffectiveModel    string
	Fallback          bool
	Error             string
}

// summaryModelsView is the Settings card. Off means --no-narrative.
type summaryModelsView struct {
	Off       bool
	Providers []summaryProviderView
	Rows      []summaryRowView
}

// summaryKindLabels names the session kinds a choice can be made for.
var summaryKindLabels = map[string]string{
	summary.ProviderClaude: "Claude Code sessions",
	summary.ProviderCodex:  "Codex sessions",
}

func (s *Server) registerSummaryModels(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/summary-models", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, s.summaryModelsView(r.Context()))
	})
	// POST checks the tools again, for after the user installed one or logged in.
	mux.HandleFunc("POST /api/summary-models/check", func(w http.ResponseWriter, r *http.Request) {
		if s.summaryModels != nil {
			s.summaryModels.Probe(r.Context())
		}
		s.writeJSON(w, s.summaryModelsView(r.Context()))
	})
	mux.HandleFunc("PUT /api/summary-models/{kind}", s.handleSaveSummaryModel)
}

// summaryProviders lists both providers. An unavailable one has no models, so
// codex is never run while it is not there.
func (s *Server) summaryProviders(ctx context.Context) []summaryProviderView {
	avail := s.summaryModels.Availability()
	out := []summaryProviderView{
		{ID: summary.ProviderClaude, Name: "Claude", Available: avail[summary.ProviderClaude]},
		{ID: summary.ProviderCodex, Name: "Codex", Available: avail[summary.ProviderCodex]},
	}
	for i := range out {
		p := &out[i]
		p.Models = []summary.ModelOption{}
		if !p.Available {
			p.Cheapest, p.CheapestKnown = summary.CheapestListed(p.ID, nil)
			continue
		}
		if p.ID == summary.ProviderClaude {
			p.Models = summary.ClaudeModels()
		} else {
			p.Models = s.summaryModels.CodexModels(ctx)
		}
		p.Cheapest, p.CheapestKnown = summary.CheapestListed(p.ID, p.Models)
	}
	return out
}

func hasModel(ms []summary.ModelOption, id string) bool {
	return slices.ContainsFunc(ms, func(m summary.ModelOption) bool { return m.ID == id })
}

func (s *Server) summaryModelsView(ctx context.Context) summaryModelsView {
	if s.summaryModels == nil {
		return summaryModelsView{Off: true, Providers: []summaryProviderView{}, Rows: []summaryRowView{}}
	}
	providers := s.summaryProviders(ctx)
	rows := []summaryRowView{}
	for _, r := range s.summaryModels.Rows() {
		listed := true // an unavailable tool's list is unknown, so nothing says it left
		for _, p := range providers {
			if p.ID == r.Saved.Provider && p.Available {
				listed = hasModel(p.Models, r.Saved.Model)
			}
		}
		rows = append(rows, summaryRowView{
			Kind:              r.Kind,
			Label:             summaryKindLabels[r.Kind],
			Provider:          r.Saved.Provider,
			Model:             r.Saved.Model,
			Listed:            listed,
			EffectiveProvider: r.Effective.Provider,
			EffectiveModel:    r.Effective.Model,
			Fallback:          r.Fallback,
			Error:             r.LastError,
		})
	}
	return summaryModelsView{Providers: providers, Rows: rows}
}

// handleSaveSummaryModel stores one session kind's choice. It takes only what
// the card offers, or the pair already saved, which stays valid after its model
// leaves the list, so a request cannot put an arbitrary string on a command line.
func (s *Server) handleSaveSummaryModel(w http.ResponseWriter, r *http.Request) {
	if s.summaryModels == nil {
		http.Error(w, "summaries are off", http.StatusConflict)
		return
	}
	kind := r.PathValue("kind")
	if _, ok := summaryKindLabels[kind]; !ok {
		http.Error(w, "unknown session kind", http.StatusNotFound)
		return
	}
	var body struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	choice := summary.Choice{Provider: body.Provider, Model: body.Model}
	if !s.summaryChoiceAllowed(r.Context(), kind, choice) {
		http.Error(w, "that provider and model are not available", http.StatusBadRequest)
		return
	}
	if err := s.store.SetSetting(summary.SettingKey(kind), summary.EncodeChoice(choice)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.summaryModels.ClearError(kind)
	s.writeJSON(w, s.summaryModelsView(r.Context()))
}

func (s *Server) summaryChoiceAllowed(ctx context.Context, kind string, c summary.Choice) bool {
	for _, row := range s.summaryModels.Rows() {
		if row.Kind == kind && row.Saved == c {
			return true
		}
	}
	for _, p := range s.summaryProviders(ctx) {
		if p.ID == c.Provider && p.Available && hasModel(p.Models, c.Model) {
			return true
		}
	}
	return false
}
