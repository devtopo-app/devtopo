package calendar

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	cases := []struct{ ics, want string }{
		{"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nX-WR-CALNAME:Work\r\nBEGIN:VEVENT\r\nX-WR-CALNAME:not this\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", "Work"},
		{"BEGIN:VCALENDAR\nX-WR-CALNAME;VALUE=TEXT:Team" + `\` + ", Platform\nEND:VCALENDAR\n", "Team, Platform"},
		{"BEGIN:VCALENDAR\r\nX-WR-CALNAME:A very long calendar na\r\n me that is folded\r\nEND:VCALENDAR\r\n", "A very long calendar name that is folded"},
		{"BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nX-WR-CALNAME:inside an event\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", ""},
		{"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n", ""},
		{"<html>sign in</html>", ""},
	}
	for _, c := range cases {
		if got := Name([]byte(c.ics)); got != c.want {
			t.Errorf("Name(%q) = %q, want %q", c.ics, got, c.want)
		}
	}
}

func TestSyncStoresTheCalendarsOwnName(t *testing.T) {
	st := openStore(t)
	if _, err := st.AddCalendarFeed("", "https://x/private.ics"); err != nil {
		t.Fatal(err)
	}
	named := strings.Replace(sampleICS, "PRODID:-//test//EN\r\n", "PRODID:-//test//EN\r\nX-WR-CALNAME:Team calendar\r\n", 1)
	if _, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(named)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	feeds, _ := st.CalendarFeeds()
	if len(feeds) != 1 || feeds[0].Name != "Team calendar" || feeds[0].Label != "" {
		t.Fatalf("feeds: %+v", feeds)
	}
}
