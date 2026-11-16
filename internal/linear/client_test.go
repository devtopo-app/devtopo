package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func fakeResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestAssignedIssuesParsesGraphQL(t *testing.T) {
	var gotAuth, gotURL, gotBody string
	c := &Client{Endpoint: "https://api.linear.app/graphql", httpDo: func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		gotURL = r.URL.String()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"abc","identifier":"ENG-1","title":"Fix X","url":"https://linear.app/x/issue/ENG-1","updatedAt":"2026-10-28T09:00:00Z","state":{"name":"In Progress"}}]}}}}`), nil
	}}
	items, err := c.AssignedIssues(context.Background(), "lin_api_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1, got %d", len(items))
	}
	it := items[0]
	if it.Identifier != "ENG-1" || it.Title != "Fix X" || it.ExternalID != "abc" || it.URL == "" || it.State != "In Progress" {
		t.Fatalf("item: %+v", it)
	}
	if it.UpdatedAt.IsZero() {
		t.Fatal("updatedAt not parsed")
	}
	if gotAuth != "lin_api_test" {
		t.Fatalf("Authorization must be the raw key, got %q", gotAuth)
	}
	if gotURL != "https://api.linear.app/graphql" {
		t.Fatalf("url: %q", gotURL)
	}
	if !strings.Contains(gotBody, "assignedIssues") {
		t.Fatalf("request body missing the query: %q", gotBody)
	}
}

func TestAssignedIssuesErrorHidesKey(t *testing.T) {
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		return fakeResp(401, `{"errors":[{"message":"authentication failed"}]}`), nil
	}}
	_, err := c.AssignedIssues(context.Background(), "lin_api_SECRET")
	if err == nil {
		t.Fatal("expected an error on 401")
	}
	if strings.Contains(err.Error(), "lin_api_SECRET") {
		t.Fatalf("the error must never contain the key: %v", err)
	}
}

func TestAssignedIssuesReadsEstimate(t *testing.T) {
	var gotBody string
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a","estimate":3},{"id":"b","estimate":null}]}}}}`), nil
	}}
	items, err := c.AssignedIssues(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "estimate") {
		t.Fatalf("query must ask for the estimate: %q", gotBody)
	}
	if len(items) != 2 || items[0].Estimate != 3 || items[1].Estimate != 0 {
		t.Fatalf("estimates: %+v", items)
	}
}

func TestAssignedIssuesReadsStateType(t *testing.T) {
	var gotBody string
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a","state":{"name":"In Review","type":"started"}}]}}}}`), nil
	}}
	items, err := c.AssignedIssues(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "state { name type }") {
		t.Fatalf("query must ask for the state type: %q", gotBody)
	}
	if len(items) != 1 || items[0].StateType != "started" {
		t.Fatalf("state type: %+v", items)
	}
}

func TestAssignedIssuesReadsProjectAndLabels(t *testing.T) {
	var gotBody string
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a","project":{"name":"Onboarding"},"labels":{"nodes":[{"name":"bug"},{"name":"infra"}]}},{"id":"b","project":null,"labels":{"nodes":[]}}]}}}}`), nil
	}}
	items, err := c.AssignedIssues(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "project { name }") || !strings.Contains(gotBody, "labels { nodes { name } }") {
		t.Fatalf("query must ask for the project and labels: %q", gotBody)
	}
	if len(items) != 2 || items[0].Project != "Onboarding" || strings.Join(items[0].Labels, ",") != "bug,infra" || items[1].Project != "" || len(items[1].Labels) != 0 {
		t.Fatalf("project and labels: %+v", items)
	}
}

func TestAssignedIssuesIncludesRecentlyFinished(t *testing.T) {
	var gotBody string
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[]}}}}`), nil
	}}
	if _, err := c.AssignedIssues(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	// open issues, or ones completed or canceled in the last 14 days
	for _, want := range []string{`or: [`, `completedAt: { gte: \"-P14D\" }`, `canceledAt: { gte: \"-P14D\" }`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("query must contain %s: %q", want, gotBody)
		}
	}
}

func TestAssignedIssuesReadsCompletedAt(t *testing.T) {
	var gotBody string
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a","completedAt":"2026-10-29T10:30:00.000Z"},{"id":"b","completedAt":null}]}}}}`), nil
	}}
	items, err := c.AssignedIssues(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, " completedAt ") {
		t.Fatalf("query must ask for completedAt: %q", gotBody)
	}
	if len(items) != 2 || !items[0].CompletedAt.Equal(time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)) || !items[1].CompletedAt.IsZero() {
		t.Fatalf("completed at: %+v", items)
	}
}

func TestAssignedIssuesPagesWithCursor(t *testing.T) {
	var afters []any
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		afters = append(afters, body.Variables["after"])
		if body.Variables["after"] == nil {
			return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a"}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}}`), nil
		}
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"b"}],"pageInfo":{"hasNextPage":false,"endCursor":"c2"}}}}}`), nil
	}}
	items, err := c.AssignedIssues(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ExternalID != "a" || items[1].ExternalID != "b" {
		t.Fatalf("want both pages, got %+v", items)
	}
	if len(afters) != 2 || afters[0] != nil || afters[1] != "c1" {
		t.Fatalf("cursors sent: %v", afters)
	}
}

func TestAssignedIssuesReadsStartedAt(t *testing.T) {
	var gotBody string
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return fakeResp(200, `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a","startedAt":"2026-10-29T09:00:00.000Z"},{"id":"b","startedAt":null}]}}}}`), nil
	}}
	items, err := c.AssignedIssues(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, " startedAt ") {
		t.Fatalf("query must ask for startedAt: %q", gotBody)
	}
	if len(items) != 2 || !items[0].StartedAt.Equal(time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)) || !items[1].StartedAt.IsZero() {
		t.Fatalf("started at: %+v", items)
	}
}

func TestMoveIssueSetsTheTeamsFirstStateOfType(t *testing.T) {
	var calls []map[string]any
	c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, body)
		if r.Header.Get("Authorization") != "lin_api_test" {
			t.Errorf("auth: %q", r.Header.Get("Authorization"))
		}
		if strings.Contains(body["query"].(string), "mutation") {
			return fakeResp(200, `{"data":{"issueUpdate":{"success":true,"issue":{"state":{"name":"Done"}}}}}`), nil
		}
		return fakeResp(200, `{"data":{"issue":{"team":{"states":{"nodes":[{"id":"s2","name":"Shipped","position":5},{"id":"s1","name":"Done","position":2}]}}}}}`), nil
	}}
	name, err := c.MoveIssue(context.Background(), "lin_api_test", "abc", "completed")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Done" || len(calls) != 2 {
		t.Fatalf("name %q after %d calls", name, len(calls))
	}
	if v := calls[0]["variables"].(map[string]any); v["id"] != "abc" || v["type"] != "completed" {
		t.Fatalf("states lookup variables: %v", v)
	}
	if v := calls[1]["variables"].(map[string]any); v["id"] != "abc" || v["stateId"] != "s1" {
		t.Fatalf("update must pick the lowest position: %v", v)
	}
}

func TestMoveIssueRefusedHidesKey(t *testing.T) {
	for name, resp := range map[string]*http.Response{
		"status":    fakeResp(401, `{"errors":[{"message":"lin_api_SECRET bad"}]}`),
		"gql error": fakeResp(200, `{"errors":[{"message":"lin_api_SECRET bad"}]}`),
		"no state":  fakeResp(200, `{"data":{"issue":{"team":{"states":{"nodes":[]}}}}}`),
	} {
		c := &Client{Endpoint: "x", httpDo: func(*http.Request) (*http.Response, error) { return resp, nil }}
		_, err := c.MoveIssue(context.Background(), "lin_api_SECRET", "abc", "started")
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: want an error without the key, got %v", name, err)
		}
	}
}

// A 200 reply that carries GraphQL errors (data null, or partial) is an
// error, never an empty list of issues.
func TestAssignedIssuesGraphQLErrorsAreAnError(t *testing.T) {
	for name, body := range map[string]string{
		"data null":    `{"data":null,"errors":[{"message":"rate limited"}]}`,
		"partial data": `{"data":{"viewer":{"assignedIssues":{"nodes":[]}}},"errors":[{"message":"field failed"}]}`,
		"no viewer":    `{"data":{"viewer":null}}`,
		"empty object": `{}`,
	} {
		c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
			return fakeResp(200, body), nil
		}}
		if items, err := c.AssignedIssues(context.Background(), "k"); err == nil {
			t.Errorf("%s: want an error, got %+v", name, items)
		}
	}
}

// A page that says there is more but gives no new cursor or no issues must
// stop with an error rather than ask for the same page forever.
func TestAssignedIssuesStuckPagingIsAnError(t *testing.T) {
	for name, body := range map[string]string{
		"same cursor":  `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a"}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}}`,
		"empty cursor": `{"data":{"viewer":{"assignedIssues":{"nodes":[{"id":"a"}],"pageInfo":{"hasNextPage":true,"endCursor":""}}}}}`,
		"empty page":   `{"data":{"viewer":{"assignedIssues":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"c9"}}}}}`,
	} {
		calls := 0
		c := &Client{Endpoint: "x", httpDo: func(r *http.Request) (*http.Response, error) {
			calls++
			if calls > 10 {
				t.Fatalf("%s: paging did not stop", name)
			}
			return fakeResp(200, body), nil
		}}
		if items, err := c.AssignedIssues(context.Background(), "k"); err == nil {
			t.Errorf("%s: want an error, got %d items", name, len(items))
		}
	}
}
