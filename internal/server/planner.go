package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

// meeting is one calendar event on a plan's day. Calendar is the label of the
// feed it came from, empty when the feed has none.
type meeting struct {
	Title    string
	Start    time.Time
	End      time.Time
	Calendar string `json:",omitempty"`
}

// planWithItems is a plan plus its rows, its items and its window capacity.
type planWithItems struct {
	store.Plan
	Rows         []store.PlanRow
	Items        []planItem
	MakerHours   float64 // window hours minus meetings, floored at 0
	MeetingHours float64 // merged meeting time overlapping the window
	Meetings     []meeting
}

// planItem is a plan item plus the current details of the ticket it refers
// to, so a ticket finished after it was planned (and so no longer plannable)
// still shows as done with its key, project and labels. All are empty for an
// own activity or a ticket no longer synced.
type planItem struct {
	store.PlanItem
	RefState   string
	RefStage   string
	RefRepo    string
	RefURL     string // the ticket in its tracker
	RefProject string
	RefLabels  []string // never null in the JSON
}

func newID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%x", prefix, b)
}

// registerPlanner mounts the planner routes on the mux. They are loopback-only
// (the server binds 127.0.0.1) and serve the local UI; every body is validated.
func (s *Server) registerPlanner(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/plans", s.handlePlansList)
	mux.HandleFunc("POST /api/plans", s.handlePlanCreate)
	mux.HandleFunc("PATCH /api/plans/{id}", s.handlePlanUpdate)
	mux.HandleFunc("DELETE /api/plans/{id}", s.handlePlanDelete)
	mux.HandleFunc("POST /api/plans/{id}/rows", s.handleRowAdd)
	mux.HandleFunc("PATCH /api/rows/{id}", s.handleRowRename)
	mux.HandleFunc("DELETE /api/rows/{id}", s.handleRowDelete)
	mux.HandleFunc("POST /api/plans/{id}/items", s.handlePlanItemAdd)
	mux.HandleFunc("PATCH /api/items/{id}", s.handleItemUpdate)
	mux.HandleFunc("DELETE /api/items/{id}", s.handleItemDelete)
	mux.HandleFunc("GET /api/plannable", s.handlePlannable)
	mux.HandleFunc("POST /api/reviews/hidden", s.handleReviewHidden)
	mux.HandleFunc("POST /api/own-prs/done", s.handleOwnPRDone)
}

func (s *Server) handlePlansList(w http.ResponseWriter, r *http.Request) {
	plans, err := s.store.ListPlans("active")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]planWithItems, 0, len(plans))
	refs, err := s.planRefs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, p := range plans {
		pw, err := s.planWithItemsFrom(p, refs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, pw)
	}
	s.writeJSON(w, out)
}

// planWithItems loads a plan's rows and items, its window capacity and the
// meetings on its local day (the calendar track shows the whole day).
func (s *Server) planWithItems(p store.Plan) (planWithItems, error) {
	refs, err := s.planRefs()
	if err != nil {
		return planWithItems{Plan: p}, err
	}
	return s.planWithItemsFrom(p, refs)
}

// itemRefs are the synced items plan items point at, by source and id, and the
// own PRs marked done: read once for all of a day's plans.
type itemRefs struct {
	byID  map[[2]string]store.ExternalItem
	marks map[string]time.Time
}

func (s *Server) planRefs() (itemRefs, error) {
	ext, err := s.store.ListExternalItems("")
	if err != nil {
		return itemRefs{}, err
	}
	refs := itemRefs{byID: map[[2]string]store.ExternalItem{}}
	for _, e := range ext {
		refs.byID[[2]string{e.Source, e.ExternalID}] = e
	}
	refs.marks, err = s.store.OwnPRsDone()
	return refs, err
}

func (s *Server) planWithItemsFrom(p store.Plan, refs itemRefs) (planWithItems, error) {
	pw := planWithItems{Plan: p}
	var err error
	if pw.Rows, err = s.store.GetPlanRows(p.ID); err != nil {
		return pw, err
	}
	items, err := s.store.GetPlanItems(p.ID)
	if err != nil {
		return pw, err
	}
	marks := refs.marks
	for _, it := range items {
		e, synced := refs.byID[[2]string{it.RefSource, it.RefID}]
		// An own PR's done is worked out here, never kept in the item's done_at: a
		// PR no longer open needs nothing, and stats read done_at.
		if strings.HasPrefix(it.RefID, store.OwnPRPrefix) && (!synced || ownPRDone(e, marks)) {
			e.Stage = "completed"
		}
		pw.Items = append(pw.Items, planItem{PlanItem: it, RefState: e.State, RefStage: e.Stage,
			RefRepo: e.Repo, RefURL: e.URL, RefProject: e.Project, RefLabels: append([]string{}, e.Labels...)})
	}
	ws := p.WindowStart.Local()
	day := time.Date(ws.Year(), ws.Month(), ws.Day(), 0, 0, 0, 0, time.Local)
	events, err := s.store.EventsInWindow(day, day.AddDate(0, 0, 1))
	if err != nil {
		return pw, err
	}
	windowHours := p.WindowEnd.Sub(p.WindowStart).Hours()
	if windowHours < 0 {
		windowHours = 0
	}
	pw.MeetingHours = meetingHours(events, p.WindowStart, p.WindowEnd)
	if m := windowHours - pw.MeetingHours; m > 0 {
		pw.MakerHours = m
	}
	for _, e := range events {
		pw.Meetings = append(pw.Meetings, meeting{Title: e.Title, Start: e.Start, End: e.End, Calendar: e.Calendar})
	}
	return pw, nil
}

// meetingHours returns the meeting time overlapping [ws, we), merging
// overlapping meetings so the total never exceeds the window.
func meetingHours(events []store.CalendarEvent, ws, we time.Time) float64 {
	var ivs [][2]time.Time
	for _, ev := range events {
		s, e := ev.Start, ev.End
		if s.Before(ws) {
			s = ws
		}
		if e.After(we) {
			e = we
		}
		if e.After(s) {
			ivs = append(ivs, [2]time.Time{s, e})
		}
	}
	if len(ivs) == 0 {
		return 0
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i][0].Before(ivs[j][0]) })
	var total time.Duration
	cs, ce := ivs[0][0], ivs[0][1]
	for _, v := range ivs[1:] {
		if v[0].After(ce) {
			total += ce.Sub(cs)
			cs, ce = v[0], v[1]
		} else if v[1].After(ce) {
			ce = v[1]
		}
	}
	total += ce.Sub(cs)
	return total.Hours()
}

// decodePlan reads a plan's name and window from a create or update body,
// writing a 400 and returning false when it is invalid.
func decodePlan(w http.ResponseWriter, r *http.Request) (store.Plan, bool) {
	var body struct {
		Name        string `json:"name"`
		WindowStart string `json:"windowStart"`
		WindowEnd   string `json:"windowEnd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return store.Plan{}, false
	}
	ws, err1 := time.Parse(time.RFC3339, body.WindowStart)
	we, err2 := time.Parse(time.RFC3339, body.WindowEnd)
	if err1 != nil || err2 != nil {
		http.Error(w, "windowStart and windowEnd must be RFC3339", http.StatusBadRequest)
		return store.Plan{}, false
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return store.Plan{}, false
	}
	return store.Plan{Name: name, WindowStart: ws, WindowEnd: we}, true
}

func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	p, ok := decodePlan(w, r)
	if !ok {
		return
	}
	p.ID, p.CreatedAt, p.Status = newID("p"), s.clock(), "active"
	if err := s.store.CreatePlan(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pw, err := s.planWithItems(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, pw)
}

// planFound writes a 404 (or a 500) and returns false when the {id} plan is
// missing.
func (s *Server) planFound(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.store.PlanExists(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.NotFound(w, r)
	}
	return ok
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.planFound(w, r) {
		return
	}
	p, ok := decodePlan(w, r)
	if !ok {
		return
	}
	p.ID = r.PathValue("id")
	if err := s.store.UpdatePlan(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlanDelete(w http.ResponseWriter, r *http.Request) {
	if !s.planFound(w, r) {
		return
	}
	if err := s.store.DeletePlan(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeRowName reads a row's name, writing a 400 and returning false when it
// is invalid or blank.
func decodeRowName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		http.Error(w, "a row needs a name", http.StatusBadRequest)
		return "", false
	}
	return strings.TrimSpace(body.Name), true
}

func (s *Server) handleRowAdd(w http.ResponseWriter, r *http.Request) {
	if !s.planFound(w, r) {
		return
	}
	name, ok := decodeRowName(w, r)
	if !ok {
		return
	}
	planID := r.PathValue("id")
	rows, err := s.store.GetPlanRows(planID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	row := store.PlanRow{ID: newID("r"), PlanID: planID, Name: name}
	if len(rows) > 0 {
		row.Order = rows[len(rows)-1].Order + 1
	}
	if err := s.store.AddPlanRow(row); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, row)
}

func (s *Server) handleRowRename(w http.ResponseWriter, r *http.Request) {
	name, ok := decodeRowName(w, r)
	if !ok {
		return
	}
	if err := s.store.RenamePlanRow(r.PathValue("id"), name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRowDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePlanRow(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// itemBody is the editable part of a plan item. A null start is "anytime this
// day".
type itemBody struct {
	RowID         string     `json:"rowId"`
	RefSource     string     `json:"refSource"`
	RefID         string     `json:"refId"`
	Text          string     `json:"text"`
	Start         *time.Time `json:"start"`
	IntendedHours float64    `json:"intendedHours"`
}

func (s *Server) handlePlanItemAdd(w http.ResponseWriter, r *http.Request) {
	if !s.planFound(w, r) {
		return
	}
	var body itemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	it := store.PlanItem{
		ID: newID("i"), PlanID: r.PathValue("id"), RowID: body.RowID,
		RefSource: body.RefSource, RefID: body.RefID, Text: body.Text,
		Start: body.Start, IntendedHours: body.IntendedHours,
	}
	if err := s.store.AddPlanItem(it); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, it)
}

func (s *Server) handleItemUpdate(w http.ResponseWriter, r *http.Request) {
	var body itemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	it := store.PlanItem{ID: r.PathValue("id"), RowID: body.RowID, Text: body.Text, Start: body.Start, IntendedHours: body.IntendedHours}
	if err := s.store.UpdatePlanItem(it); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleItemDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePlanItem(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// plannableItem is a candidate the user can drop into a plan. Kind is
// "review_request" or "task"; Hours is the block length it starts with.
type plannableItem struct {
	RefSource string
	RefID     string
	Text      string
	Repo      string
	URL       string // the ticket or PR in its tracker
	Kind      string
	Hours     float64
	State     string // the tracker's status name, e.g. "In Review"
	Stage     string // see stageRank
	Project   string
	Labels    []string // never null in the JSON
	// A review's lines, known when Sized, and whether the user hid it.
	Additions int
	Deletions int
	Sized     bool
	Hidden    bool
	// An own PR's latest word: State is its review state, and Done its done mark.
	Reviewer   string
	ReviewedAt time.Time
	Done       bool
}

// reviewHours estimates a review from the lines it changes, leaving out generated
// files. A PR of unknown size stays at half an hour.
func reviewHours(lines int, sized bool) float64 {
	switch {
	case !sized:
		return 0.5
	case lines <= 50:
		return 0.25
	case lines <= 200:
		return 0.5
	case lines <= 500:
		return 1
	case lines <= 1000:
		return 2
	}
	return 3
}

// reviewHidden reports whether the user hid this PR and hasn't been asked to
// review it since. New commits alone don't bring it back.
func reviewHidden(it store.ExternalItem, hidden map[string]time.Time) bool {
	at, ok := hidden[it.ExternalID]
	return ok && it.Kind == "review_request" && !it.RequestedAt.After(at)
}

// handleReviewHidden hides a review from the tray, or brings it back.
func (s *Server) handleReviewHidden(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL    string
		Hidden bool
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.URL) == "" {
		http.Error(w, "a body with a URL is required", http.StatusBadRequest)
		return
	}
	var err error
	if body.Hidden {
		err = s.store.HideReview(body.URL, s.clock())
	} else {
		err = s.store.UnhideReview(body.URL)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlannable(w http.ResponseWriter, r *http.Request) {
	out := make([]plannableItem, 0)
	// Candidates come from every connector: review-requests (by size) and
	// tasks from any ticket tracker (1 point = 1 hour, an hour when unestimated).
	ext, err := s.store.ListExternalItems("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hidden, err := s.store.HiddenReviews()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	marks, err := s.store.OwnPRsDone()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, it := range ext {
		sized := it.ChangedFiles > 0
		hours := reviewHours(it.Additions+it.Deletions, sized)
		switch {
		case it.Stage == "completed" || it.Stage == "canceled":
			continue // synced only so a planned ticket shows as finished
		case it.Kind == "task" && it.Estimate > 0:
			hours = it.Estimate
		case it.Kind == "task":
			hours = 1
		case it.Kind == "own_pr":
			if it.State == "" {
				continue // no review yet, so nothing for you to act on
			}
			hours = ownPRHours(it.State)
		case it.Kind != "review_request":
			continue
		}
		out = append(out, plannableItem{RefSource: it.Source, RefID: it.ExternalID, Text: it.Title, Repo: it.Repo, Kind: it.Kind, Hours: hours, URL: it.URL, State: it.State, Stage: it.Stage,
			Project: it.Project, Labels: append([]string{}, it.Labels...), Additions: it.Additions, Deletions: it.Deletions, Sized: sized,
			Hidden: reviewHidden(it, hidden), Reviewer: it.Reviewer, ReviewedAt: it.ReviewedAt, Done: it.Kind == "own_pr" && ownPRDone(it, marks)})
	}
	// Tickets first, by stage, then status name, then title. Reviews compare
	// equal, so the stable sort keeps them newest first after the tickets.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Kind == "task") != (b.Kind == "task") {
			return a.Kind == "task"
		}
		if a.Kind != "task" {
			return false
		}
		if ra, rb := stageRank(a.Stage), stageRank(b.Stage); ra != rb {
			return ra < rb
		}
		if a.State != b.State {
			return a.State < b.State
		}
		return a.Text < b.Text
	})
	s.writeJSON(w, out)
}

// stageRank orders ticket stages: work in progress first, then what is next,
// then what is not yet triaged or planned, then anything unknown.
func stageRank(stage string) int {
	switch stage {
	case "started":
		return 0
	case "unstarted":
		return 1
	case "triage":
		return 2
	case "backlog":
		return 3
	}
	return 4
}
