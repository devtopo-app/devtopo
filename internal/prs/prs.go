// Package prs is what the code-hosting connectors (GitHub, GitLab, Azure DevOps)
// share: the shape of a pull request, how reviews become its state, and how a
// source's PRs are stored. Each connector only knows how to read its own API.
package prs

import (
	"slices"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

// Item is one pull request, or merge request, from a code host.
type Item struct {
	Kind                string // "review_request" | "pr" | "review_done" | "own_pr"
	ExternalID          string // the PR url - stable across polls; see the prefixes below
	Title               string
	State               string // an own_pr's is its review state, "" with no review
	URL                 string
	Repo                string // owner/name
	ReviewRequestedOfMe bool
	UpdatedAt           time.Time
	// Additions and Deletions leave generated files out; ChangedFiles over 0
	// means the size is known. RequestedAt is your latest own review request.
	Additions, Deletions, ChangedFiles int
	RequestedAt                        time.Time
	// Reviewer and ReviewedAt are the review behind an own_pr's State.
	Reviewer   string
	ReviewedAt time.Time
}

// The review states of an own PR.
const (
	Approved         = "approved"
	Commented        = "commented"
	ChangesRequested = "changes_requested"
)

// ReviewDonePrefix starts the id of a review you submitted, before the PR's URL,
// so it never clashes with a pending review request for the same PR.
const ReviewDonePrefix = "review:"

// IsCodeHost reports whether a source holds PRs, not tickets.
func IsCodeHost(source string) bool {
	return source == "github" || source == "gitlab" || source == "azure"
}

// Review is one person's latest word on a PR.
type Review struct {
	Reviewer string
	State    string // one of the review states above
	At       time.Time
}

var urgency = map[string]int{Approved: 1, Commented: 2, ChangesRequested: 3}

// ApplyReviews sets an own PR's state from each reviewer's latest word: the most
// urgent one, the latest breaking a tie. UpdatedAt becomes when anyone last
// reviewed it. With no review, the item stays as it is.
func (it *Item) ApplyReviews(reviews []Review) {
	var newest time.Time
	for _, r := range reviews {
		u := urgency[r.State]
		if u == 0 {
			continue
		}
		if r.At.After(newest) {
			newest = r.At
		}
		shown := urgency[it.State]
		if u > shown || (u == shown && r.At.After(it.ReviewedAt)) {
			it.State, it.Reviewer, it.ReviewedAt = r.State, r.Reviewer, r.At
		}
	}
	if !newest.IsZero() {
		it.UpdatedAt = newest
	}
}

// generatedDirs and generatedFiles are what a review doesn't read line by line.
var (
	generatedDirs     = []string{"dist", "ui_dist", "vendor", "node_modules"}
	generatedFiles    = []string{"package-lock.json", "pnpm-lock.yaml", "go.sum"}
	generatedSuffixes = []string{".lock", ".min.js", ".min.css", ".map"}
)

// IsGenerated reports whether a PR's file is built output or a lockfile, by its
// path, so it doesn't count toward the PR's size.
func IsGenerated(file string) bool {
	parts := strings.Split(strings.ToLower(file), "/")
	base := parts[len(parts)-1]
	for _, dir := range parts[:len(parts)-1] {
		if slices.Contains(generatedDirs, dir) {
			return true
		}
	}
	if slices.Contains(generatedFiles, base) {
		return true
	}
	return slices.ContainsFunc(generatedSuffixes, func(s string) bool { return strings.HasSuffix(base, s) })
}

// Store replaces a source's external items with groups, then forgets the marks of
// PRs no source lists any more. Returns the count stored. Call it only after every
// fetch of a sync worked, so a transient failure never wipes the last good items.
func Store(st *store.Store, source string, groups ...[]Item) (int, error) {
	all := []store.ExternalItem{}
	for _, group := range groups {
		for _, it := range group {
			all = append(all, store.ExternalItem{
				Source: source, ExternalID: it.ExternalID, Kind: it.Kind, Title: it.Title,
				State: it.State, URL: it.URL, Repo: it.Repo,
				ReviewRequestedOfMe: it.ReviewRequestedOfMe, UpdatedAt: it.UpdatedAt,
				Additions: it.Additions, Deletions: it.Deletions, ChangedFiles: it.ChangedFiles, RequestedAt: it.RequestedAt,
				Reviewer: it.Reviewer, ReviewedAt: it.ReviewedAt,
			})
		}
	}
	if err := st.ReplaceExternalItems(source, all); err != nil {
		return 0, err
	}
	if err := Prune(st); err != nil {
		return 0, err
	}
	return len(all), nil
}

// SubmittedOr returns what a read of your finished reviews found or, when it failed,
// the ones last stored. They only feed a count, so a failed read shouldn't hold back
// the reviews and PRs that did arrive.
func SubmittedOr(st *store.Store, source string, found []Item, err error) []Item {
	if err == nil {
		return found
	}
	stored, listErr := st.ListExternalItems(source)
	if listErr != nil {
		return nil
	}
	var kept []Item
	for _, it := range stored {
		if it.Kind == "review_done" {
			kept = append(kept, Item{Kind: it.Kind, ExternalID: it.ExternalID, Title: it.Title, URL: it.URL, Repo: it.Repo, UpdatedAt: it.UpdatedAt})
		}
	}
	return kept
}

// RecordSync keeps a sync's failure for Settings to show, or clears it after a good
// one. Errors from the connectors carry no token or upstream body.
func RecordSync(st *store.Store, source string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_ = st.SetSetting(source+"_sync_error", msg)
}

// Prune forgets that a PR was hidden when it left every source's review list, so a
// later request shows it normally, and that one was marked done when it is no
// longer open. It looks at all sources: a source that failed keeps its last good
// items, and with them its marks.
func Prune(st *store.Store) error {
	items, err := st.ListExternalItems("")
	if err != nil {
		return err
	}
	var requested, open []string
	for _, it := range items {
		switch it.Kind {
		case "review_request":
			requested = append(requested, it.ExternalID)
		case "own_pr":
			open = append(open, it.URL)
		}
	}
	if err := st.KeepHiddenReviews(requested); err != nil {
		return err
	}
	return st.KeepOwnPRsDone(open)
}
