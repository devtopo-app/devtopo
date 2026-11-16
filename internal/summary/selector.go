package summary

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// ErrNoNarrator is what asking for a summary gets when no tool is available.
var ErrNoNarrator = errors.New("no summary tool available")

// Backends is how a Selector reaches the tools. It is a value so tests never
// exec a real CLI.
type Backends struct {
	ProbeClaude, ProbeCodex func(ctx context.Context) bool
	ListClaude, ListCodex   func(ctx context.Context) ([]ModelOption, error)
	NewClaude, NewCodex     func(model string) Narrator
}

// DefaultBackends drives the claude and codex binaries on PATH.
func DefaultBackends() Backends {
	return Backends{
		ProbeClaude: func(ctx context.Context) bool { return NewClaudeCLI().Available(ctx) },
		ProbeCodex:  func(ctx context.Context) bool { return NewCodexCLI().Available(ctx) },
		ListClaude:  func(ctx context.Context) ([]ModelOption, error) { return NewClaudeCLI().ListModels(ctx) },
		ListCodex:   func(ctx context.Context) ([]ModelOption, error) { return NewCodexCLI().ListModels(ctx) },
		NewClaude: func(model string) Narrator {
			c := NewClaudeCLI()
			c.Model = model
			return c
		},
		NewCodex: func(model string) Narrator {
			c := NewCodexCLI()
			c.Model = model
			return c
		},
	}
}

// Choice is a tool and one of its models.
type Choice struct{ Provider, Model string }

// RowState is one session kind ("claude" or "codex") as resolved now.
type RowState struct {
	Kind      string
	Saved     Choice // what the user chose, or the default
	Effective Choice // what runs; zero when no tool is available
	Fallback  bool   // Effective differs from Saved because Saved's tool is unavailable
	LastError string
}

// SettingKey is where a session kind's choice is stored.
func SettingKey(kind string) string { return "summary_model_" + kind }

// EncodeChoice is a choice as one stored value, so saving it is atomic.
func EncodeChoice(c Choice) string { return c.Provider + "/" + c.Model }

// Selector picks the tool and model for each session as it is summarized. The
// choice is read from the settings on every call, so a change applies at once.
// Whether a tool works is cached until the next Probe, since checking runs it.
type Selector struct {
	get func(string) (string, bool, error)
	b   Backends

	mu    sync.RWMutex
	avail map[string]bool
	errs  map[string]string

	listMu sync.Mutex // held across the listing, so readers at once run it once
	listed []ModelOption

	claudeList modelCache // what a chat can pick from Claude
}

// NewSelector builds a Selector over a settings reader. Until its first Probe it
// sees no tools.
func NewSelector(get func(string) (string, bool, error), b Backends) *Selector {
	return &Selector{get: get, b: b, avail: map[string]bool{}, errs: map[string]string{}}
}

// Probe checks both tools again and refreshes their lists on their next use. A
// new check starts with no recorded errors. A check cut short says nothing about
// the tools, so it changes nothing.
func (s *Selector) Probe(ctx context.Context) {
	claude := s.b.ProbeClaude(ctx)
	codex := s.b.ProbeCodex(ctx)
	if ctx.Err() != nil {
		return
	}
	s.listMu.Lock()
	s.listed = nil
	s.listMu.Unlock()
	s.claudeList.reset()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.avail = map[string]bool{ProviderClaude: claude, ProviderCodex: codex}
	s.errs = map[string]string{}
}

// Availability is which tools worked at the last Probe.
func (s *Selector) Availability() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]bool{ProviderClaude: s.avail[ProviderClaude], ProviderCodex: s.avail[ProviderCodex]}
}

// CodexModels is what Codex offers, or the fallback list when it cannot say. The
// answer is cached until the next Probe, unless the asker went away.
func (s *Selector) CodexModels(ctx context.Context) []ModelOption {
	s.listMu.Lock()
	defer s.listMu.Unlock()
	if s.listed != nil {
		return s.listed
	}
	ms, err := s.b.ListCodex(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return FallbackCodexModels()
		}
		ms = FallbackCodexModels()
	}
	s.listed = ms
	return ms
}

// kindOf is the row a session of agent belongs to. A session with no agent is
// Claude's, as sessions stored before Codex support are.
func kindOf(agent string) string {
	if agent == ProviderCodex {
		return ProviderCodex
	}
	return ProviderClaude
}

func otherProvider(p string) string {
	if p == ProviderCodex {
		return ProviderClaude
	}
	return ProviderCodex
}

// saved is the kind's stored choice, or its default: its own tool on its
// cheapest model. A value that does not read as a real choice counts as unset,
// so nothing odd in the settings can reach a command line.
func (s *Selector) saved(kind string) Choice {
	def := Choice{Provider: kind, Model: CheapestModel(kind)}
	v, ok, err := s.get(SettingKey(kind))
	if err != nil || !ok {
		return def
	}
	provider, model, found := strings.Cut(v, "/")
	if !found || (provider != ProviderClaude && provider != ProviderCodex) || !validModelID(model) {
		return def
	}
	return Choice{Provider: provider, Model: model}
}

// resolve is what actually runs for a kind. When the chosen tool is unavailable
// it is the other tool on its cheapest model, never a costlier one.
func (s *Selector) resolve(saved Choice) (effective Choice, fallback bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.avail[saved.Provider] {
		return saved, false
	}
	if other := otherProvider(saved.Provider); s.avail[other] {
		return Choice{Provider: other, Model: CheapestModel(other)}, true
	}
	return Choice{}, false
}

// Rows is the state of both session kinds, Claude then Codex.
func (s *Selector) Rows() []RowState {
	rows := make([]RowState, 0, 2)
	for _, kind := range []string{ProviderClaude, ProviderCodex} {
		saved := s.saved(kind)
		effective, fallback := s.resolve(saved)
		s.mu.RLock()
		lastErr := s.errs[kind]
		s.mu.RUnlock()
		rows = append(rows, RowState{Kind: kind, Saved: saved, Effective: effective, Fallback: fallback, LastError: lastErr})
	}
	return rows
}

// ClearError forgets a kind's last failure, as when the user chooses again.
func (s *Selector) ClearError(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.errs, kind)
}

// For returns the narrator for a session of agent, or nil when no tool is
// available. Its failures are recorded on the session kind's row.
func (s *Selector) For(agent string) Narrator {
	kind := kindOf(agent)
	effective, _ := s.resolve(s.saved(kind))
	var n Narrator
	switch effective.Provider {
	case ProviderClaude:
		n = s.b.NewClaude(effective.Model)
	case ProviderCodex:
		n = s.b.NewCodex(effective.Model)
	default:
		return nil
	}
	return recordingNarrator{s: s, kind: kind, n: n}
}

// Narrate serves a caller that has no session at hand, as a Claude session.
func (s *Selector) Narrate(ctx context.Context, prompt string) (string, error) {
	n := s.For("")
	if n == nil {
		return "", ErrNoNarrator
	}
	return n.Narrate(ctx, prompt)
}

// recordingNarrator keeps a kind's last failure for the Settings page, which is
// the only place someone would see that summaries have stopped.
type recordingNarrator struct {
	s    *Selector
	kind string
	n    Narrator
}

func (r recordingNarrator) Narrate(ctx context.Context, prompt string) (string, error) {
	text, err := r.n.Narrate(ctx, prompt)
	// A cancelled run is the caller going away, not a failed summary. A killed
	// child reports its own exit error, so the context is checked as well.
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return text, err
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if err != nil {
		r.s.errs[r.kind] = err.Error()
	} else {
		delete(r.s.errs, r.kind)
	}
	return text, err
}

// Ready reports whether n can write a summary now.
func Ready(n Narrator) bool {
	if n == nil {
		return false
	}
	if s, ok := n.(*Selector); ok {
		a := s.Availability()
		return a[ProviderClaude] || a[ProviderCodex]
	}
	return true
}
