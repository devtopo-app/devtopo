// Package calendar fetches and parses a private iCal feed into meeting events.
// Parsing is go-ical's; recurrence is rrule-go's, so time zones (TZID), every
// RRULE form, EXDATE and moved occurrences (RECURRENCE-ID) come out right.
// All-day and cancelled events are left out: they take no maker time.
package calendar

import (
	"bytes"
	"errors"
	"strings"
	"time"
	_ "time/tzdata" // TZIDs resolve without a zoneinfo database on the machine

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// Event is one meeting occurrence.
type Event struct {
	UID   string
	Title string
	Start time.Time
	End   time.Time
}

// ErrNotCalendar means the feed body is not an iCalendar document (a login
// page, an error page, a truncated download). Callers must keep what they had.
var ErrNotCalendar = errors.New("calendar: the feed is not a valid iCalendar document")

// Name returns the calendar's own name from its X-WR-CALNAME line (Google
// Calendar, Outlook and Apple all write one), or "" when there is none. It
// reads only the calendar's header, before the first component.
func Name(ics []byte) string {
	var cur strings.Builder
	name := ""
	flush := func() bool {
		line := cur.String()
		cur.Reset()
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return false
		}
		if k, _, _ := strings.Cut(key, ";"); strings.EqualFold(k, "X-WR-CALNAME") {
			name = val
			return true
		}
		return strings.EqualFold(line, "BEGIN:VEVENT") || strings.EqualFold(line, "BEGIN:VTIMEZONE")
	}
	for _, raw := range strings.Split(string(ics), "\n") {
		raw = strings.TrimSuffix(raw, "\r")
		if strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
			cur.WriteString(raw[1:]) // a folded continuation line
			continue
		}
		if cur.Len() > 0 && flush() {
			break
		}
		cur.WriteString(raw)
	}
	if name == "" && cur.Len() > 0 {
		flush()
	}
	r := strings.NewReplacer(`\,`, ",", `\;`, ";", `\n`, " ", `\N`, " ", `\\`, `\`)
	return strings.TrimSpace(r.Replace(name))
}

// Parse reads the ICS bytes and returns the occurrences that overlap
// [horizonStart, horizonEnd), in UTC. A body that is not a whole VCALENDAR
// is an error; within a valid calendar, an event that cannot be read is
// skipped. A valid calendar with no events yields (nil, nil).
func Parse(ics []byte, horizonStart, horizonEnd time.Time) ([]Event, error) {
	body := bytes.TrimSpace(bytes.TrimPrefix(ics, []byte("\xef\xbb\xbf")))
	if len(body) < len("BEGIN:VCALENDAR") || !strings.EqualFold(string(body[:len("BEGIN:VCALENDAR")]), "BEGIN:VCALENDAR") {
		return nil, ErrNotCalendar
	}
	cal, err := ical.NewDecoder(bytes.NewReader(body)).Decode()
	if err != nil || cal == nil || !strings.EqualFold(cal.Name, ical.CompCalendar) {
		return nil, ErrNotCalendar
	}
	events := cal.Events()

	// A moved or edited occurrence of a series is its own VEVENT carrying the
	// series' UID and a RECURRENCE-ID: it stands in for that occurrence.
	type occ struct {
		uid string
		at  int64
	}
	replaced := map[occ]bool{}
	for _, e := range events {
		if rid := e.Props.Get(ical.PropRecurrenceID); rid != nil {
			if t, ok := when(rid); ok {
				replaced[occ{uid(e), t.Unix()}] = true
			}
		}
	}

	var out []Event
	for _, e := range events {
		ds := e.Props.Get(ical.PropDateTimeStart)
		if ds == nil || ds.ValueType() == ical.ValueDate || len(ds.Value) == 8 {
			continue // no start, or all-day
		}
		if s, _ := e.Props.Text(ical.PropStatus); strings.EqualFold(s, "CANCELLED") {
			continue
		}
		start, ok := when(ds)
		if !ok {
			continue
		}
		dur := time.Hour
		if de := e.Props.Get(ical.PropDateTimeEnd); de != nil {
			if end, ok := when(de); ok && end.After(start) {
				dur = end.Sub(start)
			}
		}
		id := uid(e)
		title, _ := e.Props.Text(ical.PropSummary)

		starts := []time.Time{start}
		if e.Props.Get(ical.PropRecurrenceRule) != nil && e.Props.Get(ical.PropRecurrenceID) == nil {
			if starts = occurrences(e, start, horizonStart.Add(-dur), horizonEnd); starts == nil {
				continue
			}
		}
		for _, s := range starts {
			if replaced[occ{id, s.Unix()}] && e.Props.Get(ical.PropRecurrenceID) == nil {
				continue
			}
			if s.Before(horizonEnd) && s.Add(dur).After(horizonStart) {
				out = append(out, Event{UID: id, Title: title, Start: s.UTC(), End: s.Add(dur).UTC()})
			}
		}
	}
	return out, nil
}

// occurrences expands e's RRULE from start, less its EXDATEs, within [from, to].
// go-ical's own RecurrenceSet is not used: it adds the EXDATEs back as RDATEs.
func occurrences(e ical.Event, start, from, to time.Time) []time.Time {
	opt, err := e.Props.RecurrenceRule()
	if err != nil || opt == nil {
		return nil
	}
	opt.Dtstart = start
	r, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil
	}
	var set rrule.Set
	set.RRule(r)
	for _, ex := range e.Props[ical.PropExceptionDates] {
		// One EXDATE line may list several instants.
		for _, v := range strings.Split(ex.Value, ",") {
			p := ex
			p.Value = v
			if t, ok := when(&p); ok {
				set.ExDate(t)
			}
		}
	}
	return set.Between(from, to, true)
}

// when reads a date-time in its TZID, or in local time when it is floating or
// names a zone Go does not know.
func when(p *ical.Prop) (time.Time, bool) {
	if t, err := p.DateTime(time.Local); err == nil {
		return t, true
	}
	q := *p
	q.Params = ical.Params{}
	t, err := q.DateTime(time.Local)
	return t, err == nil
}

func uid(e ical.Event) string {
	s, _ := e.Props.Text(ical.PropUID)
	return s
}
