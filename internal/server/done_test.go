package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestItemDoneMarksAndClears(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	_ = s.store.CreatePlan(store.Plan{ID: "p", WindowStart: tnow, WindowEnd: tnow.Add(time.Hour), Status: "active"})
	_ = s.store.AddPlanItem(store.PlanItem{ID: "own", PlanID: "p", Text: "lunch"})
	_ = s.store.AddPlanItem(store.PlanItem{ID: "rev", PlanID: "p", RefSource: "github", RefID: "u"})

	if raw := listPlansRaw(t, s); !strings.Contains(raw, `"DoneAt":null`) {
		t.Fatalf("a not-done item has DoneAt null: %s", raw)
	}
	for _, id := range []string{"own", "rev"} {
		rec := do(t, s, "POST", "/api/items/"+id+"/done", `{"done":true}`)
		var it store.PlanItem
		if err := json.Unmarshal(rec.Body.Bytes(), &it); err != nil || rec.Code != 200 || it.DoneAt == nil || !it.DoneAt.Equal(tnow) {
			t.Fatalf("%s done: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	if items := listPlans(t, s)[0].Items; items[0].DoneAt == nil || items[1].DoneAt == nil {
		t.Fatalf("plans carry DoneAt: %+v", items)
	}
	rec := do(t, s, "POST", "/api/items/own/done", `{"done":false}`)
	var it store.PlanItem
	if err := json.Unmarshal(rec.Body.Bytes(), &it); err != nil || rec.Code != 200 || it.DoneAt != nil {
		t.Fatalf("not done: %d %s", rec.Code, rec.Body.String())
	}
}

func listPlansRaw(t *testing.T, s *Server) string {
	t.Helper()
	return do(t, s, "GET", "/api/plans", "").Body.String()
}

func TestItemDoneRefusals(t *testing.T) {
	tnow := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	_ = s.store.AddPlanItem(store.PlanItem{ID: "tk", PlanID: "p", RefSource: "linear", RefID: "abc"})
	if rec := do(t, s, "POST", "/api/items/nope/done", `{"done":true}`); rec.Code != 404 {
		t.Fatalf("unknown item: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/api/items/tk/done", `{"done":true}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "use /api/tickets/done") {
		t.Fatalf("ticket item: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/items/tk/done", `nope`); rec.Code != 400 {
		t.Fatalf("bad body: %d", rec.Code)
	}
}

func TestTicketDoneMovesLinearAndResyncs(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	_ = s.store.SetSetting("linear_api_key", "lin_api_test")
	var moves []string
	s.moveLinear = func(_ context.Context, key, id, stateType string) (string, error) {
		moves = append(moves, key+" "+id+" "+stateType)
		return map[string]string{"completed": "Done", "started": "In Progress"}[stateType], nil
	}
	synced := 0
	s.syncLinear = func(context.Context) (int, error) { synced++; return 1, nil }
	for _, c := range []struct{ done, want string }{{"true", "Done"}, {"false", "In Progress"}} {
		rec := do(t, s, "POST", "/api/tickets/done", `{"source":"linear","id":"abc","done":`+c.done+`}`)
		var out struct{ State string }
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 || out.State != c.want {
			t.Fatalf("done=%s: %d %s", c.done, rec.Code, rec.Body.String())
		}
	}
	if strings.Join(moves, ",") != "lin_api_test abc completed,lin_api_test abc started" || synced != 2 {
		t.Fatalf("moves %v, synced %d", moves, synced)
	}
}

func TestTicketDoneMovesJiraAndResyncs(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	for k, v := range map[string]string{"jira_site": "https://example.atlassian.net", "jira_email": "me@example.com", "jira_api_token": "jira_test_token"} {
		_ = s.store.SetSetting(k, v)
	}
	var moves []string
	s.moveJira = func(_ context.Context, site, email, token, key, category string) (string, error) {
		moves = append(moves, strings.Join([]string{site, email, token, key, category}, " "))
		return "Done", nil
	}
	synced := 0
	s.syncJira = func(context.Context) (int, error) { synced++; return 1, nil }
	if rec := do(t, s, "POST", "/api/tickets/done", `{"source":"jira","id":"OPS-1","done":true}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"State":"Done"`) {
		t.Fatalf("jira done: %d %s", rec.Code, rec.Body.String())
	}
	_ = do(t, s, "POST", "/api/tickets/done", `{"source":"jira","id":"OPS-1","done":false}`)
	want := "https://example.atlassian.net me@example.com jira_test_token OPS-1 done,https://example.atlassian.net me@example.com jira_test_token OPS-1 indeterminate"
	if strings.Join(moves, ",") != want || synced != 2 {
		t.Fatalf("moves %v, synced %d", moves, synced)
	}
}

func TestTicketDoneRefusals(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.moveLinear = func(context.Context, string, string, string) (string, error) {
		return "", errors.New("linear: unexpected status 401 lin_api_SECRET")
	}
	for body, code := range map[string]int{
		`nope`: 400,
		`{"source":"github","id":"x","done":true}`:   400,
		`{"source":"linear","id":"","done":true}`:    400,
		`{"source":"linear","id":"abc"}`:             400,
		`{"source":"linear","id":"abc","done":true}`: 400, // not configured
		`{"source":"jira","id":"OPS-1","done":true}`: 400,
	} {
		if rec := do(t, s, "POST", "/api/tickets/done", body); rec.Code != code {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	_ = s.store.SetSetting("linear_api_key", "lin_api_SECRET")
	rec := do(t, s, "POST", "/api/tickets/done", `{"source":"linear","id":"abc","done":true}`)
	if rec.Code != 502 || strings.Contains(rec.Body.String(), "SECRET") || strings.Contains(rec.Body.String(), "401") {
		t.Fatalf("a refused move is a short 502 with no credential or upstream detail: %d %s", rec.Code, rec.Body.String())
	}
}
