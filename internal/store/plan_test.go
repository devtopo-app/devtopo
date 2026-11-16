package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func openPlanStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestPlanLifecycle(t *testing.T) {
	st := openPlanStore(t)
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := st.CreatePlan(store.Plan{ID: "p1", Name: "Focus", WindowStart: now, WindowEnd: now.Add(2 * time.Hour), CreatedAt: now, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.GetPlanRows("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "Work plan" || rows[1].Name != "Private activities" {
		t.Fatalf("a new plan starts with the two default rows: %+v", rows)
	}
	work := rows[0].ID
	start := now.Add(30 * time.Minute)
	if err := st.AddPlanItem(store.PlanItem{ID: "i1", PlanID: "p1", RowID: work, RefSource: "github", RefID: "u1", Text: "review X", Start: &start, IntendedHours: 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPlanItem(store.PlanItem{ID: "i2", PlanID: "p1", Text: "dentist", IntendedHours: 1}); err != nil {
		t.Fatal(err)
	}
	items, err := st.GetPlanItems("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "i1" || items[1].ID != "i2" {
		t.Fatalf("items: %+v", items)
	}
	if items[0].RowID != work || items[0].Start == nil || !items[0].Start.Equal(start) || items[0].IntendedHours != 0.5 {
		t.Fatalf("placed item: %+v", items[0])
	}
	if items[1].Start != nil {
		t.Fatalf("an item without a start is an anytime item: %+v", items[1])
	}

	// move the anytime item onto a new row, rename it and give it 2h
	if err := st.AddPlanRow(store.PlanRow{ID: "r3", PlanID: "p1", Name: "Errands", Order: 2}); err != nil {
		t.Fatal(err)
	}
	i2 := items[1]
	i2.RowID, i2.Start, i2.Text, i2.IntendedHours = "r3", &now, "dentist trip", 2
	if err := st.UpdatePlanItem(i2); err != nil {
		t.Fatal(err)
	}
	if items, _ = st.GetPlanItems("p1"); items[1].RowID != "r3" || items[1].Start == nil || items[1].Text != "dentist trip" || items[1].IntendedHours != 2 {
		t.Fatalf("update: %+v", items[1])
	}

	if err := st.RenamePlanRow("r3", "Life"); err != nil {
		t.Fatal(err)
	}
	if rows, _ = st.GetPlanRows("p1"); len(rows) != 3 || rows[2].Name != "Life" {
		t.Fatalf("rename row: %+v", rows)
	}
	if err := st.DeletePlanRow("r3"); err != nil {
		t.Fatal(err)
	}
	if rows, _ = st.GetPlanRows("p1"); len(rows) != 2 {
		t.Fatalf("delete row: %+v", rows)
	}
	if items, _ = st.GetPlanItems("p1"); len(items) != 1 || items[0].ID != "i1" {
		t.Fatalf("deleting a row deletes its items: %+v", items)
	}

	if err := st.UpdatePlan(store.Plan{ID: "p1", Name: "Deep work", WindowStart: now.Add(time.Hour), WindowEnd: now.Add(3 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	plans, _ := st.ListPlans("active")
	if len(plans) != 1 || plans[0].Name != "Deep work" || !plans[0].WindowStart.Equal(now.Add(time.Hour)) || plans[0].Status != "active" {
		t.Fatalf("update plan: %+v", plans)
	}

	if err := st.DeletePlanItem("i1"); err != nil {
		t.Fatal(err)
	}
	if items, _ = st.GetPlanItems("p1"); len(items) != 0 {
		t.Fatalf("after delete item: %d", len(items))
	}
	if err := st.DeletePlan("p1"); err != nil {
		t.Fatal(err)
	}
	if plans, _ := st.ListPlans("active"); len(plans) != 0 {
		t.Fatal("plan not deleted")
	}
	if rows, _ = st.GetPlanRows("p1"); len(rows) != 0 {
		t.Fatalf("rows not cascaded on plan delete: %d", len(rows))
	}
}

func TestListPlansByWindowStart(t *testing.T) {
	st := openPlanStore(t)
	day := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	for _, p := range []store.Plan{
		{ID: "later", WindowStart: day.Add(24 * time.Hour), WindowEnd: day.Add(25 * time.Hour), CreatedAt: day, Status: "active"},
		{ID: "sooner", WindowStart: day, WindowEnd: day.Add(time.Hour), CreatedAt: day.Add(time.Minute), Status: "active"},
	} {
		if err := st.CreatePlan(p); err != nil {
			t.Fatal(err)
		}
	}
	if plans, _ := st.ListPlans(""); len(plans) != 2 || plans[0].ID != "sooner" {
		t.Fatalf("plans read left to right by day: %+v", plans)
	}
}

func TestArchivePastPlans(t *testing.T) {
	st := openPlanStore(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if err := st.CreatePlan(store.Plan{ID: "past", WindowStart: now.Add(-27 * time.Hour), WindowEnd: now.Add(-25 * time.Hour), CreatedAt: now.Add(-27 * time.Hour), Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreatePlan(store.Plan{ID: "future", WindowStart: now, WindowEnd: now.Add(2 * time.Hour), CreatedAt: now, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPlanItem(store.PlanItem{ID: "fi", PlanID: "future", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	n, err := st.ArchivePastPlans(now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("archived %d, want 1", n)
	}
	if active, _ := st.ListPlans("active"); len(active) != 1 || active[0].ID != "future" {
		t.Fatalf("active plans: %+v", active)
	}
	if past, _ := st.ListPlans("past"); len(past) != 1 || past[0].ID != "past" {
		t.Fatalf("past plans: %+v", past)
	}
	if items, _ := st.GetPlanItems("future"); len(items) != 1 {
		t.Fatalf("active plan's items wiped by archive: %d", len(items))
	}
}

// A plan stays on screen for the whole of its day, even after its window ends:
// only plans from an earlier day are archived.
func TestArchiveKeepsTodaysEndedPlans(t *testing.T) {
	st := openPlanStore(t)
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.Local) }
	now := day(28, 20)
	for id, start := range map[string]time.Time{"today": day(28, 9), "yesterday": day(27, 9)} {
		if err := st.CreatePlan(store.Plan{ID: id, WindowStart: start, WindowEnd: start.Add(9 * time.Hour), CreatedAt: start, Status: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ArchivePastPlans(now); err != nil {
		t.Fatal(err)
	}
	if active, _ := st.ListPlans("active"); len(active) != 1 || active[0].ID != "today" {
		t.Fatalf("today's plan must stay active after its window ends: %+v", active)
	}
}

// Moving a plan to another day takes its placed blocks along, in one step on
// the server; changing only the window on the same day leaves them alone; and
// an archived plan that is edited comes back.
func TestUpdatePlanMovesItemsWithTheDay(t *testing.T) {
	st := openPlanStore(t)
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.Local) }
	if err := st.CreatePlan(store.Plan{ID: "p", Name: "P", WindowStart: at(29, 9), WindowEnd: at(29, 18), CreatedAt: at(29, 9), Status: "active"}); err != nil {
		t.Fatal(err)
	}
	ten := at(29, 10)
	if err := st.AddPlanItem(store.PlanItem{ID: "timed", PlanID: "p", Text: "x", Start: &ten, IntendedHours: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPlanItem(store.PlanItem{ID: "anytime", PlanID: "p", Text: "y", IntendedHours: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ArchivePastPlans(at(31, 0)); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePlan(store.Plan{ID: "p", Name: "P", WindowStart: at(31, 9), WindowEnd: at(31, 18)}); err != nil {
		t.Fatal(err)
	}
	startOf := func() map[string]*time.Time {
		items, _ := st.GetPlanItems("p")
		out := map[string]*time.Time{}
		for _, it := range items {
			out[it.ID] = it.Start
		}
		return out
	}
	if s := startOf(); s["timed"] == nil || !s["timed"].Equal(at(31, 10)) || s["anytime"] != nil {
		t.Fatalf("blocks must move with the day, anytime stays anytime: %v %v", s["timed"], s["anytime"])
	}
	if err := st.UpdatePlan(store.Plan{ID: "p", Name: "P", WindowStart: at(31, 8), WindowEnd: at(31, 18)}); err != nil {
		t.Fatal(err)
	}
	if s := startOf(); !s["timed"].Equal(at(31, 10)) {
		t.Fatalf("a window change on the same day must not move blocks: %v", s["timed"])
	}
	if active, _ := st.ListPlans("active"); len(active) != 1 {
		t.Fatal("an edited archived plan must come back")
	}
}

func TestPlanItemDone(t *testing.T) {
	st := openPlanStore(t)
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if err := st.AddPlanItem(store.PlanItem{ID: "i1", PlanID: "p1", Text: "dentist", IntendedHours: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.GetPlanItem("nope"); err != nil || ok {
		t.Fatalf("unknown item: ok=%v err=%v", ok, err)
	}
	if err := st.SetPlanItemDone("i1", &now); err != nil {
		t.Fatal(err)
	}
	// Editing the block keeps its done state.
	if err := st.UpdatePlanItem(store.PlanItem{ID: "i1", Text: "dentist", IntendedHours: 2}); err != nil {
		t.Fatal(err)
	}
	it, ok, err := st.GetPlanItem("i1")
	if err != nil || !ok || it.DoneAt == nil || !it.DoneAt.Equal(now) || it.Text != "dentist" {
		t.Fatalf("done item: %+v ok=%v err=%v", it, ok, err)
	}
	if err := st.SetPlanItemDone("i1", nil); err != nil {
		t.Fatal(err)
	}
	if items, _ := st.GetPlanItems("p1"); len(items) != 1 || items[0].DoneAt != nil {
		t.Fatalf("not done again: %+v", items)
	}
}
