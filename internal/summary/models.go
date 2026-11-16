package summary

import (
	"encoding/json"
	"regexp"
	"sort"
)

// The tools that can write summaries.
const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
)

// ModelOption is one model a provider can summarize with. Multiple is its usage
// relative to the provider's cheapest, 0 when unknown. PaidCredits marks a model
// that can bill to paid usage credits rather than the subscription's limits.
type ModelOption struct {
	ID          string
	Name        string
	Description string
	Multiple    int
	PaidCredits bool
}

// codexMultiples are from OpenAI's credit rates, relative to GPT-6 Luna.
var codexMultiples = map[string]int{"gpt-6.1-terra": 10, "gpt-6.1-sol": 20, "gpt-6-astra": 100}

// ClaudeModels is fixed: these aliases always follow the latest version. Summaries
// choose from it, and a chat falls back to it when claude can't list its models.
func ClaudeModels() []ModelOption {
	return []ModelOption{
		{ID: "haiku", Name: "Haiku"},
		{ID: "sonnet", Name: "Sonnet"},
		{ID: "opus", Name: "Opus"},
		{ID: "fable", Name: "Fable", PaidCredits: true},
	}
}

// FallbackCodexModels is used when codex cannot list its own models.
func FallbackCodexModels() []ModelOption {
	return []ModelOption{
		{ID: "gpt-6-luna", Name: "GPT-6 Luna"},
		{ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra", Multiple: codexMultiples["gpt-6.1-terra"]},
		{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", Multiple: codexMultiples["gpt-6.1-sol"]},
		{ID: "gpt-6-astra", Name: "GPT-6 Astra", Multiple: codexMultiples["gpt-6-astra"]},
	}
}

// CheapestModel is the model a provider uses unless the user chooses another.
func CheapestModel(provider string) string {
	if provider == ProviderCodex {
		return "gpt-6-luna"
	}
	return "haiku"
}

// CheapestListed is the model a change to provider lands on: its usual cheapest
// when listed, else the listed model of lowest known cost, else the first listed.
// known is true only for the usual cheapest; otherwise the cost is not certain to
// be the lowest, so the card still warns about it.
func CheapestListed(provider string, models []ModelOption) (id string, known bool) {
	usual := CheapestModel(provider)
	if len(models) == 0 {
		return usual, false
	}
	id = models[0].ID
	lowest := 0
	for _, m := range models {
		if m.ID == usual {
			return usual, true
		}
		if m.Multiple > 0 && (lowest == 0 || m.Multiple < lowest) {
			id, lowest = m.ID, m.Multiple
		}
	}
	return id, false
}

var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validModelID reports whether id is safe to pass as a command-line value: it
// cannot be read as a flag.
func validModelID(id string) bool { return modelIDPattern.MatchString(id) }

// parseCodexModels reads the output of codex debug models: the entries Codex
// itself would offer, in its own order. Output it cannot read gives none.
func parseCodexModels(out []byte) []ModelOption {
	var catalog struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Description string `json:"description"`
			Visibility  string `json:"visibility"`
			Priority    int    `json:"priority"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &catalog); err != nil {
		return nil
	}
	sort.SliceStable(catalog.Models, func(i, j int) bool {
		return catalog.Models[i].Priority < catalog.Models[j].Priority
	})
	var ms []ModelOption
	for _, m := range catalog.Models {
		if m.Visibility != "list" || !validModelID(m.Slug) {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		ms = append(ms, ModelOption{ID: m.Slug, Name: name, Description: m.Description, Multiple: codexMultiples[m.Slug]})
	}
	return ms
}
