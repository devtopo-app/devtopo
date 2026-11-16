package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestCalendarEventsWindow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ws := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	we := ws.Add(4 * time.Hour) // 10:00-14:00
	before := store.CalendarEvent{UID: "b", Title: "before", Start: ws.Add(-2 * time.Hour), End: ws.Add(-time.Hour)}
	overlap := store.CalendarEvent{UID: "o", Title: "overlap", Start: ws.Add(-30 * time.Minute), End: ws.Add(30 * time.Minute)}
	inside := store.CalendarEvent{UID: "i", Title: "inside", Start: ws.Add(time.Hour), End: ws.Add(2 * time.Hour)}
	if err := st.ReplaceCalendarEvents([]store.CalendarEvent{before, overlap, inside}); err != nil {
		t.Fatal(err)
	}
	got, err := st.EventsInWindow(ws, we)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UID != "o" || got[1].UID != "i" {
		t.Fatalf("window should return the 2 overlapping events, ordered by start: %+v", got)
	}

	if err := st.ReplaceCalendarEvents([]store.CalendarEvent{inside}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.EventsInWindow(ws, we); len(got) != 1 || got[0].UID != "i" {
		t.Fatalf("replace should fully swap: %+v", got)
	}
}
