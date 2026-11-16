package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/ask"
	"github.com/devtopo-app/devtopo/internal/summary"
)

// maxAskBody bounds a question and the conversation sent with it.
const maxAskBody = 2 << 20

// SetNarrativeOff records that the user switched written summaries off
// (--no-narrative), so the chat says so instead of offering models.
func (s *Server) SetNarrativeOff(off bool) { s.narrativeOff.Store(off) }

func (s *Server) registerAsk(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ask/models", s.handleAskModels)
	mux.HandleFunc("POST /api/ask", s.handleAsk)
}

// defaultAskModels lists the models of the tools available now, so a tool found
// by Check again is offered without a restart.
func (s *Server) defaultAskModels() []summary.Model {
	if s.summaryModels != nil {
		return s.summaryModels.AskModels(context.Background())
	}
	return nil
}

// defaultAskNarrator returns a narrator for a listed model.
func (s *Server) defaultAskNarrator(id string) (summary.Narrator, bool) {
	if s.summaryModels != nil {
		return s.summaryModels.AskNarrator(context.Background(), id)
	}
	return nil, false
}

func (s *Server) handleAskModels(w http.ResponseWriter, r *http.Request) {
	off := s.narrativeOff.Load()
	models := []summary.Model{}
	if !off {
		if ms := s.askModels(); ms != nil {
			models = ms
		}
	}
	s.writeJSON(w, struct {
		Models []summary.Model
		Off    bool
	}{models, off})
}

// askSessions builds today's sessions for a question: those with activity on
// now's local day, with their full stored summary points, not the card's ten.
func (s *Server) askSessions(now time.Time) ([]ask.Session, error) {
	v, err := s.todayView()
	if err != nil {
		return nil, err
	}
	var out []ask.Session
	for _, lane := range []Lane{v.Running, v.Waiting, v.Idle} {
		for _, c := range lane.Cards {
			if !sameLocalDay(c.LastActivity, now) {
				continue
			}
			as := ask.Session{ID: c.ID, Label: firstNonEmpty(c.Name, c.Title, c.Branch, c.Project), Agent: c.Agent}
			se, ok, err := s.store.GetSession(c.ID)
			if err != nil {
				return nil, err
			}
			if ok {
				as.TranscriptPath = se.TranscriptPath
			}
			sum, ok, err := s.store.GetSummary(summary.LevelDay, summary.DayKey(c.ID, now))
			if err != nil {
				return nil, err
			}
			var points []string
			if ok && json.Unmarshal([]byte(sum.Structured), &points) == nil && len(points) > 0 {
				as.Points, as.HasSummary = points, true
				as.Stale = c.LastActivity.After(sum.AsOf)
			}
			out = append(out, as)
		}
	}
	return out, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// handleAsk answers a question about today as a stream of events, one JSON
// object per line. Everything that can be refused is refused before the stream
// starts; after that a failure is an error event.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if s.narrativeOff.Load() || len(s.askModels()) == 0 {
		http.Error(w, "questions are off: no summary tool is available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		ModelID  string
		Question string
		Memory   []ask.Turn
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAskBody))
	if err := dec.Decode(&body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "the question and conversation are too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		http.Error(w, "the question is empty", http.StatusBadRequest)
		return
	}
	n, ok := s.askNarrator(body.ModelID)
	if !ok || n == nil {
		http.Error(w, "unknown model", http.StatusBadRequest)
		return
	}
	// One run at a time, and the lock is held until every call Run started is
	// over, so a new run never overlaps the children of an old one.
	if !s.askMu.TryLock() {
		http.Error(w, "Another question is still running.", http.StatusConflict)
		return
	}
	defer s.askMu.Unlock()

	now := s.clock()
	sessions, err := s.askSessions(now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	flush()
	enc := json.NewEncoder(w)
	ask.Run(r.Context(), n, ask.Request{Question: question, Memory: body.Memory, Sessions: sessions, Day: now}, func(e ask.Event) {
		if enc.Encode(e) == nil { // Encode ends the line
			flush()
		}
	})
}
