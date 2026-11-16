package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

func dayDeltaFor(id string, days map[string]facts.Day) store.Delta {
	return store.Delta{
		Project:    store.Project{Key: "k", Name: "k"},
		Session:    store.Session{ID: id, ProjectKey: "k"},
		Days:       days,
		CursorPath: id,
	}
}

func TestApplyAddsDayTimeAndWidensFirstLast(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := func(h, m, s, ns int) time.Time { return time.Date(2026, 10, 22, h, m, s, ns, time.UTC) }

	if err := st.Apply(dayDeltaFor("s1", map[string]facts.Day{"2026-10-22": {AISeconds: 10, First: at(9, 0, 0, 0), Last: at(10, 0, 0, 0)}})); err != nil {
		t.Fatal(err)
	}
	// As RFC3339Nano strings "10:00:00.5Z" sorts before "10:00:00Z", so the
	// widening must compare times, not strings.
	if err := st.Apply(dayDeltaFor("s1", map[string]facts.Day{
		"2026-10-22": {AISeconds: 5, First: at(9, 0, 0, 500_000_000), Last: at(10, 0, 0, 500_000_000)},
		"2026-10-23": {AISeconds: 1, First: at(23, 0, 0, 0), Last: at(23, 0, 0, 0)},
	})); err != nil {
		t.Fatal(err)
	}
	if err := st.Apply(dayDeltaFor("s2", map[string]facts.Day{"2026-10-22": {AISeconds: 2, First: at(11, 0, 0, 0), Last: at(11, 0, 0, 0)}})); err != nil {
		t.Fatal(err)
	}

	got, err := st.SessionDays("2026-10-22")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want s1 and s2, got %+v", got)
	}
	s1 := got["s1"]
	if s1.AISeconds != 15 || !s1.First.Equal(at(9, 0, 0, 0)) || !s1.Last.Equal(at(10, 0, 0, 500_000_000)) {
		t.Fatalf("s1: %+v", s1)
	}
	if got["s2"].AISeconds != 2 {
		t.Fatalf("s2: %+v", got["s2"])
	}
}

func TestApplyAddsHourBuckets(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var a, b [24]float64
	a[9], b[9], b[14] = 60, 30, 120
	for _, h := range [][24]float64{a, b} {
		if err := st.Apply(dayDeltaFor("s1", map[string]facts.Day{"2026-10-22": {AISeconds: h[9] + h[14], Hours: h}})); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.SessionDays("2026-10-22")
	if err != nil {
		t.Fatal(err)
	}
	if h := got["s1"].Hours; h[9] != 90 || h[14] != 120 {
		t.Fatalf("hours: %v", h)
	}
}

func TestMonthDaysSumsAITimePerDay(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, d := range []store.Delta{
		dayDeltaFor("s1", map[string]facts.Day{"2026-10-01": {AISeconds: 100}, "2026-11-01": {AISeconds: 7}}),
		dayDeltaFor("s2", map[string]facts.Day{"2026-10-01": {AISeconds: 50}, "2026-10-31": {AISeconds: 3}}),
	} {
		if err := st.Apply(d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.MonthDays("2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["2026-10-01"] != 150 || got["2026-10-31"] != 3 {
		t.Fatalf("got %v", got)
	}
}
