package linear

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeFetcher struct {
	items []Item
	err   error
}

func (f fakeFetcher) AssignedIssues(context.Context, string) ([]Item, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
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

func TestSyncNoKeyIsNoop(t *testing.T) {
	st := openStore(t)
	n, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{ExternalID: "x"}}})
	if err != nil || n != 0 {
		t.Fatalf("no key must be a no-op, got n=%d err=%v", n, err)
	}
	if items, _ := st.ListExternalItems("linear"); len(items) != 0 {
		t.Fatal("no items should be stored when no key is set")
	}
}

func TestSyncStoresAssignedIssues(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "lin_api_test"); err != nil {
		t.Fatal(err)
	}
	n, err := Sync(context.Background(), st, fakeFetcher{items: []Item{
		{ExternalID: "abc", Identifier: "ENG-1", Title: "Fix X", UpdatedAt: time.Now()},
	}})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	items, _ := st.ListExternalItems("linear")
	if len(items) != 1 || items[0].Kind != "task" || !items[0].AssignedToMe || items[0].Repo != "ENG-1" {
		t.Fatalf("stored linear item: %+v", items)
	}
}

func TestSyncLeavesItemsOnFetchError(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{ExternalID: "a", UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{err: errors.New("down")}); err == nil {
		t.Fatal("expected a fetch error")
	}
	if items, _ := st.ListExternalItems("linear"); len(items) != 1 {
		t.Fatalf("a fetch error must not wipe items, got %d", len(items))
	}
}

func TestSyncStoresEstimate(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{ExternalID: "a", Estimate: 2, UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if items, _ := st.ListExternalItems("linear"); len(items) != 1 || items[0].Estimate != 2 {
		t.Fatalf("estimate not stored: %+v", items)
	}
}

func TestSyncNormalizesStage(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "lin_api_test"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"started": "started", "unstarted": "unstarted", "backlog": "backlog", "triage": "triage", "completed": "completed", "canceled": "canceled", "odd": ""}
	var in []Item
	for typ := range want {
		in = append(in, Item{ExternalID: typ, StateType: typ, UpdatedAt: time.Now()})
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{items: in}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.ListExternalItems("linear")
	for _, it := range items {
		if it.Stage != want[it.ExternalID] {
			t.Errorf("state type %q: stage %q, want %q", it.ExternalID, it.Stage, want[it.ExternalID])
		}
	}
}

func TestSyncStoresProjectAndLabels(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "lin_api_test"); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{ExternalID: "a", Project: "Onboarding", Labels: []string{"bug"}, UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if items, _ := st.ListExternalItems("linear"); len(items) != 1 || items[0].Project != "Onboarding" || len(items[0].Labels) != 1 || items[0].Labels[0] != "bug" {
		t.Fatalf("project and labels not stored: %+v", items)
	}
}

func TestSyncStoresCompletedAt(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "lin_api_test"); err != nil {
		t.Fatal(err)
	}
	done := time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{ExternalID: "a", StateType: "completed", CompletedAt: done, UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if items, _ := st.ListExternalItems("linear"); len(items) != 1 || !items[0].CompletedAt.Equal(done) {
		t.Fatalf("completed at not stored: %+v", items)
	}
}

func TestSyncStoresStartedAt(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "lin_api_test"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{ExternalID: "a", StateType: "started", StartedAt: at}}}); err != nil {
		t.Fatal(err)
	}
	if items, _ := st.ListExternalItems("linear"); len(items) != 1 || !items[0].StartedAt.Equal(at) {
		t.Fatalf("started at not stored: %+v", items)
	}
}

// A Linear reply of 200 with GraphQL errors must not wipe the stored issues.
func TestSyncLeavesItemsOnGraphQLErrors(t *testing.T) {
	st := openStore(t)
	if err := st.SetSetting("linear_api_key", "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{ExternalID: "a", UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	c := &Client{Endpoint: "x", httpDo: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":null,"errors":[{"message":"rate limited"}]}`)), Header: make(http.Header)}, nil
	}}
	if _, err := Sync(context.Background(), st, c); err == nil {
		t.Fatal("expected an error for a GraphQL error reply")
	}
	if items, _ := st.ListExternalItems("linear"); len(items) != 1 {
		t.Fatalf("GraphQL errors must not wipe items, got %d", len(items))
	}
}
