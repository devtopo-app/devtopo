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

const ownPRURL = "https://x/pull/1"

var ownNoon = time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)

// ownItem is one of your open PRs with a review in state.
func ownItem(state string) store.ExternalItem {
	return store.ExternalItem{Source: "github", Kind: "own_pr", ExternalID: store.OwnPRPrefix + ownPRURL, URL: ownPRURL, Title: "Mine", Repo: "o/r",
		State: state, Reviewer: "alex", ReviewedAt: ownNoon.Add(-time.Hour), UpdatedAt: ownNoon.Add(-time.Hour)}
}

func storeOwn(t *testing.T, s *Server, items ...store.ExternalItem) {
	t.Helper()
	if err := s.store.ReplaceExternalItems("github", items); err != nil {
		t.Fatal(err)
	}
}

func todayOf(t *testing.T, s *Server) TodayView {
	t.Helper()
	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOwnPRDoneRoute(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	storeOwn(t, s, ownItem("approved"))
	body := func(done string) string { return `{"URL":"` + ownPRURL + `","Done":` + done + `}` }
	if rec := do(t, s, "POST", "/api/own-prs/done", body("true")); rec.Code != 204 {
		t.Fatalf("mark: %d %s", rec.Code, rec.Body)
	}
	if !plannableReview(t, s).Done {
		t.Fatal("marking must show in plannable")
	}
	if rec := do(t, s, "POST", "/api/own-prs/done", body("false")); rec.Code != 204 {
		t.Fatalf("unmark: %d %s", rec.Code, rec.Body)
	}
	if plannableReview(t, s).Done {
		t.Fatal("unmarking must show in plannable")
	}
	for _, bad := range []string{`{"URL":"","Done":true}`, `{"URL":"  ","Done":true}`, `not json`} {
		if rec := do(t, s, "POST", "/api/own-prs/done", bad); rec.Code != 400 {
			t.Fatalf("%s: %d", bad, rec.Code)
		}
	}
}

func TestOwnPRDoneRefusesAnotherSite(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	r := httptest.NewRequest("POST", "/api/own-prs/done", strings.NewReader(`{"URL":"`+ownPRURL+`","Done":true}`))
	r.Host = testHost
	r.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
	if marks, _ := s.store.OwnPRsDone(); len(marks) != 0 {
		t.Fatalf("a refused write must not mark: %v", marks)
	}
}

func TestOwnPRDoneRule(t *testing.T) {
	marks := map[string]time.Time{ownPRURL: ownNoon}
	cases := []struct {
		name   string
		review time.Time
		marks  map[string]time.Time
		want   bool
	}{
		{"reviewed before", ownNoon.Add(-time.Hour), marks, true},
		{"reviewed at the same moment", ownNoon, marks, true},
		{"reviewed since", ownNoon.Add(time.Hour), marks, false},
		{"not marked", ownNoon.Add(-time.Hour), nil, false},
	}
	for _, c := range cases {
		it := ownItem("approved")
		it.UpdatedAt = c.review
		if got := ownPRDone(it, c.marks); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPlannableListsOwnPRs(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	storeOwn(t, s, ownItem(""))
	var items []plannableItem
	if err := json.Unmarshal(do(t, s, "GET", "/api/plannable", "").Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("a PR with no review must be left out: %+v", items)
	}
	for state, hours := range map[string]float64{"approved": 0.25, "changes_requested": 0.5, "commented": 0.5} {
		storeOwn(t, s, ownItem(state))
		got := plannableReview(t, s)
		if got.Kind != "own_pr" || got.RefID != "own:"+ownPRURL || got.State != state || got.Hours != hours || got.Reviewer != "alex" || !got.ReviewedAt.Equal(ownNoon.Add(-time.Hour)) || got.URL != ownPRURL {
			t.Errorf("%s: %+v", state, got)
		}
	}
}

func TestTodayListsYourPRs(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	if raw := get(t, s, "/api/today").Body.String(); !strings.Contains(raw, `"YourPRs":[]`) {
		t.Fatalf("empty must be [], not null: %s", raw)
	}
	unreviewed := ownItem("")
	unreviewed.ExternalID, unreviewed.URL = "own:https://x/pull/2", "https://x/pull/2"
	older := ownItem("commented")
	older.ExternalID, older.URL = "own:https://x/pull/3", "https://x/pull/3"
	older.UpdatedAt = ownNoon.Add(-5 * time.Hour)
	storeOwn(t, s, ownItem("changes_requested"), unreviewed, older)
	if err := s.store.MarkOwnPRDone(older.URL, ownNoon); err != nil {
		t.Fatal(err)
	}
	got := todayOf(t, s).YourPRs
	if len(got) != 2 {
		t.Fatalf("listed: %+v", got)
	}
	first := got[0]
	if first.RefID != "own:"+ownPRURL || first.Title != "Mine" || first.Repo != "o/r" || first.URL != ownPRURL || first.ReviewState != "changes_requested" || first.Reviewer != "alex" || first.Hours != 0.5 || first.Done {
		t.Fatalf("first: %+v", first)
	}
	if got[1].URL != older.URL || !got[1].Done {
		t.Fatalf("second: %+v", got[1])
	}
}

func TestPlanItemOwnPRDoneState(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	_ = s.store.CreatePlan(store.Plan{ID: "p", WindowStart: ownNoon, WindowEnd: ownNoon.Add(time.Hour), Status: "active"})
	_ = s.store.AddPlanItem(store.PlanItem{ID: "i", PlanID: "p", RefSource: "github", RefID: "own:" + ownPRURL, Text: "Mine"})
	item := func() planItem { return listPlans(t, s)[0].Items[0] }

	storeOwn(t, s, ownItem("approved"))
	if it := item(); it.RefStage != "" || it.RefState != "approved" || it.RefURL != ownPRURL {
		t.Fatalf("open: %+v", it)
	}
	if err := s.store.MarkOwnPRDone(ownPRURL, ownNoon); err != nil {
		t.Fatal(err)
	}
	if it := item(); it.RefStage != "completed" {
		t.Fatalf("marked: %+v", it)
	}
	if err := s.store.UnmarkOwnPRDone(ownPRURL); err != nil {
		t.Fatal(err)
	}
	storeOwn(t, s)
	if it := item(); it.RefStage != "completed" || it.RefURL != "" {
		t.Fatalf("no longer open: %+v", it)
	}
}

func TestItemDoneRefusesOwnPR(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	_ = s.store.CreatePlan(store.Plan{ID: "p", WindowStart: ownNoon, WindowEnd: ownNoon.Add(time.Hour), Status: "active"})
	_ = s.store.AddPlanItem(store.PlanItem{ID: "i", PlanID: "p", RefSource: "github", RefID: "own:" + ownPRURL})
	if rec := do(t, s, "POST", "/api/items/i/done", `{"done":true}`); rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
	if it := listPlans(t, s)[0].Items[0]; it.DoneAt != nil {
		t.Fatalf("done at: %+v", it)
	}
}

func TestOwnPRsLeaveTheStatsAlone(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	_ = s.store.CreatePlan(store.Plan{ID: "p", WindowStart: ownNoon, WindowEnd: ownNoon.Add(time.Hour), Status: "active"})
	_ = s.store.AddPlanItem(store.PlanItem{ID: "i", PlanID: "p", RefSource: "github", RefID: "own:" + ownPRURL, Text: "Mine"})
	storeOwn(t, s, ownItem("approved"))
	if rec := do(t, s, "POST", "/api/own-prs/done", `{"URL":"`+ownPRURL+`","Done":true}`); rec.Code != 204 {
		t.Fatalf("mark: %d", rec.Code)
	}
	var d HistoryDay
	if err := json.Unmarshal(get(t, s, "/api/history/day?d="+ownNoon.Format("2006-01-02")).Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Reviews != 0 || d.OtherTasks != 0 {
		t.Fatalf("stats: %+v", d)
	}
	if v := todayOf(t, s); len(v.ReviewsDone) != 0 || len(v.FinishedToday) != 0 {
		t.Fatalf("today: %+v", v)
	}
}
