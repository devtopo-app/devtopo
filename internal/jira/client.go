// Package jira reads a developer's assigned Jira Cloud issues through the REST
// v3 API, using the user's own email and API token (basic auth). It is a direct
// vendor API call with the user's own token (not scraping, so no proxy). It
// reads myself and search/jql; its one write is Transition, run only when the
// user marks a ticket done or not done. The token is never logged or returned.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Issues assigned to the token's owner that are open or finished in the last
// 14 days (so a planned ticket shows as done), newest first. maxIssues caps the
// paging so a huge backlog cannot stall a sync.
const (
	assignedJQL = `assignee = currentUser() AND (statusCategory != Done OR statusCategoryChangedDate >= -14d) ORDER BY updated DESC`
	maxIssues   = 200
)

// Item is one assigned Jira issue.
type Item struct {
	Key   string
	Title string
	State string
	// Category is the status category key: new, indeterminate or done.
	Category string
	URL      string
	// Points is the "Story point estimate" (customfield_10016); 0 when unset.
	Points    float64
	UpdatedAt time.Time
	Project   string
	Labels    []string
	// CategoryChangedAt is when the status category last changed (statuscategorychangedate).
	CategoryChangedAt time.Time
}

// ErrNotJira and ErrRefused tell the user which part of a connect failed: the
// address is not a Jira site, or Jira refused the email and token.
var (
	ErrNotJira = errors.New("jira: not a Jira site")
	ErrRefused = errors.New("jira: the email and token were refused")
)

// Client calls the Jira Cloud REST API of whichever site it is given.
type Client struct {
	hc *http.Client
	// gateway is where a scoped API token is accepted; a site refuses one.
	gateway string
}

// NewClient builds the production client.
func NewClient() *Client {
	return &Client{hc: &http.Client{Timeout: 20 * time.Second}, gateway: "https://api.atlassian.com"}
}

// NormalizeSite turns what a user pastes ("acme.atlassian.net", a browser URL)
// into "https://<host>" with no path or trailing slash.
func NormalizeSite(site string) (string, error) {
	site = strings.TrimSpace(site)
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	u, err := url.Parse(site)
	if err != nil || u.Host == "" {
		return "", errors.New("jira: invalid site")
	}
	return "https://" + u.Host, nil
}

// Myself checks the credentials against GET /rest/api/3/myself. The reply must
// name an account: a wrong site can still answer 200 with a web page.
func (c *Client) Myself(ctx context.Context, site, email, token string) error {
	b, err := c.base(ctx, site, email, token)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodGet, b, "/rest/api/3/myself", email, token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var me struct {
		AccountID string `json:"accountId"`
	}
	if json.NewDecoder(resp.Body).Decode(&me) != nil || me.AccountID == "" {
		return ErrNotJira
	}
	return nil
}

// AssignedIssues fetches the authenticated user's open assigned issues, paging
// with nextPageToken up to maxIssues.
func (c *Client) AssignedIssues(ctx context.Context, site, email, token string) ([]Item, error) {
	b, err := c.base(ctx, site, email, token)
	if err != nil {
		return nil, err
	}
	var items []Item
	next := ""
	for len(items) < maxIssues {
		req := map[string]any{"jql": assignedJQL, "fields": []string{"summary", "status", "updated", "customfield_10016", "project", "labels", "statuscategorychangedate"}, "maxResults": 100}
		if next != "" {
			req["nextPageToken"] = next
		}
		body, _ := json.Marshal(req)
		resp, err := c.do(ctx, http.MethodPost, b, "/rest/api/3/search/jql", email, token, body)
		if err != nil {
			return nil, err
		}
		var out struct {
			Issues []struct { // nil when the reply has no "issues"
				Key    string `json:"key"`
				Fields struct {
					Summary string  `json:"summary"`
					Updated string  `json:"updated"`
					Points  float64 `json:"customfield_10016"` // null decodes to 0
					Status  struct {
						Name           string `json:"name"`
						StatusCategory struct {
							Key string `json:"key"`
						} `json:"statusCategory"`
					} `json:"status"`
					Project struct {
						Name string `json:"name"`
					} `json:"project"`
					Labels          []string `json:"labels"`
					CategoryChanged string   `json:"statuscategorychangedate"`
				} `json:"fields"`
			} `json:"issues"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("jira: decode response: %w", err)
		}
		// A reply with no issues list at all is not "nothing assigned": it is
		// something other than a search result, and must not wipe the items.
		if out.Issues == nil {
			return nil, fmt.Errorf("jira: the response has no issues list")
		}
		for _, is := range out.Issues {
			t, _ := time.Parse("2006-01-02T15:04:05.000-0700", is.Fields.Updated)
			changed, _ := time.Parse("2006-01-02T15:04:05.000-0700", is.Fields.CategoryChanged)
			items = append(items, Item{
				Key: is.Key, Title: is.Fields.Summary, State: is.Fields.Status.Name, Category: is.Fields.Status.StatusCategory.Key,
				URL: site + "/browse/" + is.Key, Points: is.Fields.Points, UpdatedAt: t.UTC(),
				Project: is.Fields.Project.Name, Labels: is.Fields.Labels, CategoryChangedAt: changed.UTC(),
			})
		}
		if out.NextPageToken == "" {
			break
		}
		// A page that claims more but makes no progress would loop forever.
		if len(out.Issues) == 0 || out.NextPageToken == next {
			return nil, fmt.Errorf("jira: paging made no progress")
		}
		next = out.NextPageToken
	}
	return items, nil
}

// Transition applies the issue's first available transition whose target
// status is in category ("done" or "indeterminate") and returns that status's
// name. This is the one write the client makes, on the user's explicit click.
func (c *Client) Transition(ctx context.Context, site, email, token, key, category string) (string, error) {
	b, err := c.base(ctx, site, email, token)
	if err != nil {
		return "", err
	}
	p := "/rest/api/3/issue/" + url.PathEscape(key) + "/transitions"
	resp, err := c.do(ctx, http.MethodGet, b, p, email, token, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		Transitions []struct {
			ID string `json:"id"`
			To struct {
				Name           string `json:"name"`
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"to"`
		} `json:"transitions"`
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("jira: decode response: %w", err)
	}
	for _, tr := range out.Transitions {
		if tr.To.StatusCategory.Key != category {
			continue
		}
		body, _ := json.Marshal(map[string]any{"transition": map[string]string{"id": tr.ID}})
		resp, err := c.do(ctx, http.MethodPost, b, p, email, token, body)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		return tr.To.Name, nil
	}
	return "", fmt.Errorf("jira: no transition to a %s status", category)
}

// base picks where to send the API calls for site. A scoped API token works
// only through the gateway: the site answers 401 to "who am I", but searches as
// an anonymous visitor and finds nothing, so the choice is made on myself.
func (c *Client) base(ctx context.Context, site, email, token string) (string, error) {
	resp, err := c.send(ctx, http.MethodGet, site+"/rest/api/3/myself", email, token, nil)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		return site, nil
	}
	id, err := c.cloudID(ctx, site)
	if err != nil {
		return "", err
	}
	return c.gateway + "/ex/jira/" + id, nil
}

// do sends one authenticated request for path under base and returns the
// response only on 2xx (a transition answers 204).
func (c *Client) do(ctx context.Context, method, base, path, email, token string, body []byte) (*http.Response, error) {
	resp, err := c.send(ctx, method, base+path, email, token, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Drain but do not include the body or the token in the error.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, ErrRefused
		}
		return nil, fmt.Errorf("jira: unexpected status %d", resp.StatusCode)
	}
	return resp, nil
}

// cloudID reads the site's cloud ID from its public tenant_info, which needs
// no credentials.
func (c *Client) cloudID(ctx context.Context, site string) (string, error) {
	resp, err := c.send(ctx, http.MethodGet, site+"/_edge/tenant_info", "", "", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		CloudID string `json:"cloudId"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil || out.CloudID == "" {
		return "", ErrNotJira
	}
	return out.CloudID, nil
}

// send makes one request, with basic auth when an email is given.
func (c *Client) send(ctx context.Context, method, u, email, token string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if email != "" {
		req.SetBasicAuth(email, token)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.hc.Do(req)
}
