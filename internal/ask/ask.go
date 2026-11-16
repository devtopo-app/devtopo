// Package ask answers a question about today's coding sessions through the
// user's own coding-agent CLI, in up to three steps: Check (summaries), Read
// (one call per session) and Combine.
package ask

// Session is one of today's sessions as the question sees it.
type Session struct {
	ID, Label, Agent, TranscriptPath string
	Points                           []string
	HasSummary, Stale                bool
}

// Note is one thing a Read call found in a session.
type Note struct{ SessionID, Label, Text string }

// Turn is one earlier question and answer, with the notes that backed it.
type Turn struct {
	Question, Answer string
	Notes            []Note
}

// Source points at the moment in a session that an answer rests on.
type Source struct{ SessionID, Label, Agent, At, Why string }

// Result is a finished answer.
type Result struct {
	Answer            string
	Sources           []Source
	FromSummaries     bool
	Total, Read       int
	Shortened, Failed []string // labels
}

// Event is one progress step of a run.
type Event struct {
	Type               string // checking, checked, relevant, read, combining, done, error
	Total, Count, Done int
	Note               *Note
	Result             *Result
	Error              string
}

const (
	// MaxParallelReads is how many sessions are read at the same time.
	MaxParallelReads = 3
	// MemoryBudget bounds the earlier conversation put in a prompt, in bytes.
	MemoryBudget = 48 * 1024
)
