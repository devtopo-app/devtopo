package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestReviewHoursBands(t *testing.T) {
	cases := []struct {
		lines int
		sized bool
		want  float64
	}{
		{0, true, 0.25}, {50, true, 0.25}, {51, true, 0.5}, {200, true, 0.5}, {201, true, 1}, {500, true, 1},
		{501, true, 2}, {1000, true, 2}, {1001, true, 3}, {100000, true, 3}, {0, false, 0.5}, {5000, false, 0.5},
	}
	for _, c := range cases {
		if got := reviewHours(c.lines, c.sized); got != c.want {
			t.Errorf("reviewHours(%d, %v) = %v, want %v", c.lines, c.sized, got, c.want)
		}
	}
}

// plannableReview is the one item /api/plannable lists.
func plannableReview(t *testing.T, s *Server) plannableItem {
	t.Helper()
	var items []plannableItem
	if err := json.Unmarshal(do(t, s, "GET", "/api/plannable", "").Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	return items[0]
}

func storeReview(t *testing.T, s *Server, it store.ExternalItem) {
	t.Helper()
	it.Source, it.Kind, it.ExternalID, it.URL, it.ReviewRequestedOfMe = "github", "review_request", "https://x/pull/1", "https://x/pull/1", true
	if err := s.store.ReplaceExternalItems("github", []store.ExternalItem{it}); err != nil {
		t.Fatal(err)
	}
}

func TestPlannableEstimatesReviews(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	storeReview(t, s, store.ExternalItem{Additions: 40, Deletions: 20, ChangedFiles: 3})
	if got := plannableReview(t, s); got.Hours != 0.5 || !got.Sized || got.Additions != 40 || got.Deletions != 20 {
		t.Fatalf("sized: %+v", got)
	}
	storeReview(t, s, store.ExternalItem{Additions: 10, Deletions: 10, ChangedFiles: 1})
	if got := plannableReview(t, s); got.Hours != 0.25 {
		t.Fatalf("a small PR: %+v", got)
	}
	storeReview(t, s, store.ExternalItem{ChangedFiles: 1})
	if got := plannableReview(t, s); got.Hours != 0.25 || !got.Sized {
		t.Fatalf("all generated is known to be small: %+v", got)
	}
	storeReview(t, s, store.ExternalItem{})
	if got := plannableReview(t, s); got.Hours != 0.5 || got.Sized {
		t.Fatalf("unknown size: %+v", got)
	}
}

func TestPlannableMarksHidden(t *testing.T) {
	noon := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, noon, nil, nil)
	if err := s.store.HideReview("https://x/pull/1", noon); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		it   store.ExternalItem
		want bool
	}{
		{"asked before hiding", store.ExternalItem{RequestedAt: noon.Add(-time.Hour)}, true},
		{"no request of you seen", store.ExternalItem{}, true},
		{"new commits", store.ExternalItem{UpdatedAt: noon.Add(time.Hour), RequestedAt: noon.Add(-time.Hour)}, true},
		{"asked again", store.ExternalItem{RequestedAt: noon.Add(time.Hour)}, false},
	}
	for _, c := range cases {
		storeReview(t, s, c.it)
		if got := plannableReview(t, s); got.Hidden != c.want {
			t.Errorf("%s: hidden = %v, want %v", c.name, got.Hidden, c.want)
		}
	}
}

func TestHideReviewRoute(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	storeReview(t, s, store.ExternalItem{})
	if rec := do(t, s, "POST", "/api/reviews/hidden", `{"URL":"https://x/pull/1","Hidden":true}`); rec.Code != 204 {
		t.Fatalf("hide: %d %s", rec.Code, rec.Body)
	}
	if !plannableReview(t, s).Hidden {
		t.Fatal("hide must show in plannable")
	}
	if rec := do(t, s, "POST", "/api/reviews/hidden", `{"URL":"https://x/pull/1","Hidden":false}`); rec.Code != 204 {
		t.Fatalf("unhide: %d %s", rec.Code, rec.Body)
	}
	if plannableReview(t, s).Hidden {
		t.Fatal("unhide must show in plannable")
	}
	for _, body := range []string{`{"URL":"","Hidden":true}`, `{"URL":"  ","Hidden":true}`, `not json`} {
		if rec := do(t, s, "POST", "/api/reviews/hidden", body); rec.Code != 400 {
			t.Fatalf("%s: %d", body, rec.Code)
		}
	}
}

func TestHideReviewRefusesAnotherSite(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	r := httptest.NewRequest("POST", "/api/reviews/hidden", strings.NewReader(`{"URL":"https://x/pull/1","Hidden":true}`))
	r.Host = testHost
	r.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	if hidden, _ := s.store.HiddenReviews(); len(hidden) != 0 {
		t.Fatalf("a refused write must not hide: %v", hidden)
	}
}
