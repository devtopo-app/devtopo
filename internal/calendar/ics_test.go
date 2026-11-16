package calendar

import (
	"strings"
	"testing"
	"time"
)

var (
	hs = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	he = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
)

// cal wraps VEVENT bodies (lines without CRLF) into a feed like Google's.
func cal(events ...[]string) []byte {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n")
	for _, ev := range events {
		b.WriteString("BEGIN:VEVENT\r\n")
		for _, l := range ev {
			b.WriteString(l + "\r\n")
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return []byte(b.String())
}

func TestParseTimedEvent(t *testing.T) {
	evs := mustParse(t, cal([]string{"UID:a", "SUMMARY:Standup", "DTSTART:20261028T100000Z", "DTEND:20261028T101500Z"}), hs, he)
	if len(evs) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(evs), evs)
	}
	e := evs[0]
	if e.Title != "Standup" || e.UID != "a" {
		t.Fatalf("event: %+v", e)
	}
	if !e.Start.Equal(time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("start: %v", e.Start)
	}
	if e.End.Sub(e.Start) != 15*time.Minute {
		t.Fatalf("duration: %v", e.End.Sub(e.Start))
	}
}

func TestParseFolded(t *testing.T) {
	evs := mustParse(t, cal([]string{"UID:b", "SUMMARY:Long title that is", "  folded", "DTSTART:20261028T100000Z", "DTEND:20261028T110000Z"}), hs, he)
	if len(evs) != 1 || evs[0].Title != "Long title that is folded" {
		t.Fatalf("unfold failed: %+v", evs)
	}
}

// A meeting set in another time zone lands at its real instant: 09:30 in Sao
// Paulo (UTC-3) is 12:30 UTC, not 09:30 UTC.
func TestParseHonoursTZID(t *testing.T) {
	evs := mustParse(t, cal([]string{"UID:tz", "SUMMARY:1on1", "DTSTART;TZID=America/Sao_Paulo:20261029T093000", "DTEND;TZID=America/Sao_Paulo:20261029T100000"}), hs, he)
	if len(evs) != 1 || !evs[0].Start.Equal(time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)) {
		t.Fatalf("want 12:30 UTC, got %+v", evs)
	}
}

// All-day events (birthdays, out-of-office) take no maker time.
func TestParseSkipsAllDayAndCancelled(t *testing.T) {
	evs := mustParse(t, cal(
		[]string{"UID:ad", "SUMMARY:Holiday", "DTSTART;VALUE=DATE:20261028", "DTEND;VALUE=DATE:20261029"},
		[]string{"UID:cx", "SUMMARY:Called off", "STATUS:CANCELLED", "DTSTART:20261028T100000Z", "DTEND:20261028T110000Z"},
	), hs, he)
	if len(evs) != 0 {
		t.Fatalf("all-day and cancelled events must be skipped: %+v", evs)
	}
}

func TestParseWeeklyRecurrence(t *testing.T) {
	evs := mustParse(t, cal([]string{"UID:c", "SUMMARY:Weekly", "DTSTART:20261007T090000Z", "DTEND:20261007T093000Z", "RRULE:FREQ=WEEKLY;COUNT=3"}), hs, he)
	if len(evs) != 3 {
		t.Fatalf("want 3 occurrences, got %d: %+v", len(evs), evs)
	}
	if !evs[1].Start.Equal(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("2nd occurrence: %v", evs[1].Start)
	}
	if evs[2].End.Sub(evs[2].Start) != 30*time.Minute {
		t.Fatalf("occurrence duration: %v", evs[2].End.Sub(evs[2].Start))
	}
}

func TestParseByDay(t *testing.T) {
	evs := mustParse(t, cal([]string{"UID:bd", "SUMMARY:Sync", "DTSTART:20261005T090000Z", "DTEND:20261005T091500Z", "RRULE:FREQ=WEEKLY;BYDAY=MO,WE;COUNT=4"}), hs, he)
	if len(evs) != 4 || evs[1].Start.Weekday() != time.Wednesday {
		t.Fatalf("want Mon and Wed occurrences, got %+v", evs)
	}
}

func TestParseExdateSkipsAnOccurrence(t *testing.T) {
	evs := mustParse(t, cal([]string{"UID:ex", "SUMMARY:Daily", "DTSTART:20261007T090000Z", "DTEND:20261007T091000Z", "RRULE:FREQ=DAILY;COUNT=3", "EXDATE:20261008T090000Z"}), hs, he)
	if len(evs) != 2 || evs[1].Start.Day() != 9 {
		t.Fatalf("EXDATE must drop the 8th: %+v", evs)
	}
}

// One moved occurrence of a series comes as its own VEVENT with RECURRENCE-ID:
// it replaces the series' occurrence instead of showing next to it.
func TestParseOverrideReplacesItsOccurrence(t *testing.T) {
	evs := mustParse(t, cal(
		[]string{"UID:s", "SUMMARY:1on1", "DTSTART;TZID=America/Sao_Paulo:20261022T093000", "DTEND;TZID=America/Sao_Paulo:20261022T100000", "RRULE:FREQ=WEEKLY;COUNT=3"},
		[]string{"UID:s", "SUMMARY:1on1", "RECURRENCE-ID;TZID=America/Sao_Paulo:20261029T093000", "DTSTART;TZID=America/Sao_Paulo:20261029T110000", "DTEND;TZID=America/Sao_Paulo:20261029T113000"},
	), hs, he)
	var on29 []Event
	for _, e := range evs {
		if e.Start.Day() == 29 {
			on29 = append(on29, e)
		}
	}
	if len(evs) != 3 || len(on29) != 1 || !on29[0].Start.Equal(time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("want 3 events, the 29th once at 14:00 UTC: %+v", evs)
	}
}

func TestParseUntilStops(t *testing.T) {
	evs := mustParse(t, cal([]string{"UID:d", "SUMMARY:Daily", "DTSTART:20261007T090000Z", "DTEND:20261007T091000Z", "RRULE:FREQ=DAILY;UNTIL=20261009T090000Z"}), hs, he)
	if len(evs) != 3 { // 10/7, 10/8, 10/9
		t.Fatalf("UNTIL should stop at 3, got %d: %+v", len(evs), evs)
	}
}

func TestParseFarPastDailyRecurrence(t *testing.T) {
	// A daily standup whose series started years before the horizon - Google's
	// iCal emits the original DTSTART, not a pre-expanded series.
	start := hs.AddDate(-6, 0, 0)
	evs := mustParse(t, cal([]string{"UID:e", "SUMMARY:Standup",
		"DTSTART:" + start.Format("20060102T150405Z"),
		"DTEND:" + start.Add(15*time.Minute).Format("20060102T150405Z"),
		"RRULE:FREQ=DAILY"}), hs, he)
	if len(evs) == 0 {
		t.Fatal("a far-past daily series must still yield its in-horizon occurrences")
	}
	for _, e := range evs {
		if e.Start.Before(hs) || !e.Start.Before(he) {
			t.Fatalf("occurrence outside the horizon: %v", e.Start)
		}
	}
}

func TestParseMalformedIsSafe(t *testing.T) {
	if evs, err := Parse([]byte("not an ics\r\nBEGIN:VEVENT\r\nSUMMARY:no start\r\nEND:VEVENT\r\n"), hs, he); err == nil || len(evs) != 0 {
		t.Fatalf("garbage must be an error and yield nothing: %+v %v", evs, err)
	}
	if evs := mustParse(t, cal([]string{"UID:x", "SUMMARY:no start"}), hs, he); len(evs) != 0 {
		t.Fatalf("a VEVENT missing DTSTART must be skipped: %+v", evs)
	}
}

// A sign-in page answered with 200, an empty body, or a feed cut off part-way
// is not a calendar: Parse must say so rather than return no events.
func TestParseRejectsWhatIsNotACalendar(t *testing.T) {
	full := string(cal([]string{"UID:a", "SUMMARY:Standup", "DTSTART:20261028T100000Z", "DTEND:20261028T101500Z"}))
	for name, body := range map[string]string{
		"html":      "<!DOCTYPE html><html><head><title>Sign in</title></head><body>Sign in</body></html>",
		"empty":     "",
		"truncated": full[:len(full)/2],
		"no end":    strings.TrimSuffix(full, "END:VCALENDAR\r\n"),
	} {
		if evs, err := Parse([]byte(body), hs, he); err == nil {
			t.Errorf("%s: want an error, got %+v", name, evs)
		}
	}
}

// A valid calendar with no events is not an error: it really is empty. A
// leading byte-order mark is tolerated.
func TestParseEmptyCalendarIsNotAnError(t *testing.T) {
	if evs := mustParse(t, cal(), hs, he); len(evs) != 0 {
		t.Fatalf("want no events, got %+v", evs)
	}
	if evs := mustParse(t, append([]byte("\xef\xbb\xbf"), cal()...), hs, he); len(evs) != 0 {
		t.Fatalf("want no events, got %+v", evs)
	}
}

func mustParse(t *testing.T, ics []byte, from, to time.Time) []Event {
	t.Helper()
	evs, err := Parse(ics, from, to)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}
