package summary

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// AskTimeout bounds one chat answer, which can be slower than a summary.
const AskTimeout = 5 * time.Minute

// How long Claude's list is kept. A failed listing is retried sooner, so a passing
// problem doesn't leave the short list in place for long.
const (
	modelListTTL      = time.Hour
	modelListRetryTTL = time.Minute
)

// paidCreditsNote warns about the models that can leave the subscription's limits.
const paidCreditsNote = "May bill to paid usage credits on some plans."

// initializeRequest asks a stream-json claude for its session details, which
// include the models this account can use. No prompt is sent, so no model runs.
const initializeRequest = `{"type":"control_request","request_id":"1","request":{"subtype":"initialize"}}` + "\n"

// listArgs starts claude the same locked-down way as a summary, without a prompt.
func (c *ClaudeCLI) listArgs() []string {
	return []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--safe-mode",
		"--tools", "",
		"--disallowedTools", "mcp__*",
		"--no-session-persistence",
	}
}

// ListModels asks claude which models this account can use. An error means it
// couldn't say, and the caller falls back to the fixed list.
func (c *ClaudeCLI) ListModels(ctx context.Context) ([]ModelOption, error) {
	if c.ProbeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.ProbeTimeout)
		defer cancel()
	}
	out, err := c.run(ctx, c.Bin, c.listArgs(), initializeRequest, subscriptionEnv())
	if err != nil {
		return nil, err
	}
	ms := parseClaudeModels(out)
	if len(ms) == 0 {
		return nil, errors.New("claude listed no usable models")
	}
	return ms, nil
}

// parseClaudeModels reads the models from claude's reply, in claude's order.
// "default" is left out: it's another name for one of the others, and which one
// depends on the account.
func parseClaudeModels(out []byte) []ModelOption {
	for _, line := range strings.Split(string(out), "\n") {
		var reply struct {
			Type     string `json:"type"`
			Response struct {
				Subtype  string `json:"subtype"`
				Response struct {
					Models []struct {
						Value       string `json:"value"`
						DisplayName string `json:"displayName"`
						Description string `json:"description"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(line), &reply) != nil || reply.Type != "control_response" || reply.Response.Subtype != "success" {
			continue
		}
		var ms []ModelOption
		for _, m := range reply.Response.Response.Models {
			if m.Value == "default" || !validModelID(m.Value) {
				continue
			}
			name := m.DisplayName
			if name == "" {
				name = m.Value
			}
			ms = append(ms, ModelOption{ID: m.Value, Name: name, Description: m.Description, PaidCredits: strings.Contains(m.Value, "fable")})
		}
		return ms
	}
	return nil
}

// modelCache keeps Claude's list, since asking takes a few seconds and the list
// is needed for every question. The lock is held while asking, so readers at the
// same moment run it once.
type modelCache struct {
	mu      sync.Mutex
	models  []ModelOption
	expires time.Time
	now     func() time.Time
}

// get is the cached list, refreshed from lister when it has expired. When that
// fails it keeps the last good list, or uses fixed if there has never been one.
func (c *modelCache) get(lister func(context.Context) ([]ModelOption, error), fixed func() []ModelOption) []ModelOption {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if c.models != nil && now().Before(c.expires) {
		return c.models
	}
	ms, err := lister(context.Background())
	if err != nil {
		if c.models == nil {
			c.models = fixed()
		}
		c.expires = now().Add(modelListRetryTTL)
		return c.models
	}
	c.models, c.expires = ms, now().Add(modelListTTL)
	return c.models
}

// reset forgets the list, so the next reader asks again.
func (c *modelCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.models, c.expires = nil, time.Time{}
}

// Model is a choice the user can make for one chat call: ID is "tool:model".
// Default marks the one a chat starts on.
type Model struct {
	ID, Tool, Label, Description string
	Default                      bool
}

// claudeModels is what Claude lists, or the fixed list when it can't say.
func (s *Selector) claudeModels() []ModelOption {
	if s.b.ListClaude == nil {
		return ClaudeModels()
	}
	return s.claudeList.get(s.b.ListClaude, ClaudeModels)
}

// AskModels lists the models of the tools that worked at the last Probe, Claude
// first. It is separate from the choice summaries use: a chat picks for itself.
func (s *Selector) AskModels(ctx context.Context) []Model {
	avail := s.Availability()
	var ms []Model
	add := func(tool string, options []ModelOption) {
		for _, m := range options {
			description := m.Description
			if m.PaidCredits {
				description = strings.TrimPrefix(strings.TrimRight(description, ".")+". "+paidCreditsNote, ". ")
			}
			ms = append(ms, Model{ID: tool + ":" + m.ID, Tool: tool, Label: m.Name, Description: description})
		}
	}
	if avail[ProviderClaude] {
		add(ProviderClaude, s.claudeModels())
	}
	if avail[ProviderCodex] {
		add(ProviderCodex, s.CodexModels(ctx))
	}
	markDefault(ms)
	return ms
}

// AskNarrator returns a narrator for a listed model, with the tool's fixed
// arguments and a chat's longer limit. It is false for anything not listed, so a
// value from a request can't reach a command line. Its failures are the chat's
// to show, so they don't land on the Settings card as a failed summary.
func (s *Selector) AskNarrator(ctx context.Context, id string) (Narrator, bool) {
	listed := false
	for _, m := range s.AskModels(ctx) {
		if m.ID == id {
			listed = true
		}
	}
	if !listed {
		return nil, false
	}
	tool, model, _ := strings.Cut(id, ":")
	var n Narrator
	if tool == ProviderClaude {
		n = s.b.NewClaude(model)
	} else {
		n = s.b.NewCodex(model)
	}
	switch c := n.(type) {
	case *ClaudeCLI:
		c.NarrateTimeout = AskTimeout
	case *CodexCLI:
		c.NarrateTimeout = AskTimeout
	}
	return n, true
}

// markDefault flags the model a chat starts on: the cheapest we can recognize,
// so a first question never lands on a costly model. Without any of these it is
// the first listed.
func markDefault(ms []Model) {
	if len(ms) == 0 {
		return
	}
	for _, name := range []string{"haiku", "luna", "sonnet"} {
		for i := range ms {
			if strings.Contains(strings.ToLower(ms[i].ID), name) {
				ms[i].Default = true
				return
			}
		}
	}
	ms[0].Default = true
}
