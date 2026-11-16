// Package ingest wires the capture pipeline together: discover transcript
// files, read new records, derive facts, time, and project identity, and write
// each file's delta into the store in one transaction.
package ingest

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/host"
	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/transcript"
)

// transcriptGlobs match, under <root>/projects, a session's transcript and its
// sub-agents' transcripts (<slug>/<sessionId>/subagents/agent-*.jsonl).
var transcriptGlobs = []string{
	filepath.Join("*", "*.jsonl"),
	filepath.Join("*", "*", "subagents", "*.jsonl"),
}

// DiscoverClaude returns every transcript file under <root>/projects/, the
// sessions' own before their sub-agents'.
func DiscoverClaude(root string) ([]string, error) {
	var files []string
	for _, g := range transcriptGlobs {
		m, err := filepath.Glob(filepath.Join(root, "projects", g))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	return files, nil
}

// IsTranscript reports whether path is a transcript DiscoverClaude would find
// under root.
func IsTranscript(root, path string) bool {
	rel, err := filepath.Rel(filepath.Join(root, "projects"), path)
	if err != nil {
		return false
	}
	for _, g := range transcriptGlobs {
		if ok, _ := filepath.Match(g, rel); ok {
			return true
		}
	}
	return false
}

// rolloutGlob matches, under <root>/sessions, a Codex session's rollout
// (YYYY/MM/DD/rollout-<time>-<session id>.jsonl).
var rolloutGlob = filepath.Join("*", "*", "*", "rollout-*.jsonl")

// CodexHomes are Codex's homes: ~/.codex, where the Codex app writes, and
// $CODEX_HOME when it names another folder (an Orca terminal points it at
// Orca's own). Paths compare cleaned and case-insensitive, as on Windows.
func CodexHomes() []string {
	home, _ := os.UserHomeDir()
	homes := []string{filepath.Join(home, ".codex")}
	if d := os.Getenv("CODEX_HOME"); d != "" && !strings.EqualFold(filepath.Clean(d), homes[0]) {
		homes = append(homes, filepath.Clean(d))
	}
	return homes
}

// DiscoverCodex returns every Codex rollout under each root's sessions/.
func DiscoverCodex(roots ...string) ([]string, error) {
	var files []string
	for _, root := range roots {
		m, err := filepath.Glob(filepath.Join(root, "sessions", rolloutGlob))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	return files, nil
}

// IsCodexRollout reports whether path is a rollout DiscoverCodex would find
// under root.
func IsCodexRollout(root, path string) bool {
	rel, err := filepath.Rel(filepath.Join(root, "sessions"), path)
	if err != nil {
		return false
	}
	ok, _ := filepath.Match(rolloutGlob, rel)
	return ok
}

// CodexTitles maps each Codex session id to its title, the newest thread_name
// in each root's session_index.jsonl (Codex appends a line on each rename).
func CodexTitles(roots ...string) map[string]string {
	out := map[string]string{}
	for _, root := range roots {
		codexTitles(root, out)
	}
	return out
}

func codexTitles(root string, out map[string]string) {
	b, err := os.ReadFile(filepath.Join(root, "session_index.jsonl"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		var e struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.ID != "" && e.ThreadName != "" {
			out[e.ID] = e.ThreadName
		}
	}
}

// isRollout reports whether path is a Codex rollout rather than a Claude
// transcript.
func isRollout(path string) bool {
	return strings.HasPrefix(filepath.Base(path), "rollout-")
}

// isSubagent reports whether path is a sub-agent transcript.
func isSubagent(path string) bool {
	return filepath.Base(filepath.Dir(path)) == "subagents"
}

// IngestFile reads a transcript's new records since its cursor and writes the
// resulting delta. The cursor is keyed by path; the session id is the file's
// base name. AI active time is accumulated onto the stored total, seeded with
// the previous batch's last event so a gap across the checkpoint still counts.
// A file with no new records is a no-op (the cursor does not move).
func IngestFile(st *store.Store, git project.Lookup, path string) (dropped int, err error) {
	cur, err := st.Cursor(path)
	if err != nil {
		return 0, err
	}
	recs, newOffset, dropped, err := transcript.ReadNew(path, cur)
	if err != nil {
		return 0, err
	}
	if len(recs) == 0 {
		// Only lines that could not be read (or blank ones): step past them, so
		// they are not read, and counted as dropped, again on every pass.
		if newOffset > cur {
			return dropped, st.AdvanceCursor(path, newOffset)
		}
		return dropped, nil
	}
	if isSubagent(path) {
		return dropped, ingestSubagent(st, path, recs, newOffset)
	}

	// The record's own sessionId is authoritative; the file base name is only a
	// fallback for a batch that carries none. This keeps the session identity
	// correct even if a base name is reused under two project slugs.
	codex := isRollout(path)
	sessionID := newestField(recs, func(r transcript.Record) string { return r.SessionID })
	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if codex && len(sessionID) > 36 {
			sessionID = sessionID[len(sessionID)-36:] // a rollout's name ends in its session id
		}
	}
	existing, had, err := st.GetSession(sessionID)
	if err != nil {
		return 0, err
	}

	// A session keeps the folder it started in: the agent may change directory
	// for a while, and the folder is what ties the session to its terminal.
	cwd := existing.CWD
	if cwd == "" {
		cwd = newestField(recs, func(r transcript.Record) string { return r.CWD })
	}
	branch := newestField(recs, func(r transcript.Record) string { return r.GitBranch })
	if branch == "" {
		branch = existing.Branch
	}

	// Resolve the repo only when we have a directory. git.Info("") would run git
	// in devtopo's own working directory and misattribute the session.
	var info project.GitInfo
	if cwd != "" {
		info, _ = git.Info(cwd)
	}
	key := project.Key(info, cwd)
	if key == "" {
		key = sessionID
	}
	name := project.Name(key)

	seed, err := st.CursorBoundary(path)
	if err != nil {
		return 0, err
	}
	delta, last := facts.AIActiveSeconds(recs, seed)
	days, _ := facts.ByDay(recs, seed, time.Local)
	agent, tail := "claude", facts.TailKind(recs, existing.Tail)
	if codex {
		agent, tail = "codex", facts.TurnTail(recs, existing.Tail)
	}

	total := delta
	if had {
		total = existing.AIActiveSeconds + delta
	}

	// Claude Code also writes lines with no time (titles, modes); a batch of
	// only those must not blank the last activity, which keeps the session on
	// Today and its summary going.
	lastActivity := newestTS(recs)
	if lastActivity.Before(existing.LastActivityAt) {
		lastActivity = existing.LastActivityAt
	}

	startedAt := existing.StartedAt
	if !had || startedAt.IsZero() {
		startedAt = earliestTS(recs)
	}

	d := store.Delta{
		Project: store.Project{Key: key, Name: name},
		Session: store.Session{
			ID:              sessionID,
			ProjectKey:      key,
			Agent:           agent,
			CWD:             cwd,
			Branch:          branch,
			StartedAt:       startedAt,
			LastActivityAt:  lastActivity,
			LastEventAt:     last.Time,
			LastEventIsAI:   last.IsAI,
			AIActiveSeconds: total,
			Status:          existing.Status, // carried forward; the watcher's sweep owns the live value
			Tail:            tail,
			TranscriptPath:  filepath.Clean(path),
		},
		Facts:          facts.Extract(recs),
		Days:           days,
		CursorPath:     path,
		CursorOffset:   newOffset,
		CursorBoundary: last,
	}
	if err := st.Apply(d); err != nil {
		return dropped, err
	}
	// Claude Code continued this session under a new id: it is one session.
	if next := newestField(recs, func(r transcript.Record) string { return r.ContinuedIn }); next != "" {
		return dropped, st.MergeContinued(sessionID, next)
	}
	// The Codex app keeps its sessions open; no process tells which is whose.
	if newestField(recs, func(r transcript.Record) string { return r.Originator }) == "Codex Desktop" {
		return dropped, st.SetSessionHost(sessionID, host.CodexApp)
	}
	return dropped, nil
}

// ingestSubagent adds a sub-agent batch's AI time and day rows to its parent
// session (the records' sessionId) and changes nothing else on it: the parent's
// own transcript owns its status, events, and facts, and a sub-agent's prompts
// are the lead agent's, not the human's. The sub-agent's own last event and
// tail shape are kept apart, so status can tell it is still working for the
// parent. Parallel sub-agents interleave, so the
// gaps are seeded from this file's own last event, kept with its cursor. With
// no parent session yet the batch is left unread, to be retried.
func ingestSubagent(st *store.Store, path string, recs []transcript.Record, newOffset int64) error {
	sessionID := newestField(recs, func(r transcript.Record) string { return r.SessionID })
	if sessionID == "" {
		sessionID = filepath.Base(filepath.Dir(filepath.Dir(path)))
	}
	// A sub-agent of a session since continued under a new id counts toward that.
	sessionID, err := st.ContinuedAs(sessionID)
	if err != nil {
		return err
	}
	parent, had, err := st.GetSession(sessionID)
	if err != nil || !had {
		return err
	}
	seed, err := st.CursorBoundary(path)
	if err != nil {
		return err
	}
	delta, last := facts.AIActiveSeconds(recs, seed)
	days, _ := facts.ByDay(recs, seed, time.Local)
	parent.AIActiveSeconds += delta
	return st.Apply(store.Delta{
		Project:        store.Project{Key: parent.ProjectKey, Name: project.Name(parent.ProjectKey)},
		Session:        parent,
		Days:           days,
		CursorPath:     path,
		CursorOffset:   newOffset,
		CursorBoundary: last,
		// An empty tail (no real event in the batch) leaves the stored one.
		Subagent: store.Subagent{LastEventAt: last.Time, Tail: facts.TailKind(recs, "")},
	})
}

// IngestAll discovers and ingests every Claude transcript and Codex rollout,
// returning a summary plus the total count of undecodable lines skipped across
// all files.
func IngestAll(st *store.Store, git project.Lookup, claudeRoot string, codexRoots ...string) (projects, sessions int, aiSeconds float64, dropped int, err error) {
	if err := RebuildAITime(st); err != nil {
		return 0, 0, 0, 0, err
	}
	files, err := DiscoverClaude(claudeRoot)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	rollouts, err := DiscoverCodex(codexRoots...)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for _, f := range append(files, rollouts...) {
		d, err := IngestFile(st, git, f)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		dropped += d
	}
	projects, sessions, aiSeconds, err = st.Summary()
	return projects, sessions, aiSeconds, dropped, err
}

// rebuiltKey marks that RebuildAITime has run on this store. It replaces the
// earlier one-time day-row backfill, whose marker (session_days_backfilled) is
// left in old stores unread. v3 recounts once more because prompt facts no
// longer count agent reports and compaction summaries, v4 to fill the day
// rows' hour buckets, v5 to find the decisions already answered, and v6 to fold
// a session Claude Code continued under a new id into that one (DEV-17); the
// older markers are left unread.
const rebuiltKey = "ai_time_rebuilt_v6"

// RebuildAITime makes the next ingest recount, once per store, every session
// whose transcript is still on disk, under the current definition of AI time:
// its total, day rows, facts and cursors are cleared, so its transcripts are
// read again from the start. A session whose transcript is gone keeps what it
// has. Call it before the watcher starts, so no ingest runs alongside it.
func RebuildAITime(st *store.Store) error {
	if _, done, err := st.GetSetting(rebuiltKey); err != nil || done {
		return err
	}
	sessions, err := st.ListSessions()
	if err != nil {
		return err
	}
	var onDisk []store.Session
	for _, se := range sessions {
		if se.TranscriptPath == "" {
			continue
		}
		if _, err := os.Stat(se.TranscriptPath); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		onDisk = append(onDisk, se)
	}
	return st.ResetAITime(onDisk, rebuiltKey)
}

func newestField(recs []transcript.Record, get func(transcript.Record) string) string {
	var val string
	var at time.Time
	set := false
	for _, r := range recs {
		v := get(r)
		if v == "" {
			continue
		}
		if !set || r.Timestamp.After(at) {
			val, at, set = v, r.Timestamp, true
		}
	}
	return val
}

func earliestTS(recs []transcript.Record) time.Time {
	var t time.Time
	for _, r := range recs {
		if r.Timestamp.IsZero() {
			continue
		}
		if t.IsZero() || r.Timestamp.Before(t) {
			t = r.Timestamp
		}
	}
	return t
}

func newestTS(recs []transcript.Record) time.Time {
	var t time.Time
	for _, r := range recs {
		if r.Timestamp.After(t) {
			t = r.Timestamp
		}
	}
	return t
}
