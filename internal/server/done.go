package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/devtopo-app/devtopo/internal/store"
)

// handleItemDone marks an own activity or a review item done (done_at = now) or
// not done. A ticket's done state lives in its tracker, so a ticket item is
// sent to /api/tickets/done.
func (s *Server) handleItemDone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Done *bool `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Done == nil {
		http.Error(w, "a body with done true or false is required", http.StatusBadRequest)
		return
	}
	it, ok, err := s.store.GetPlanItem(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if it.RefSource == "linear" || it.RefSource == "jira" {
		http.Error(w, "use /api/tickets/done", http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(it.RefID, store.OwnPRPrefix) {
		http.Error(w, "use /api/own-prs/done", http.StatusBadRequest)
		return
	}
	it.DoneAt = nil
	if *body.Done {
		now := s.clock()
		it.DoneAt = &now
	}
	if err := s.store.SetPlanItemDone(it.ID, it.DoneAt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, it)
}

// handleTicketDone moves a Linear or Jira ticket to done (or back to started)
// in its tracker, then re-syncs that source so the plans, the plannable list
// and today show the new state at once. The credentials are read from the
// store and never logged or returned; a refused move answers a short 502
// without the upstream detail.
func (s *Server) handleTicketDone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		ID     string `json:"id"`
		Done   *bool  `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" || body.Done == nil {
		http.Error(w, "source, id and done are required", http.StatusBadRequest)
		return
	}
	var (
		state string
		err   error
	)
	switch body.Source {
	case "linear":
		key, ok, gerr := s.store.GetSetting("linear_api_key")
		if gerr != nil {
			http.Error(w, gerr.Error(), http.StatusInternalServerError)
			return
		}
		if !ok || key == "" {
			http.Error(w, "linear is not configured", http.StatusBadRequest)
			return
		}
		stateType := "started"
		if *body.Done {
			stateType = "completed"
		}
		state, err = s.moveLinear(r.Context(), key, body.ID, stateType)
	case "jira":
		creds := map[string]string{}
		for _, k := range []string{"jira_site", "jira_email", "jira_api_token"} {
			v, ok, gerr := s.store.GetSetting(k)
			if gerr != nil {
				http.Error(w, gerr.Error(), http.StatusInternalServerError)
				return
			}
			if !ok || v == "" {
				http.Error(w, "jira is not configured", http.StatusBadRequest)
				return
			}
			creds[k] = v
		}
		category := "indeterminate"
		if *body.Done {
			category = "done"
		}
		state, err = s.moveJira(r.Context(), creds["jira_site"], creds["jira_email"], creds["jira_api_token"], body.ID, category)
	default:
		http.Error(w, "source must be linear or jira", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, body.Source+" refused the move", http.StatusBadGateway)
		return
	}
	// The move happened; a failed re-sync is caught up by the background loop.
	sync := s.syncLinear
	if body.Source == "jira" {
		sync = s.syncJira
	}
	if sync != nil {
		_, _ = sync(r.Context())
	}
	s.writeJSON(w, struct{ State string }{state})
}
