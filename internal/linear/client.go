// Package linear reads a developer's assigned Linear issues through the Linear
// GraphQL API, using a personal API key the user supplies. It is a direct
// vendor API call with the user's own key (not scraping, so no proxy), and it
// reads viewer/assignedIssues; its one write is MoveIssue, run only when the
// user marks a ticket done or not done. The key is never logged or returned.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	endpoint  = "https://api.linear.app/graphql"
	maxIssues = 200 // caps the paging so a huge backlog cannot stall a sync
)

// Personal API keys authenticate with the raw key in the Authorization header
// (no "Bearer"). Open issues, plus ones completed or canceled in the last 14
// days so a planned ticket shows as finished rather than vanishing.
const assignedIssuesQuery = `query($after: String) { viewer { assignedIssues(first: 50, after: $after, filter: { or: [{ state: { type: { nin: ["completed","canceled"] } } }, { completedAt: { gte: "-P14D" } }, { canceledAt: { gte: "-P14D" } }] }) { nodes { id identifier title url updatedAt completedAt startedAt estimate state { name type } project { name } labels { nodes { name } } } pageInfo { hasNextPage endCursor } } } }`

// The issue's team's workflow states of one type, and the update that moves
// the issue to one of them.
const (
	teamStatesQuery  = `query($id: String!, $type: String!) { issue(id: $id) { team { states(filter: { type: { eq: $type } }) { nodes { id name position } } } } }`
	issueStateUpdate = `mutation($id: String!, $stateId: String!) { issueUpdate(id: $id, input: { stateId: $stateId }) { success issue { state { name } } } }`
)

// Item is one assigned Linear issue.
type Item struct {
	ExternalID  string
	Identifier  string
	Title       string
	State       string
	StateType   string // triage, backlog, unstarted, started, completed or canceled
	URL         string
	UpdatedAt   time.Time
	Estimate    float64 // story points, 0 when the issue has none
	Project     string  // "" when the issue is in no project
	Labels      []string
	CompletedAt time.Time // zero unless the issue is completed
	StartedAt   time.Time // when the issue moved into a started state, zero if never
}

// Client queries the Linear GraphQL API. httpDo is injectable so tests never hit
// the network.
type Client struct {
	Endpoint string
	httpDo   func(*http.Request) (*http.Response, error)
}

// NewClient builds the production client.
func NewClient() *Client {
	hc := &http.Client{Timeout: 20 * time.Second}
	return &Client{Endpoint: endpoint, httpDo: hc.Do}
}

// AssignedIssues fetches the authenticated user's assigned issues, paging with
// the endCursor up to maxIssues so finished issues never push open ones out.
func (c *Client) AssignedIssues(ctx context.Context, apiKey string) ([]Item, error) {
	var items []Item
	var after any // null asks for the first page
	for len(items) < maxIssues {
		var out struct {
			Viewer *struct {
				AssignedIssues *struct {
					Nodes []struct {
						ID          string  `json:"id"`
						Identifier  string  `json:"identifier"`
						Title       string  `json:"title"`
						URL         string  `json:"url"`
						UpdatedAt   string  `json:"updatedAt"`
						CompletedAt string  `json:"completedAt"` // null decodes to ""
						StartedAt   string  `json:"startedAt"`
						Estimate    float64 `json:"estimate"`
						State       struct {
							Name string `json:"name"`
							Type string `json:"type"`
						} `json:"state"`
						Project struct {
							Name string `json:"name"`
						} `json:"project"` // null decodes to ""
						Labels struct {
							Nodes []struct {
								Name string `json:"name"`
							} `json:"nodes"`
						} `json:"labels"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"assignedIssues"`
			} `json:"viewer"`
		}
		// call refuses a reply that carries GraphQL errors or no data, so a
		// partial failure can never pass for "no assigned issues".
		if err := c.call(ctx, apiKey, assignedIssuesQuery, map[string]any{"after": after}, &out); err != nil {
			return nil, err
		}
		if out.Viewer == nil || out.Viewer.AssignedIssues == nil {
			return nil, fmt.Errorf("linear: the response has no assigned issues")
		}

		page := out.Viewer.AssignedIssues
		for _, n := range page.Nodes {
			t, _ := time.Parse(time.RFC3339, n.UpdatedAt)
			done, _ := time.Parse(time.RFC3339, n.CompletedAt)
			started, _ := time.Parse(time.RFC3339, n.StartedAt)
			var labels []string
			for _, l := range n.Labels.Nodes {
				labels = append(labels, l.Name)
			}
			items = append(items, Item{
				ExternalID: n.ID, Identifier: n.Identifier, Title: n.Title,
				State: n.State.Name, StateType: n.State.Type, URL: n.URL, UpdatedAt: t.UTC(),
				Estimate: n.Estimate, Project: n.Project.Name, Labels: labels, CompletedAt: done.UTC(),
				StartedAt: started.UTC(),
			})
		}
		if !page.PageInfo.HasNextPage {
			break
		}
		// A page that claims more but makes no progress would loop forever.
		if len(page.Nodes) == 0 || page.PageInfo.EndCursor == "" || page.PageInfo.EndCursor == after {
			return nil, fmt.Errorf("linear: paging made no progress")
		}
		after = page.PageInfo.EndCursor
	}
	return items, nil
}

// MoveIssue sets an issue's state to its team's first state of stateType
// (lowest position), e.g. "completed" or "started", and returns the new state
// name. This is the one write the client makes, on the user's explicit click.
func (c *Client) MoveIssue(ctx context.Context, apiKey, issueID, stateType string) (string, error) {
	var states struct {
		Issue struct {
			Team struct {
				States struct {
					Nodes []struct {
						ID       string  `json:"id"`
						Name     string  `json:"name"`
						Position float64 `json:"position"`
					} `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}
	if err := c.call(ctx, apiKey, teamStatesQuery, map[string]any{"id": issueID, "type": stateType}, &states); err != nil {
		return "", err
	}
	nodes := states.Issue.Team.States.Nodes
	if len(nodes) == 0 {
		return "", fmt.Errorf("linear: the team has no %s state", stateType)
	}
	best := nodes[0]
	for _, n := range nodes[1:] {
		if n.Position < best.Position {
			best = n
		}
	}
	var upd struct {
		IssueUpdate struct {
			Success bool `json:"success"`
			Issue   struct {
				State struct {
					Name string `json:"name"`
				} `json:"state"`
			} `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := c.call(ctx, apiKey, issueStateUpdate, map[string]any{"id": issueID, "stateId": best.ID}, &upd); err != nil {
		return "", err
	}
	if !upd.IssueUpdate.Success {
		return "", fmt.Errorf("linear: the update was refused")
	}
	return upd.IssueUpdate.Issue.State.Name, nil
}

// call posts one GraphQL request and decodes its data into out. Neither the
// key nor the upstream body ever goes into an error.
func (c *Client) call(ctx context.Context, apiKey, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)
	resp, err := c.httpDo(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("linear: unexpected status %d", resp.StatusCode)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct{}      `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("linear: decode response: %w", err)
	}
	if len(env.Errors) > 0 || len(env.Data) == 0 || string(env.Data) == "null" {
		return fmt.Errorf("linear: the request was refused")
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("linear: decode response: %w", err)
	}
	return nil
}
