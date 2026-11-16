package github

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeFetcher struct {
	reviews []Item
	prs     []Item
	done    []Item
	own     []Item
	rerr    error
	oerr    error
}

func (f fakeFetcher) ReviewsSubmitted(context.Context, time.Time) ([]Item, error) { return f.done, nil }

func (f fakeFetcher) ReviewRequests(context.Context) ([]Item, error) {
	if f.rerr != nil {
		return nil, f.rerr
	}
	return f.reviews, nil
}
func (f fakeFetcher) AuthoredPRs(context.Context) ([]Item, error) { return f.prs, nil }
func (f fakeFetcher) OwnOpenPRs(context.Context) ([]Item, error)  { return f.own, f.oerr }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSyncStoresItems(t *testing.T) {
	st := openStore(t)
	f := fakeFetcher{
		reviews: []Item{{Kind: "review_request", ExternalID: "rr1", Title: "review me", Repo: "o/r", URL: "rr1", ReviewRequestedOfMe: true, UpdatedAt: time.Now()}},
		prs:     []Item{{Kind: "pr", ExternalID: "pr1", Title: "my pr", Repo: "o/r", URL: "pr1", State: "open", UpdatedAt: time.Now()}},
	}
	n, err := Sync(context.Background(), st, f)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("stored %d", n)
	}
	items, _ := st.ListExternalItems("github")
	if len(items) != 2 {
		t.Fatalf("list %d", len(items))
	}
	var rr *store.ExternalItem
	for i := range items {
		if items[i].Kind == "review_request" {
			rr = &items[i]
		}
	}
	if rr == nil || !rr.ReviewRequestedOfMe {
		t.Fatalf("review_request flag lost: %+v", items)
	}
}

// A review you submitted is stored next to a pending request for the same PR.
func TestSyncStoresSubmittedReviews(t *testing.T) {
	st := openStore(t)
	f := fakeFetcher{
		reviews: []Item{{Kind: "review_request", ExternalID: "https://x/pull/1", URL: "https://x/pull/1", UpdatedAt: time.Now()}},
		done:    []Item{{Kind: "review_done", ExternalID: "review:https://x/pull/1", URL: "https://x/pull/1", Title: "t", UpdatedAt: time.Now()}},
	}
	if n, err := Sync(context.Background(), st, f); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	items, _ := st.ListExternalItems("github")
	kinds := map[string]bool{}
	for _, it := range items {
		kinds[it.Kind] = true
	}
	if !kinds["review_done"] || !kinds["review_request"] {
		t.Fatalf("stored: %+v", items)
	}
}

func TestSyncLeavesItemsOnFetchError(t *testing.T) {
	st := openStore(t)
	ok := fakeFetcher{reviews: []Item{{Kind: "review_request", ExternalID: "rr1", Title: "x", UpdatedAt: time.Now()}}}
	if _, err := Sync(context.Background(), st, ok); err != nil {
		t.Fatal(err)
	}
	bad := fakeFetcher{rerr: errors.New("gh down")}
	if _, err := Sync(context.Background(), st, bad); err == nil {
		t.Fatal("a fetch error must be returned")
	}
	if items, _ := st.ListExternalItems("github"); len(items) != 1 {
		t.Fatalf("a fetch error must not wipe stored items, got %d", len(items))
	}
}

func TestSyncKeepsTheListWhenGitHubFails(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := Sync(context.Background(), st, fakeFetcher{reviews: []Item{{Kind: "review_request", ExternalID: "u", URL: "u", UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{rerr: errors.New("boom")}); err == nil {
		t.Fatal("want the error")
	}
	items, _ := st.ListExternalItems("github")
	if len(items) != 1 || items[0].ExternalID != "u" {
		t.Fatalf("a failed sync must leave the list: %+v", items)
	}
}

func TestSyncForgetsHiddenPRsThatLeftTheList(t *testing.T) {
	st := openStore(t)
	for _, u := range []string{"stays", "left"} {
		if err := st.HideReview(u, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// A PR you authored or reviewed with the same URL doesn't keep it.
	f := fakeFetcher{
		reviews: []Item{{Kind: "review_request", ExternalID: "stays", URL: "stays", UpdatedAt: time.Now()}},
		prs:     []Item{{Kind: "pr", ExternalID: "left", URL: "left", UpdatedAt: time.Now()}},
	}
	if _, err := Sync(context.Background(), st, f); err != nil {
		t.Fatal(err)
	}
	hidden, _ := st.HiddenReviews()
	if _, ok := hidden["stays"]; !ok || len(hidden) != 1 {
		t.Fatalf("hidden: %v", hidden)
	}
}

func TestSyncFailureKeepsHiddenPRs(t *testing.T) {
	st := openStore(t)
	if err := st.HideReview("u", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{rerr: errors.New("boom")}); err == nil {
		t.Fatal("want the error")
	}
	if hidden, _ := st.HiddenReviews(); len(hidden) != 1 {
		t.Fatalf("a failed sync must keep hidden PRs: %v", hidden)
	}
}

func TestSyncStoresOwnPRs(t *testing.T) {
	st := openStore(t)
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	f := fakeFetcher{
		prs: []Item{{Kind: "pr", ExternalID: "https://x/pull/1", URL: "https://x/pull/1", UpdatedAt: at}},
		own: []Item{{Kind: "own_pr", ExternalID: store.OwnPRPrefix + "https://x/pull/1", URL: "https://x/pull/1", State: "approved", Reviewer: "alex", ReviewedAt: at, UpdatedAt: at}},
	}
	if n, err := Sync(context.Background(), st, f); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	items, _ := st.ListExternalItems("github")
	for _, it := range items {
		if it.Kind == "own_pr" {
			if it.ExternalID != "own:https://x/pull/1" || it.State != "approved" || it.Reviewer != "alex" || !it.ReviewedAt.Equal(at) {
				t.Fatalf("own pr: %+v", it)
			}
			return
		}
	}
	t.Fatalf("no own pr stored: %+v", items)
}

func TestSyncForgetsDoneMarksOfPRsNoLongerOpen(t *testing.T) {
	st := openStore(t)
	for _, u := range []string{"open", "closed"} {
		if err := st.MarkOwnPRDone(u, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	f := fakeFetcher{own: []Item{{Kind: "own_pr", ExternalID: "own:open", URL: "open", UpdatedAt: time.Now()}}}
	if _, err := Sync(context.Background(), st, f); err != nil {
		t.Fatal(err)
	}
	marks, _ := st.OwnPRsDone()
	if _, ok := marks["open"]; !ok || len(marks) != 1 {
		t.Fatalf("marks: %v", marks)
	}
}

func TestSyncFailureKeepsOwnPRDoneMarks(t *testing.T) {
	st := openStore(t)
	ok := fakeFetcher{own: []Item{{Kind: "own_pr", ExternalID: "own:u", URL: "u", UpdatedAt: time.Now()}}}
	if _, err := Sync(context.Background(), st, ok); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkOwnPRDone("u", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{oerr: errors.New("boom")}); err == nil {
		t.Fatal("want the error")
	}
	if marks, _ := st.OwnPRsDone(); len(marks) != 1 {
		t.Fatalf("a failed sync must keep done marks: %v", marks)
	}
	if items, _ := st.ListExternalItems("github"); len(items) != 1 {
		t.Fatalf("a failed sync must keep the items: %+v", items)
	}
}
