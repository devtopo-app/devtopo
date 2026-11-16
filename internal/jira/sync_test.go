package jira

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeFetcher struct {
	items []Item
	err   error
}

func (f fakeFetcher) AssignedIssues(context.Context, string, string, string) ([]Item, error) {
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

func connect(t *testing.T, st *store.Store) {
	t.Helper()
	for k, v := range map[string]string{
		"jira_site": "https://example.atlassian.net", "jira_email": "me@example.com", "jira_api_token": "jira_test_token",
	} {
		if err := st.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncNoCredentialsIsNoop(t *testing.T) {
	st := openStore(t)
	n, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{Key: "ABC-1"}}})
	if err != nil || n != 0 {
		t.Fatalf("no credentials must be a no-op, got n=%d err=%v", n, err)
	}
	if items, _ := st.ListExternalItems("jira"); len(items) != 0 {
		t.Fatal("no items should be stored when no credentials are set")
	}
}

func TestSyncStoresAssignedIssues(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	n, err := Sync(context.Background(), st, fakeFetcher{items: []Item{
		{Key: "ABC-1", Title: "Fix X", URL: "https://example.atlassian.net/browse/ABC-1", Points: 3, UpdatedAt: time.Now()},
	}})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	items, _ := st.ListExternalItems("jira")
	if len(items) != 1 || items[0].Kind != "task" || !items[0].AssignedToMe || items[0].ExternalID != "ABC-1" || items[0].Repo != "ABC-1" || items[0].Estimate != 3 {
		t.Fatalf("stored jira item: %+v", items)
	}
}

func TestSyncLeavesItemsOnFetchError(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{Key: "ABC-1", UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{err: errors.New("down")}); err == nil {
		t.Fatal("expected a fetch error")
	}
	if items, _ := st.ListExternalItems("jira"); len(items) != 1 {
		t.Fatalf("a fetch error must not wipe items, got %d", len(items))
	}
}

func TestSyncNormalizesStage(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	want := map[string]string{"indeterminate": "started", "new": "unstarted", "done": "completed", "odd": ""}
	var in []Item
	for cat := range want {
		in = append(in, Item{Key: cat, Category: cat, UpdatedAt: time.Now()})
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{items: in}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.ListExternalItems("jira")
	for _, it := range items {
		if it.Stage != want[it.ExternalID] {
			t.Errorf("category %q: stage %q, want %q", it.ExternalID, it.Stage, want[it.ExternalID])
		}
	}
}

func TestSyncStoresProjectAndLabels(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{{Key: "ABC-1", Project: "Platform", Labels: []string{"bug"}, UpdatedAt: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	if items, _ := st.ListExternalItems("jira"); len(items) != 1 || items[0].Project != "Platform" || len(items[0].Labels) != 1 || items[0].Labels[0] != "bug" {
		t.Fatalf("project and labels not stored: %+v", items)
	}
}

func TestSyncCompletedAtOnlyWhenDone(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	changed := time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{
		{Key: "done", Category: "done", CategoryChangedAt: changed, UpdatedAt: time.Now()},
		{Key: "doing", Category: "indeterminate", CategoryChangedAt: changed, UpdatedAt: time.Now()},
	}}); err != nil {
		t.Fatal(err)
	}
	got := map[string]time.Time{}
	items, _ := st.ListExternalItems("jira")
	for _, it := range items {
		got[it.ExternalID] = it.CompletedAt
	}
	if !got["done"].Equal(changed) || !got["doing"].IsZero() {
		t.Fatalf("completed at is the category change of a done issue only: %+v", got)
	}
}

func TestSyncStartedAtOnlyWhenInProgress(t *testing.T) {
	st := openStore(t)
	connect(t, st)
	changed := time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)
	if _, err := Sync(context.Background(), st, fakeFetcher{items: []Item{
		{Key: "done", Category: "done", CategoryChangedAt: changed, UpdatedAt: time.Now()},
		{Key: "doing", Category: "indeterminate", CategoryChangedAt: changed, UpdatedAt: time.Now()},
	}}); err != nil {
		t.Fatal(err)
	}
	got := map[string]time.Time{}
	items, _ := st.ListExternalItems("jira")
	for _, it := range items {
		got[it.ExternalID] = it.StartedAt
	}
	if !got["doing"].Equal(changed) || !got["done"].IsZero() {
		t.Fatalf("started at is the category change of an in-progress issue only: %+v", got)
	}
}
