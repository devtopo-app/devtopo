// Package server renders the DevTopo today surface over localhost HTTP. It is
// read-mostly: it reads the local store and shows sessions grouped into lanes,
// with the day's AI-hours and what each session did today. It never writes
// session data, and calls the narrator only for a detailed summary on demand.
package server

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
	"github.com/devtopo-app/devtopo/internal/watch"
)

// Card is one session as shown on the surface.
type Card struct {
	ID string
	// Name is what the user called the session; "" until they rename it.
	Name string
	// Agent is the agent running the session, "claude" or "codex", and Title
	// the name that agent gave it ("" when it gives none).
	Agent string
	Title string
	// RunningAgents names the session's sub-agents working now, newest first.
	RunningAgents []string
	// Host is the app the session runs in, or last ran in when closed ("" when
	// unknown); Live says a claude process is running it now.
	Host string
	Live bool
	// CanFocus says DevTopo can bring the session's app to the front on this
	// machine; it cannot on Linux, where a closed session is only resumed.
	CanFocus     bool
	Project      string
	Branch       string
	Status       string
	AIHours      float64
	WallHours    float64
	Prompts      int
	Decisions    int // questions the human answered today
	LastActivity time.Time
	// FinishedAt is the newest real event of the session or its sub-agents, which is
	// when the agent stopped once it isn't running. Zero when there is none.
	FinishedAt time.Time
	// BackgroundTasks counts the background tasks the session's agent left running.
	BackgroundTasks int
	// WorkingSince is when a running session's current task started: the last prompt,
	// or when the agent picked up again on a background task's news. The UI counts on
	// from it. Zero when not running or unknown.
	WorkingSince time.Time
	// TodayPoints is what the session did on the view's day, as at most ten
	// short points, and TodayAsOf the activity they cover. nil and zero when
	// there is no summary for the day yet.
	TodayPoints []string
	TodayAsOf   time.Time
	// Signals are per-type fact counts for hover detail, limited to the
	// whitelisted types. It never includes tool_call (honesty guardrail).
	Signals map[string]int
}

// Lane is a group of cards under one status heading.
type Lane struct {
	Title  string
	Status string
	Cards  []Card
}

// ReviewItem is one pull request whose review is requested of you.
type ReviewItem struct {
	Title     string
	Repo      string
	URL       string
	UpdatedAt time.Time
}

// FinishedTicket is a ticket from any tracker completed on the view's day.
type FinishedTicket struct {
	Source      string
	ID          string // the external id the ticket endpoints take
	Key         string
	Title       string
	URL         string
	Project     string
	CompletedAt time.Time
}

// ReviewDone is one PR review you submitted on a code host.
type ReviewDone struct {
	Title       string
	Repo        string
	URL         string
	SubmittedAt time.Time
}

// StartedTicket is a ticket that entered its started stage on the view's day.
// Hours is its estimate, or 1 when unestimated.
type StartedTicket struct {
	Source    string
	ID        string // the external id the ticket endpoints take
	Key       string
	Title     string
	URL       string
	Project   string
	State     string
	Stage     string
	StartedAt time.Time
	Hours     float64
}

// TodayView is the whole surface: the status lanes, the day's totals, reviews
// requested of you, tickets finished today, and a plain orienting sentence.
type TodayView struct {
	Running      Lane
	Waiting      Lane
	Idle         Lane
	DayAIHours   float64
	DaySpanHours float64
	DaySessions  int          // sessions active today - the population DayAIHours sums
	Reviews      []ReviewItem // reviews requested of you
	ContribPRs   int          // PRs you authored - a plain contribution figure
	// FinishedToday lists tickets completed today, newest first; never null.
	FinishedToday []FinishedTicket
	// PromptsToday counts the prompts typed today across all sessions.
	PromptsToday int
	// ReviewsDone and StartedToday are newest first; never null.
	ReviewsDone  []ReviewDone
	StartedToday []StartedTicket
	// YourPRs are your open PRs with a review, newest review first; never null.
	YourPRs    []YourPR
	StateOfNow string
	AsOf       time.Time
}

// sameLocalDay reports whether t falls on the same calendar date as now, in
// now's location. Stored times are UTC; this converts before comparing.
func sameLocalDay(t, now time.Time) bool {
	if t.IsZero() {
		return false
	}
	ty, tm, td := t.In(now.Location()).Date()
	ny, nm, nd := now.Date()
	return ty == ny && tm == nm && td == nd
}

// BuildView turns the stored sessions, their day summaries, per-session prompt
// counts, and today's per-session activity (days, keyed by session id) into the
// today surface as of now. Lane status is computed live from
// watch.SessionStatus, so the view is current whatever the stored status column
// says. Every hour figure comes from today's activity only, so a session
// carried over from a prior day counts just what it did today.
func BuildView(sessions []store.Session, summaries []store.Summary, days map[string]facts.Day, external []store.ExternalItem, now time.Time) TodayView {
	byID := make(map[string]store.Summary, len(summaries))
	for _, s := range summaries {
		byID[s.SubjectID] = s
	}

	v := TodayView{
		Running: Lane{Title: "Running", Status: watch.StatusRunning},
		Waiting: Lane{Title: "Waiting on you", Status: watch.StatusWaiting},
		Idle:    Lane{Title: "Idle", Status: watch.StatusIdle},
		AsOf:    now,

		FinishedToday: []FinishedTicket{},
		ReviewsDone:   []ReviewDone{},
		StartedToday:  []StartedTicket{},
	}

	var spanStart, spanEnd time.Time
	for _, se := range sessions {
		status := watch.SessionStatus(now, se)
		day, hasDay := days[se.ID]
		sum := byID[summary.DayKey(se.ID, now)]
		card := Card{
			ID:           se.ID,
			Agent:        se.Agent,
			Project:      se.ProjectKey,
			Branch:       se.Branch,
			Status:       status,
			AIHours:      day.AISeconds / 3600,
			WallHours:    day.Last.Sub(day.First).Hours(),
			LastActivity: se.LastActivityAt,
		}
		if json.Unmarshal([]byte(sum.Structured), &card.TodayPoints) == nil {
			card.TodayAsOf = sum.AsOf
		}

		activeToday := sameLocalDay(se.LastActivityAt, now)
		switch status {
		case watch.StatusRunning:
			v.Running.Cards = append(v.Running.Cards, card)
		case watch.StatusWaiting:
			v.Waiting.Cards = append(v.Waiting.Cards, card)
		case watch.StatusIdle:
			if activeToday {
				v.Idle.Cards = append(v.Idle.Cards, card)
			}
		}

		if hasDay {
			v.DaySessions++
			v.DayAIHours += day.AISeconds / 3600
			if spanStart.IsZero() || day.First.Before(spanStart) {
				spanStart = day.First
			}
			if day.Last.After(spanEnd) {
				spanEnd = day.Last
			}
		}
	}

	if !spanStart.IsZero() && spanEnd.After(spanStart) {
		v.DaySpanHours = spanEnd.Sub(spanStart).Hours()
	}
	for _, it := range external {
		switch it.Kind {
		case "review_request":
			v.Reviews = append(v.Reviews, ReviewItem{Title: it.Title, Repo: it.Repo, URL: it.URL, UpdatedAt: it.UpdatedAt})
		case "pr":
			// Only PRs active today count toward the day's contribution figure,
			// so it reads honestly in the today-scoped totals line.
			if sameLocalDay(it.UpdatedAt, now) {
				v.ContribPRs++
			}
		case "review_done":
			if sameLocalDay(it.UpdatedAt, now) {
				v.ReviewsDone = append(v.ReviewsDone, ReviewDone{Title: it.Title, Repo: it.Repo, URL: it.URL, SubmittedAt: it.UpdatedAt})
			}
		case "task":
			if it.Stage == "completed" && sameLocalDay(it.CompletedAt, now) {
				v.FinishedToday = append(v.FinishedToday, FinishedTicket{
					Source: it.Source, ID: it.ExternalID, Key: it.Repo, Title: it.Title, URL: it.URL, Project: it.Project, CompletedAt: it.CompletedAt,
				})
			}
			if it.Stage == "started" && sameLocalDay(it.StartedAt, now) {
				hours := it.Estimate
				if hours <= 0 {
					hours = 1
				}
				v.StartedToday = append(v.StartedToday, StartedTicket{
					Source: it.Source, ID: it.ExternalID, Key: it.Repo, Title: it.Title, URL: it.URL, Project: it.Project,
					State: it.State, Stage: it.Stage, StartedAt: it.StartedAt, Hours: hours,
				})
			}
		}
	}
	sort.Slice(v.FinishedToday, func(i, j int) bool { return v.FinishedToday[i].CompletedAt.After(v.FinishedToday[j].CompletedAt) })
	sort.Slice(v.ReviewsDone, func(i, j int) bool { return v.ReviewsDone[i].SubmittedAt.After(v.ReviewsDone[j].SubmittedAt) })
	sort.Slice(v.StartedToday, func(i, j int) bool { return v.StartedToday[i].StartedAt.After(v.StartedToday[j].StartedAt) })

	v.StateOfNow = stateOfNow(len(v.Running.Cards), len(v.Waiting.Cards), len(v.Idle.Cards))
	return v
}

// stateOfNow is a deterministic, plain-language orienting sentence. It is not
// AI-authored (the AI version is a later enhancement).
func stateOfNow(running, waiting, idle int) string {
	if running == 0 && waiting == 0 && idle == 0 {
		return "Nothing running yet today."
	}
	parts := []string{
		fmt.Sprintf("%d running", running),
		fmt.Sprintf("%d waiting on you", waiting),
		fmt.Sprintf("%d idle", idle),
	}
	s := parts[0]
	for i := 1; i < len(parts); i++ {
		if i == len(parts)-1 {
			s += ", and " + parts[i]
		} else {
			s += ", " + parts[i]
		}
	}
	return s + "."
}
