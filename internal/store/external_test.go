package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestExternalItemsReplaceAndList(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	mk := func(id, title string, at time.Time, rr bool) store.ExternalItem {
		return store.ExternalItem{
			Source: "github", Kind: "review_request", ExternalID: id, Title: title,
			Repo: "o/r", URL: id, ReviewRequestedOfMe: rr, UpdatedAt: at,
		}
	}
	a := mk("a", "A", base, true)
	b := mk("b", "B", base.Add(time.Minute), true)
	if err := st.ReplaceExternalItems("github", []store.ExternalItem{a, b}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListExternalItems("github")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	// newest UpdatedAt first -> b then a
	if got[0].ExternalID != "b" || got[1].ExternalID != "a" {
		t.Fatalf("order (newest first): %+v", got)
	}
	if !got[0].ReviewRequestedOfMe {
		t.Fatal("ReviewRequestedOfMe lost on round-trip")
	}

	// a different source must survive a github replace
	if err := st.ReplaceExternalItems("linear", []store.ExternalItem{mk("x", "X", base, false)}); err != nil {
		t.Fatal(err)
	}
	c := mk("c", "C", base.Add(2*time.Minute), true)
	if err := st.ReplaceExternalItems("github", []store.ExternalItem{b, c}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.ListExternalItems("github")
	if len(got) != 2 {
		t.Fatalf("after replace want 2, got %d", len(got))
	}
	for _, it := range got {
		if it.ExternalID == "a" {
			t.Fatal("A should be gone after a whole-source replace")
		}
	}
	if lin, _ := st.ListExternalItems("linear"); len(lin) != 1 {
		t.Fatalf("a different source must be untouched, got %d", len(lin))
	}
}

func TestExternalItemsEstimateAndAllSources(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := st.ReplaceExternalItems("linear", []store.ExternalItem{{Source: "linear", Kind: "task", ExternalID: "l", Estimate: 3, UpdatedAt: at}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertExternalItem(store.ExternalItem{Source: "github", Kind: "review_request", ExternalID: "g", UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	all, err := st.ListExternalItems("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("source \"\" should list every source, got %+v", all)
	}
	lin, _ := st.ListExternalItems("linear")
	if len(lin) != 1 || lin[0].Estimate != 3 {
		t.Fatalf("estimate lost on round-trip: %+v", lin)
	}
}

func TestExternalItemsStageRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := st.ReplaceExternalItems("linear", []store.ExternalItem{{Source: "linear", Kind: "task", ExternalID: "l", Stage: "started", UpdatedAt: at}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertExternalItem(store.ExternalItem{Source: "jira", Kind: "task", ExternalID: "j", Stage: "backlog", UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	lin, _ := st.ListExternalItems("linear")
	jir, _ := st.ListExternalItems("jira")
	if len(lin) != 1 || lin[0].Stage != "started" || len(jir) != 1 || jir[0].Stage != "backlog" {
		t.Fatalf("stage lost on round-trip: %+v %+v", lin, jir)
	}
}

func TestExternalItemsProjectAndLabelsRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := st.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "l", Project: "Onboarding", Labels: []string{"bug", "infra, ops"}, UpdatedAt: at},
		{Source: "linear", Kind: "task", ExternalID: "n", UpdatedAt: at},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertExternalItem(store.ExternalItem{Source: "jira", Kind: "task", ExternalID: "j", Project: "Ops", Labels: []string{"x"}, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	got := map[string]store.ExternalItem{}
	all, _ := st.ListExternalItems("")
	for _, it := range all {
		got[it.ExternalID] = it
	}
	if l := got["l"]; l.Project != "Onboarding" || len(l.Labels) != 2 || l.Labels[0] != "bug" || l.Labels[1] != "infra, ops" {
		t.Fatalf("project and labels lost on replace: %+v", l)
	}
	if n := got["n"]; n.Project != "" || len(n.Labels) != 0 {
		t.Fatalf("no labels must read back as none: %+v", n)
	}
	if j := got["j"]; j.Project != "Ops" || len(j.Labels) != 1 || j.Labels[0] != "x" {
		t.Fatalf("project and labels lost on upsert: %+v", j)
	}
}

func TestExternalItemsCompletedAtRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	done := at.Add(time.Hour)
	if err := st.ReplaceExternalItems("linear", []store.ExternalItem{{Source: "linear", Kind: "task", ExternalID: "l", CompletedAt: done, UpdatedAt: at}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertExternalItem(store.ExternalItem{Source: "jira", Kind: "task", ExternalID: "j", CompletedAt: done, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertExternalItem(store.ExternalItem{Source: "jira", Kind: "task", ExternalID: "open", UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	got := map[string]time.Time{}
	all, _ := st.ListExternalItems("")
	for _, it := range all {
		got[it.ExternalID] = it.CompletedAt
	}
	if !got["l"].Equal(done) || !got["j"].Equal(done) || !got["open"].IsZero() {
		t.Fatalf("completed at lost on round-trip: %+v", got)
	}
}

func TestExternalItemsStartedAtRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := st.ReplaceExternalItems("linear", []store.ExternalItem{{Source: "linear", Kind: "task", ExternalID: "l", StartedAt: at}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertExternalItem(store.ExternalItem{Source: "jira", Kind: "task", ExternalID: "j", StartedAt: at}); err != nil {
		t.Fatal(err)
	}
	all, _ := st.ListExternalItems("")
	if len(all) != 2 || !all[0].StartedAt.Equal(at) || !all[1].StartedAt.Equal(at) {
		t.Fatalf("started at lost on round-trip: %+v", all)
	}
}

// A finished ticket or a submitted review outlives the sync window that
// replaces the trackers' items, so older days keep what was finished on them.
func TestFinishesAreKeptAfterTheItemsAreReplaced(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	if err := st.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "i1", Repo: "ENG-1", Title: "Done one", URL: "u1", Stage: "completed", CompletedAt: at},
		{Source: "linear", Kind: "task", ExternalID: "i2", Repo: "ENG-2", Title: "Open one", Stage: "started"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceExternalItems("github", []store.ExternalItem{
		{Source: "github", Kind: "review_done", ExternalID: "review:p", Repo: "o/r", Title: "A PR", URL: "p", UpdatedAt: at.Add(time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	// The next syncs no longer return them, and the same finish synced twice is kept once.
	for i := 0; i < 2; i++ {
		if err := st.ReplaceExternalItems("linear", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.ReplaceExternalItems("github", nil); err != nil {
		t.Fatal(err)
	}
	got, err := st.Finishes(at.Add(-time.Hour), at.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := []store.Finish{
		{Source: "linear", Kind: "task", Key: "ENG-1", Title: "Done one", URL: "u1", At: at},
		{Source: "github", Kind: "review_done", Key: "o/r", Title: "A PR", URL: "p", At: at.Add(time.Hour)},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
	if out, _ := st.Finishes(at.Add(time.Minute), at.Add(time.Hour)); len(out) != 0 {
		t.Fatalf("outside [from, to): %+v", out)
	}
}

func TestExternalItemsKeepSizes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := st.ReplaceExternalItems("github", []store.ExternalItem{
		{Source: "github", Kind: "review_request", ExternalID: "u", Additions: 120, Deletions: 30, ChangedFiles: 4, RequestedAt: at, UpdatedAt: at},
		{Source: "github", Kind: "review_request", ExternalID: "v", UpdatedAt: at},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListExternalItems("github")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]store.ExternalItem{}
	for _, it := range items {
		byID[it.ExternalID] = it
	}
	u := byID["u"]
	if u.Additions != 120 || u.Deletions != 30 || u.ChangedFiles != 4 || !u.RequestedAt.Equal(at) {
		t.Fatalf("sizes: %+v", u)
	}
	if !byID["v"].RequestedAt.IsZero() {
		t.Fatalf("no request stays zero: %+v", byID["v"])
	}
}

func TestHiddenReviews(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, u := range []string{"a", "b", "c"} {
		if err := st.HideReview(u, at); err != nil {
			t.Fatal(err)
		}
	}
	later := at.Add(time.Hour)
	if err := st.HideReview("a", later); err != nil {
		t.Fatal(err)
	}
	if err := st.UnhideReview("b"); err != nil {
		t.Fatal(err)
	}
	got, err := st.HiddenReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["a"].Equal(later) || !got["c"].Equal(at) {
		t.Fatalf("hidden: %v", got)
	}
	if err := st.KeepHiddenReviews([]string{"a", "z"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.HiddenReviews(); len(got) != 1 || !got["a"].Equal(later) {
		t.Fatalf("after keep: %v", got)
	}
	if err := st.KeepHiddenReviews(nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.HiddenReviews(); len(got) != 0 {
		t.Fatalf("keeping nothing clears: %v", got)
	}
}
