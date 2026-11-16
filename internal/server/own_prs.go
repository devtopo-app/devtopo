package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

// YourPR is one of your open PRs that has a review. ReviewState, Reviewer and
// ReviewedAt describe the review behind its badge.
type YourPR struct {
	Source                  string // the code host: github, gitlab or azure
	RefID, Title, Repo, URL string
	ReviewState, Reviewer   string
	ReviewedAt              time.Time
	Hours                   float64
	Done                    bool
}

// ownPRDone reports whether the PR is marked done and no one has reviewed it since.
func ownPRDone(it store.ExternalItem, marks map[string]time.Time) bool {
	at, ok := marks[it.URL]
	return ok && !it.UpdatedAt.After(at)
}

// ownPRHours is how long an own PR is planned for: PR size says little about
// addressing feedback, and an approved one mostly wants merging.
func ownPRHours(state string) float64 {
	if state == "approved" {
		return 0.25
	}
	return 0.5
}

// yourPRs lists the own PRs in external, which is newest first, that have a review.
func (s *Server) yourPRs(external []store.ExternalItem) ([]YourPR, error) {
	marks, err := s.store.OwnPRsDone()
	if err != nil {
		return nil, err
	}
	out := []YourPR{}
	for _, it := range external {
		if it.Kind == "own_pr" && it.State != "" {
			out = append(out, YourPR{Source: it.Source, RefID: it.ExternalID, Title: it.Title, Repo: it.Repo, URL: it.URL, ReviewState: it.State,
				Reviewer: it.Reviewer, ReviewedAt: it.ReviewedAt, Hours: ownPRHours(it.State), Done: ownPRDone(it, marks)})
		}
	}
	return out, nil
}

// handleOwnPRDone marks one of your PRs done in the app, or not done. It never
// touches the code host.
func (s *Server) handleOwnPRDone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL  string
		Done bool
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.URL) == "" {
		http.Error(w, "a body with a URL is required", http.StatusBadRequest)
		return
	}
	var err error
	if body.Done {
		err = s.store.MarkOwnPRDone(body.URL, s.clock())
	} else {
		err = s.store.UnmarkOwnPRDone(body.URL)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
