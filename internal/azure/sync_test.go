package azure

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeFetcher struct {
	reviews, own, done []prs.Item
	err                error
	calls              int
}

func (f *fakeFetcher) ReviewRequests(context.Context, string, string) ([]prs.Item, error) {
	f.calls++
	return f.reviews, f.err
}
func (f *fakeFetcher) OwnOpenPRs(context.Context, string, string) ([]prs.Item, error) {
	return f.own, f.err
}
func (f *fakeFetcher) ReviewsSubmitted(context.Context, string, string, time.Time) ([]prs.Item, error) {
	return f.done, f.err
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func connect(t *testing.T, st *store.Store) {
	t.Helper()
	for k, v := range map[string]string{"azure_org": "https://ado", "azure_token": "t"} {
		if err := st.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncDoesNothingUntilConnected(t *testing.T) {
	st := openStore(t)
	f := &fakeFetcher{}
	if n, err := Sync(context.Background(), st, f); err != nil || n != 0 || f.calls != 0 {
		t.Fatalf("n=%d err=%v calls=%d", n, err, f.calls)
	}
}

func TestSyncStoresUnderItsSource(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	now := time.Now()
	f := &fakeFetcher{
		reviews: []prs.Item{{Kind: "review_request", ExternalID: "u", URL: "u", UpdatedAt: now}},
		own:     []prs.Item{{Kind: "own_pr", ExternalID: "own:m", URL: "m", State: prs.Approved, UpdatedAt: now}},
		done:    []prs.Item{{Kind: "review_done", ExternalID: "review:d", URL: "d", UpdatedAt: now}},
	}
	if n, err := Sync(context.Background(), st, f); err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if items, _ := st.ListExternalItems("azure"); len(items) != 3 {
		t.Fatalf("items: %+v", items)
	}
}

func TestSyncFailureKeepsTheItemsAndMarks(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	ok := &fakeFetcher{own: []prs.Item{{Kind: "own_pr", ExternalID: "own:m", URL: "m", UpdatedAt: time.Now()}}}
	if _, err := Sync(context.Background(), st, ok); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkOwnPRDone("m", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, &fakeFetcher{err: errors.New("boom")}); err == nil {
		t.Fatal("want the error")
	}
	if items, _ := st.ListExternalItems("azure"); len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	if marks, _ := st.OwnPRsDone(); len(marks) != 1 {
		t.Fatalf("marks: %v", marks)
	}
}

type doneFails struct{ fakeFetcher }

func (f *doneFails) ReviewsSubmitted(context.Context, string, string, time.Time) ([]prs.Item, error) {
	return nil, errors.New("too busy")
}

// Finished reviews only feed a count, so failing to read them must not hold back
// the reviews and PRs that did arrive.
func TestSyncKeepsLastFinishedReviewsWhenTheirReadFails(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	now := time.Now().UTC().Truncate(time.Second)
	done := prs.Item{Kind: "review_done", ExternalID: "review:d", URL: "d", Title: "Done", Repo: "o/r", UpdatedAt: now}
	if _, err := Sync(context.Background(), st, &fakeFetcher{done: []prs.Item{done}}); err != nil {
		t.Fatal(err)
	}
	f := &doneFails{fakeFetcher{reviews: []prs.Item{{Kind: "review_request", ExternalID: "new", URL: "new", UpdatedAt: now}}}}
	if n, err := Sync(context.Background(), st, f); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	kept := map[string]store.ExternalItem{}
	items, _ := st.ListExternalItems("azure")
	for _, it := range items {
		kept[it.ExternalID] = it
	}
	if d := kept["review:d"]; d.Title != "Done" || !d.UpdatedAt.Equal(now) {
		t.Fatalf("items: %+v", items)
	}
	if _, ok := kept["new"]; !ok {
		t.Fatalf("items: %+v", items)
	}
}

func TestSyncRecordsItsLastFailure(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	if _, err := Sync(context.Background(), st, &fakeFetcher{err: errors.New("boom")}); err == nil {
		t.Fatal("want the error")
	}
	if got, _, _ := st.GetSetting("azure_sync_error"); got != "boom" {
		t.Fatalf("recorded %q", got)
	}
	if _, err := Sync(context.Background(), st, &fakeFetcher{}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.GetSetting("azure_sync_error"); got != "" {
		t.Fatalf("a good sync must clear it: %q", got)
	}
}
