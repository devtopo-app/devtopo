package linear

import (
	"context"

	"github.com/devtopo-app/devtopo/internal/store"
)

// Fetcher is the slice of Client that Sync needs.
type Fetcher interface {
	AssignedIssues(ctx context.Context, apiKey string) ([]Item, error)
}

// Sync reads the stored Linear API key; if none is set it is a no-op (not an
// error). Otherwise it fetches assigned issues and replaces the "linear"
// external items. A fetch error returns without replacing, so a transient
// failure never wipes the last good items. Returns the count stored.
func Sync(ctx context.Context, st *store.Store, f Fetcher) (int, error) {
	key, ok, err := st.GetSetting("linear_api_key")
	if err != nil {
		return 0, err
	}
	if !ok || key == "" {
		return 0, nil
	}
	items, err := f.AssignedIssues(ctx, key)
	if err != nil {
		return 0, err
	}
	ext := make([]store.ExternalItem, 0, len(items))
	for _, it := range items {
		ext = append(ext, store.ExternalItem{
			Source: "linear", Kind: "task", ExternalID: it.ExternalID, Title: it.Title,
			State: it.State, Stage: stage(it.StateType), URL: it.URL, Repo: it.Identifier, AssignedToMe: true, UpdatedAt: it.UpdatedAt, Estimate: it.Estimate,
			Project: it.Project, Labels: it.Labels, CompletedAt: it.CompletedAt, StartedAt: it.StartedAt,
		})
	}
	if err := st.ReplaceExternalItems("linear", ext); err != nil {
		return 0, err
	}
	return len(ext), nil
}

// stage keeps the Linear state types the planner knows; anything else maps
// to "".
func stage(stateType string) string {
	switch stateType {
	case "started", "unstarted", "backlog", "triage", "completed", "canceled":
		return stateType
	}
	return ""
}
