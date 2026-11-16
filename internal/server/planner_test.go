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

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Host = testHost
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	return rec
}

func listPlans(t *testing.T, s *Server) []planWithItems {
	t.Helper()
	var plans []planWithItems
	if err := json.Unmarshal(do(t, s, "GET", "/api/plans", "").Body.Bytes(), &plans); err != nil {
		t.Fatal(err)
	}
	return plans
}

func TestPlannerCreateAndList(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)

	rec := do(t, s, "POST", "/api/plans", `{"name":"Thursday focus","windowStart":"2026-10-28T12:00:00Z","windowEnd":"2026-10-28T14:00:00Z"}`)
	if rec.Code != 200 {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body.String())
	}
	var created planWithItems
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Thursday focus" || len(created.Rows) != 2 {
		t.Fatalf("created plan: %+v", created)
	}

	work := created.Rows[0].ID
	rec = do(t, s, "POST", "/api/plans/"+created.ID+"/items", `{"rowId":"`+work+`","refSource":"github","refId":"u1","text":"review X","start":"2026-10-28T12:30:00Z","intendedHours":0.5}`)
	if rec.Code != 200 {
		t.Fatalf("add item status %d: %s", rec.Code, rec.Body.String())
	}
	if rec = do(t, s, "POST", "/api/plans/"+created.ID+"/items", `{"text":"investigate flaky test","start":null,"intendedHours":1.5}`); rec.Code != 200 {
		t.Fatalf("add anytime item status %d: %s", rec.Code, rec.Body.String())
	}

	plans := listPlans(t, s)
	if len(plans) != 1 || len(plans[0].Rows) != 2 || len(plans[0].Items) != 2 {
		t.Fatalf("plans: %+v", plans)
	}
	placed, anytime := plans[0].Items[0], plans[0].Items[1]
	if placed.RowID != work || placed.Start == nil || !placed.Start.Equal(time.Date(2026, 10, 28, 12, 30, 0, 0, time.UTC)) || placed.IntendedHours != 0.5 {
		t.Fatalf("placed item: %+v", placed)
	}
	if anytime.Start != nil || anytime.IntendedHours != 1.5 {
		t.Fatalf("anytime item: %+v", anytime)
	}
}

func TestPlannerEditsPlanRowsAndItems(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	var p planWithItems
	_ = json.Unmarshal(do(t, s, "POST", "/api/plans", `{"name":"a","windowStart":"2026-10-28T09:00:00Z","windowEnd":"2026-10-28T17:00:00Z"}`).Body.Bytes(), &p)

	if rec := do(t, s, "PATCH", "/api/plans/"+p.ID, `{"name":"b","windowStart":"2026-10-28T10:00:00Z","windowEnd":"2026-10-28T16:00:00Z"}`); rec.Code != 204 {
		t.Fatalf("patch plan status %d: %s", rec.Code, rec.Body.String())
	}

	rec := do(t, s, "POST", "/api/plans/"+p.ID+"/rows", `{"name":"Errands"}`)
	var row store.PlanRow
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil || rec.Code != 200 || row.Name != "Errands" {
		t.Fatalf("add row %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "PATCH", "/api/rows/"+row.ID, `{"name":"Life"}`); rec.Code != 204 {
		t.Fatalf("rename row status %d", rec.Code)
	}

	var it store.PlanItem
	_ = json.Unmarshal(do(t, s, "POST", "/api/plans/"+p.ID+"/items", `{"text":"New activity","intendedHours":1}`).Body.Bytes(), &it)
	if rec := do(t, s, "PATCH", "/api/items/"+it.ID, `{"rowId":"`+row.ID+`","start":"2026-10-28T11:00:00Z","intendedHours":1.25,"text":"Lunch"}`); rec.Code != 204 {
		t.Fatalf("patch item status %d: %s", rec.Code, rec.Body.String())
	}

	got := listPlans(t, s)[0]
	if got.Name != "b" || !got.WindowStart.Equal(time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("plan after patch: %+v", got.Plan)
	}
	if len(got.Rows) != 3 || got.Rows[2].Name != "Life" {
		t.Fatalf("rows: %+v", got.Rows)
	}
	if len(got.Items) != 1 || got.Items[0].RowID != row.ID || got.Items[0].Start == nil || got.Items[0].IntendedHours != 1.25 || got.Items[0].Text != "Lunch" {
		t.Fatalf("item after patch: %+v", got.Items)
	}

	if rec := do(t, s, "DELETE", "/api/rows/"+row.ID, ""); rec.Code != 204 {
		t.Fatalf("delete row status %d", rec.Code)
	}
	if got = listPlans(t, s)[0]; len(got.Rows) != 2 || len(got.Items) != 0 {
		t.Fatalf("deleting a row deletes its items: %+v", got)
	}
}

func TestPlannerBadRequests(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if rec := do(t, s, "POST", "/api/plans", "not json"); rec.Code != 400 {
		t.Fatalf("bad body -> 400, got %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/api/plans", `{"name":" ","windowStart":"2026-10-28T09:00:00Z","windowEnd":"2026-10-28T17:00:00Z"}`); rec.Code != 400 {
		t.Fatalf("blank plan name -> 400, got %d", rec.Code)
	}
	if rec := do(t, s, "PATCH", "/api/plans/nope", `{"name":"x","windowStart":"2026-10-28T09:00:00Z","windowEnd":"2026-10-28T17:00:00Z"}`); rec.Code != 404 {
		t.Fatalf("patch missing plan -> 404, got %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/api/plans/nope/items", `{"text":"x"}`); rec.Code != 404 {
		t.Fatalf("item to missing plan -> 404, got %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/api/plans/nope/rows", `{"name":"x"}`); rec.Code != 404 {
		t.Fatalf("row to missing plan -> 404, got %d", rec.Code)
	}
	if rec := do(t, s, "PATCH", "/api/items/x", `{"start":"not a time"}`); rec.Code != 400 {
		t.Fatalf("bad item start -> 400, got %d", rec.Code)
	}
	if rec := do(t, s, "DELETE", "/api/plans/nope", ""); rec.Code != 404 {
		t.Fatalf("delete missing plan -> 404, got %d", rec.Code)
	}
}

func TestPlannableFromGitHub(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if rec := do(t, s, "GET", "/api/plannable", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty plannable should be []: %q", rec.Body.String())
	}
	if err := s.store.ReplaceExternalItems("github", []store.ExternalItem{
		{Source: "github", Kind: "review_request", ExternalID: "u1", Title: "please review", Repo: "o/r", URL: "u1", ReviewRequestedOfMe: true, UpdatedAt: tnow},
		{Source: "github", Kind: "pr", ExternalID: "u2", Title: "my pr", URL: "u2", UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/api/plannable", "")
	var cands []plannableItem
	if err := json.Unmarshal(rec.Body.Bytes(), &cands); err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Text != "please review" || cands[0].Repo != "o/r" {
		t.Fatalf("plannable (only review_requests): %+v", cands)
	}
	if cands[0].Kind != "review_request" || cands[0].Hours != 0.5 {
		t.Fatalf("a review is half an hour: %+v", cands[0])
	}
}

func TestPlannableTicketsFromAnySourceWithHours(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "abc", Title: "Fix the bug", Repo: "ENG-1", Estimate: 3, AssignedToMe: true, UpdatedAt: tnow},
		{Source: "linear", Kind: "task", ExternalID: "def", Title: "Unestimated", Repo: "ENG-2", AssignedToMe: true, UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.ReplaceExternalItems("jira", []store.ExternalItem{
		{Source: "jira", Kind: "task", ExternalID: "OPS-1", Title: "Upgrade the worker", Repo: "OPS-1", Estimate: 2, UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	var cands []plannableItem
	if err := json.Unmarshal(do(t, s, "GET", "/api/plannable", "").Body.Bytes(), &cands); err != nil {
		t.Fatal(err)
	}
	hours := map[string]float64{}
	for _, c := range cands {
		if c.Kind != "task" {
			t.Fatalf("tickets keep kind task: %+v", c)
		}
		hours[c.RefSource+"/"+c.RefID] = c.Hours
	}
	// 1 point = 1 hour; an unestimated ticket is an hour
	if hours["linear/abc"] != 3 || hours["linear/def"] != 1 || hours["jira/OPS-1"] != 2 {
		t.Fatalf("ticket hours: %+v", hours)
	}
}

func TestPlanCapacityFromEvents(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	ws := time.Date(2026, 10, 28, 10, 0, 0, 0, time.Local)
	we := ws.Add(4 * time.Hour) // 4h window
	if err := s.store.CreatePlan(store.Plan{ID: "p1", WindowStart: ws, WindowEnd: we, CreatedAt: tnow, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	// a 1h meeting inside the window, one earlier that day, one the next day
	if err := s.store.ReplaceCalendarEvents([]store.CalendarEvent{
		{UID: "m", Title: "Standup", Start: ws.Add(time.Hour), End: ws.Add(2 * time.Hour)},
		{UID: "e", Title: "Early", Start: ws.Add(-2 * time.Hour), End: ws.Add(-time.Hour)},
		{UID: "n", Title: "Tomorrow", Start: ws.Add(24 * time.Hour), End: ws.Add(25 * time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	plans := listPlans(t, s)
	if len(plans) != 1 {
		t.Fatalf("plans: %+v", plans)
	}
	if plans[0].MeetingHours != 1 || plans[0].MakerHours != 3 {
		t.Fatalf("capacity: meeting=%v maker=%v (want 1, 3)", plans[0].MeetingHours, plans[0].MakerHours)
	}
	// the calendar track shows the whole day, not only the window
	if len(plans[0].Meetings) != 2 || plans[0].Meetings[0].Title != "Early" || plans[0].Meetings[1].Title != "Standup" {
		t.Fatalf("meetings: %+v", plans[0].Meetings)
	}
}

func TestPlannableIncludesLinearTasks(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "abc", Title: "Fix the bug", Repo: "ENG-1", AssignedToMe: true, UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/api/plannable", "")
	var cands []plannableItem
	if err := json.Unmarshal(rec.Body.Bytes(), &cands); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cands {
		if c.RefSource == "linear" && c.Text == "Fix the bug" && c.Repo == "ENG-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a linear task should be plannable: %+v", cands)
	}
}

func TestPlannableIncludesJiraTasks(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if err := s.store.ReplaceExternalItems("jira", []store.ExternalItem{
		{Source: "jira", Kind: "task", ExternalID: "ABC-1", Title: "Fix the Jira bug", Repo: "ABC-1", AssignedToMe: true, UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, "GET", "/api/plannable", "")
	var cands []plannableItem
	if err := json.Unmarshal(rec.Body.Bytes(), &cands); err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].RefSource != "jira" || cands[0].RefID != "ABC-1" || cands[0].Text != "Fix the Jira bug" {
		t.Fatalf("a jira task should be plannable: %+v", cands)
	}
}

func TestPlannableTicketsOrderedByStage(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	mk := func(id, title, state, stage string) store.ExternalItem {
		return store.ExternalItem{Source: "linear", Kind: "task", ExternalID: id, Title: title, State: state, Stage: stage, UpdatedAt: tnow}
	}
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		mk("none", "A", "Odd", ""),
		mk("back", "A", "Backlog", "backlog"),
		mk("todo", "A", "Todo", "unstarted"),
		mk("tri", "A", "Triage", "triage"),
		mk("prog-b", "B", "In Progress", "started"),
		mk("rev", "A", "In Review", "started"),
		mk("prog-a", "A", "In Progress", "started"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.ReplaceExternalItems("github", []store.ExternalItem{
		{Source: "github", Kind: "review_request", ExternalID: "r1", Title: "review", UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	var cands []plannableItem
	if err := json.Unmarshal(do(t, s, "GET", "/api/plannable", "").Body.Bytes(), &cands); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cands {
		if c.Kind == "task" {
			got = append(got, c.RefID)
		}
	}
	// stage rank, then the state name, then the title
	want := "prog-a prog-b rev todo tri back none"
	if strings.Join(got, " ") != want {
		t.Fatalf("ticket order: got %q, want %q", strings.Join(got, " "), want)
	}
	if cands[0].State != "In Progress" || cands[0].Stage != "started" {
		t.Fatalf("state and stage are returned: %+v", cands[0])
	}
	if len(cands) != 8 {
		t.Fatalf("the review stays plannable: %+v", cands)
	}
}

func TestPlannableTicketsCarryProjectAndLabels(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "a", Title: "A", URL: "https://linear.app/x/issue/ENG-1", Project: "Onboarding", Labels: []string{"bug"}, UpdatedAt: tnow},
		{Source: "linear", Kind: "task", ExternalID: "b", Title: "B", UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	body := do(t, s, "GET", "/api/plannable", "").Body.String()
	if !strings.Contains(body, `"URL":"https://linear.app/x/issue/ENG-1"`) || !strings.Contains(body, `"Project":"Onboarding","Labels":["bug"]`) || !strings.Contains(body, `"Project":"","Labels":[]`) {
		t.Fatalf("url, project and labels, never null: %s", body)
	}
}

func TestPlannableLeavesOutFinishedTickets(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "open", Stage: "started", UpdatedAt: tnow},
		{Source: "linear", Kind: "task", ExternalID: "done", Stage: "completed", UpdatedAt: tnow},
		{Source: "linear", Kind: "task", ExternalID: "gone", Stage: "canceled", UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	var cands []plannableItem
	if err := json.Unmarshal(do(t, s, "GET", "/api/plannable", "").Body.Bytes(), &cands); err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].RefID != "open" {
		t.Fatalf("finished tickets are not plannable: %+v", cands)
	}
}

func TestPlanItemsCarryTheirTicketState(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	ws := time.Date(2026, 10, 28, 9, 0, 0, 0, time.Local)
	if err := s.store.CreatePlan(store.Plan{ID: "p1", WindowStart: ws, WindowEnd: ws.Add(8 * time.Hour), CreatedAt: tnow, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	for _, it := range []store.PlanItem{
		{ID: "done", PlanID: "p1", RefSource: "linear", RefID: "a", Text: "Shipped", IntendedHours: 1},
		{ID: "lost", PlanID: "p1", RefSource: "jira", RefID: "OPS-9", Text: "No longer synced", IntendedHours: 1},
		{ID: "own", PlanID: "p1", Text: "Lunch", IntendedHours: 1},
	} {
		if err := s.store.AddPlanItem(it); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "a", State: "Done", Stage: "completed", UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	plans := listPlans(t, s)
	if len(plans) != 1 {
		t.Fatalf("plans: %+v", plans)
	}
	got := map[string][2]string{}
	for _, it := range plans[0].Items {
		got[it.ID] = [2]string{it.RefState, it.RefStage}
	}
	if got["done"] != [2]string{"Done", "completed"} || got["lost"] != [2]string{"", ""} || got["own"] != [2]string{"", ""} {
		t.Fatalf("ref state and stage: %+v", got)
	}
	if body := do(t, s, "GET", "/api/plans", "").Body.String(); strings.Count(body, `"RefStage":`) != 3 {
		t.Fatalf("RefStage must always be in the JSON: %s", body)
	}
}

func TestPlanItemsCarryTheirTicketKeyProjectAndLabels(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	ws := time.Date(2026, 10, 28, 9, 0, 0, 0, time.Local)
	if err := s.store.CreatePlan(store.Plan{ID: "p1", WindowStart: ws, WindowEnd: ws.Add(8 * time.Hour), CreatedAt: tnow, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	for _, it := range []store.PlanItem{
		{ID: "done", PlanID: "p1", RefSource: "linear", RefID: "a", Text: "Shipped", IntendedHours: 1},
		{ID: "own", PlanID: "p1", Text: "Lunch", IntendedHours: 1},
	} {
		if err := s.store.AddPlanItem(it); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "a", Repo: "ENG-1", URL: "https://linear.app/x/issue/ENG-1", Project: "Agent", Labels: []string{"bug"}, Stage: "completed", UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	items := listPlans(t, s)[0].Items
	got := map[string]planItem{}
	for _, it := range items {
		got[it.ID] = it
	}
	if d := got["done"]; d.RefRepo != "ENG-1" || d.RefURL != "https://linear.app/x/issue/ENG-1" || d.RefProject != "Agent" || strings.Join(d.RefLabels, ",") != "bug" {
		t.Fatalf("ticket key, url, project and labels: %+v", d)
	}
	if body := do(t, s, "GET", "/api/plans", "").Body.String(); !strings.Contains(body, `"RefLabels":[]`) {
		t.Fatalf("RefLabels must never be null: %s", body)
	}
}
