package ask

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/devtopo-app/devtopo/internal/summary"
)

// Request is one question and what it may draw on.
type Request struct {
	Question string
	Memory   []Turn
	Sessions []Session
	Day      time.Time
}

const (
	noSessionsAnswer = "There are no sessions today yet."
	notFoundAnswer   = "I didn't find it in what I read."
	nothingNote      = "Read for this question: nothing found."
	limitPrefix      = "Your subscription's usage limit was reached: "
)

// isLimit reports whether err says the user's usage limit or quota is used up.
func isLimit(err error) bool {
	if err == nil {
		return false
	}
	t := strings.ToLower(err.Error())
	for _, s := range []string{"usage limit", "rate limit", "limit reached", "hit your limit", "quota"} {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// errorText words an error. Only an error from the model call can be a usage
// limit; a file error that happens to mention a quota is not one.
func errorText(err error, fromModel bool) string {
	if fromModel && isLimit(err) {
		return limitPrefix + err.Error()
	}
	return err.Error()
}

// readOutcome is what one Read goroutine reports.
type readOutcome struct {
	session   Session
	note      *Note
	nothing   *Note // the read found nothing; kept so a follow-up need not read again
	failed    bool
	err       error // why the read failed
	cancelled bool
	limit     error
	shortened bool
}

// Run answers req.Question. It calls emit for each step, never concurrently,
// and ends with a done or error event. When ctx is cancelled it returns without
// emitting anything more. It returns only after every call it started is over.
func Run(ctx context.Context, n summary.Narrator, req Request, emit func(Event)) {
	total := len(req.Sessions)
	if total == 0 {
		emit(Event{Type: "done", Result: &Result{Answer: noSessionsAnswer}})
		return
	}
	memory := trimMemory(req.Memory, MemoryBudget)
	fail := func(err error, fromModel bool) {
		if ctx.Err() != nil {
			return
		}
		emit(Event{Type: "error", Error: errorText(err, fromModel)})
	}

	emit(Event{Type: "checking", Total: total})
	text, err := n.Narrate(ctx, checkPrompt(req.Question, memory, req.Sessions))
	if err != nil {
		fail(err, true)
		return
	}
	if ctx.Err() != nil {
		return
	}
	emit(Event{Type: "checked", Total: total})

	r, ok := parseReply(text, aliases(req.Sessions))
	if ok && r.Answer != "" && len(r.Sources) > 0 {
		emit(Event{Type: "done", Result: &Result{Answer: r.Answer, Sources: r.Sources, FromSummaries: true, Total: total}})
		return
	}

	var relevant []Session
	readAll := !ok || (r.DroppedRead && len(r.Read) == 0) || (r.Answer != "" && len(r.Sources) == 0 && len(r.Read) == 0)
	if readAll {
		relevant = req.Sessions
	} else {
		chosen := map[string]bool{}
		for _, id := range r.Read {
			chosen[id] = true
		}
		for _, id := range r.Read { // first-seen order, each once
			if !chosen[id] {
				continue
			}
			chosen[id] = false
			for _, s := range req.Sessions {
				if s.ID == id {
					relevant = append(relevant, s)
				}
			}
		}
		listed := map[string]bool{}
		for _, s := range relevant {
			listed[s.ID] = true
		}
		for _, s := range req.Sessions {
			if !s.HasSummary && !listed[s.ID] {
				relevant = append(relevant, s)
			}
		}
	}
	if len(relevant) == 0 {
		emit(Event{Type: "done", Result: &Result{Answer: notFoundAnswer, Total: total}})
		return
	}
	emit(Event{Type: "relevant", Total: total, Count: len(relevant)})

	// Read: one call per session, at most MaxParallelReads at once.
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	outcomes := make(chan readOutcome, len(relevant))
	sem := make(chan struct{}, MaxParallelReads)
	var wg sync.WaitGroup
	for _, s := range relevant {
		wg.Add(1)
		go func(s Session) {
			defer wg.Done()
			o := readOutcome{session: s}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-readCtx.Done():
				o.cancelled = true
				outcomes <- o
				return
			}
			if readCtx.Err() != nil {
				o.cancelled = true
				outcomes <- o
				return
			}
			prompt, shortened, err := summary.ReadDay(s.TranscriptPath, readPreamble(req.Question), req.Day, summary.DetailBudget)
			if err != nil {
				o.failed, o.err = true, err
				outcomes <- o
				return
			}
			o.shortened = shortened
			if prompt == "" { // no turns today: read, nothing to find
				outcomes <- o
				return
			}
			out, err := n.Narrate(readCtx, prompt)
			switch {
			case isLimit(err):
				o.limit = err
				cancel()
			case err != nil && readCtx.Err() != nil:
				o.cancelled = true
			case err != nil:
				o.failed, o.err = true, err
			default:
				if t := strings.TrimSpace(out); t != "" {
					if strings.EqualFold(strings.Trim(t, ".! "), "NOTHING") {
						o.nothing = &Note{SessionID: s.ID, Label: s.Label, Text: nothingNote}
					} else {
						o.note = &Note{SessionID: s.ID, Label: s.Label, Text: t}
					}
				}
			}
			outcomes <- o
		}(s)
	}

	var (
		notes, readCount, done int
		noteList               []Note
		res                    = Result{Total: total}
		limitErr, lastErr      error
	)
	for range relevant {
		o := <-outcomes
		if o.limit != nil && limitErr == nil {
			limitErr = o.limit
		}
		if limitErr != nil || o.cancelled || ctx.Err() != nil {
			continue
		}
		done++
		if o.failed {
			res.Failed = append(res.Failed, o.session.Label)
			lastErr = o.err
		} else {
			readCount++
			if o.shortened {
				res.Shortened = append(res.Shortened, o.session.Label)
			}
			if o.note != nil {
				notes++
				noteList = append(noteList, *o.note)
			}
		}
		keep := o.note
		if keep == nil {
			keep = o.nothing
		}
		emit(Event{Type: "read", Total: len(relevant), Done: done, Note: keep})
	}
	wg.Wait()

	if ctx.Err() != nil {
		return
	}
	if limitErr != nil {
		fail(limitErr, true)
		return
	}
	if readCount == 0 {
		fail(lastErr, false)
		return
	}
	res.Read = readCount
	if notes == 0 {
		res.Answer = notFoundAnswer
		emit(Event{Type: "done", Result: &res})
		return
	}

	emit(Event{Type: "combining", Total: total})
	text, err = n.Narrate(ctx, combinePrompt(req.Question, memory, noteList, req.Sessions))
	if err != nil {
		fail(err, true)
		return
	}
	if ctx.Err() != nil {
		return
	}
	c, ok := parseReply(text, aliases(req.Sessions))
	// namedSources lists each session with a real note, once.
	namedSources := func() {
		seen := map[string]bool{}
		for _, nt := range noteList {
			if seen[nt.SessionID] {
				continue
			}
			seen[nt.SessionID] = true
			for _, s := range relevant {
				if s.ID == nt.SessionID {
					res.Sources = append(res.Sources, Source{SessionID: s.ID, Label: s.Label, Agent: s.Agent})
				}
			}
		}
	}
	switch {
	case ok && c.Answer != "":
		res.Answer, res.Sources = c.Answer, c.Sources
		if len(res.Sources) == 0 {
			namedSources()
		}
	case ok || strings.TrimSpace(text) == "": // an empty reply says nothing
		res.Answer = notFoundAnswer
	default:
		res.Answer = nameSessions(strings.TrimSpace(text), aliases(req.Sessions))
		namedSources()
	}
	emit(Event{Type: "done", Result: &res})
}
