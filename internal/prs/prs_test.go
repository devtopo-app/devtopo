package prs

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func at(hour int) time.Time { return time.Date(2026, 10, 28, hour, 0, 0, 0, time.UTC) }

func TestApplyReviewsMostUrgentWins(t *testing.T) {
	it := Item{UpdatedAt: at(8)}
	it.ApplyReviews([]Review{{"alex", "approved", at(12)}, {"sam", "changes_requested", at(9)}, {"kim", "commented", at(10)}})
	if it.State != "changes_requested" || it.Reviewer != "sam" || !it.ReviewedAt.Equal(at(9)) || !it.UpdatedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestApplyReviewsLatestWinsATie(t *testing.T) {
	it := Item{UpdatedAt: at(8)}
	it.ApplyReviews([]Review{{"alex", "commented", at(9)}, {"sam", "commented", at(12)}})
	if it.Reviewer != "sam" || !it.ReviewedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestApplyReviewsWithNoneLeavesTheItem(t *testing.T) {
	it := Item{UpdatedAt: at(8)}
	it.ApplyReviews(nil)
	if it.State != "" || it.Reviewer != "" || !it.ReviewedAt.IsZero() || !it.UpdatedAt.Equal(at(8)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestIsGenerated(t *testing.T) {
	if !IsGenerated("web/dist/a.js") || !IsGenerated("go.sum") || IsGenerated("main.go") {
		t.Fatal("generated files are told apart by path")
	}
}

func open(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestStoreKeepsOnlyItsOwnSourceReplaced(t *testing.T) {
	st := open(t)
	if _, err := Store(st, "github", []Item{{Kind: "review_request", ExternalID: "g", URL: "g", UpdatedAt: at(9)}}); err != nil {
		t.Fatal(err)
	}
	n, err := Store(st, "gitlab", []Item{{Kind: "review_request", ExternalID: "l", URL: "l", UpdatedAt: at(9)}}, []Item{{Kind: "own_pr", ExternalID: "own:m", URL: "m", State: "approved", Reviewer: "alex", ReviewedAt: at(9), UpdatedAt: at(9)}})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	items, _ := st.ListExternalItems("")
	if len(items) != 3 {
		t.Fatalf("items: %+v", items)
	}
	own, _ := st.ListExternalItems("gitlab")
	found := false
	for _, it := range own {
		found = found || (it.Kind == "own_pr" && it.Reviewer == "alex" && it.State == "approved" && it.ReviewedAt.Equal(at(9)))
	}
	if !found {
		t.Fatalf("own PR fields lost: %+v", own)
	}
}

// One source's sync must not forget what the user hid or marked done on another's PRs.
func TestStoreKeepsMarksOfOtherSources(t *testing.T) {
	st := open(t)
	if _, err := Store(st, "github", []Item{{Kind: "review_request", ExternalID: "g", URL: "g", UpdatedAt: at(9)}}, []Item{{Kind: "own_pr", ExternalID: "own:gm", URL: "gm", UpdatedAt: at(9)}}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"g", "gone"} {
		if err := st.HideReview(u, at(10)); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []string{"gm", "gonem"} {
		if err := st.MarkOwnPRDone(u, at(10)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Store(st, "gitlab", nil); err != nil {
		t.Fatal(err)
	}
	hidden, _ := st.HiddenReviews()
	if _, ok := hidden["g"]; !ok || len(hidden) != 1 {
		t.Fatalf("hidden: %v", hidden)
	}
	marks, _ := st.OwnPRsDone()
	if _, ok := marks["gm"]; !ok || len(marks) != 1 {
		t.Fatalf("marks: %v", marks)
	}
}

func TestApplyReviewsSkipsAnUnknownState(t *testing.T) {
	it := Item{UpdatedAt: at(8)}
	it.ApplyReviews([]Review{{"alex", "dismissed", at(12)}})
	if it.State != "" || it.Reviewer != "" || !it.UpdatedAt.Equal(at(8)) {
		t.Fatalf("got: %+v", it)
	}
}
