package jira

import (
	"context"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

// Fetcher is the slice of Client that Sync needs.
type Fetcher interface {
	AssignedIssues(ctx context.Context, site, email, token string) ([]Item, error)
}

// Sync reads the stored Jira site, email and API token; if any is missing it is
// a no-op (not an error). Otherwise it fetches assigned issues and replaces the
// "jira" external items. A fetch error returns without replacing, so a
// transient failure never wipes the last good items. Returns the count stored.
func Sync(ctx context.Context, st *store.Store, f Fetcher) (int, error) {
	creds := map[string]string{}
	for _, k := range []string{"jira_site", "jira_email", "jira_api_token"} {
		v, ok, err := st.GetSetting(k)
		if err != nil {
			return 0, err
		}
		if !ok || v == "" {
			return 0, nil
		}
		creds[k] = v
	}
	items, err := f.AssignedIssues(ctx, creds["jira_site"], creds["jira_email"], creds["jira_api_token"])
	if err != nil {
		return 0, err
	}
	ext := make([]store.ExternalItem, 0, len(items))
	for _, it := range items {
		var completed, started time.Time
		switch it.Category {
		case "done":
			completed = it.CategoryChangedAt
		case "indeterminate":
			started = it.CategoryChangedAt
		}
		ext = append(ext, store.ExternalItem{
			Source: "jira", Kind: "task", ExternalID: it.Key, Title: it.Title,
			State: it.State, Stage: stage(it.Category), URL: it.URL, Repo: it.Key, AssignedToMe: true, UpdatedAt: it.UpdatedAt, Estimate: it.Points,
			Project: it.Project, Labels: it.Labels, CompletedAt: completed, StartedAt: started,
		})
	}
	if err := st.ReplaceExternalItems("jira", ext); err != nil {
		return 0, err
	}
	return len(ext), nil
}

// stage maps a Jira status category onto the planner's stage names (the Linear
// state types).
func stage(category string) string {
	switch category {
	case "indeterminate":
		return "started"
	case "new":
		return "unstarted"
	case "done":
		return "completed"
	}
	return ""
}
