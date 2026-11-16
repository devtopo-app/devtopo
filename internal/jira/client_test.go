package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAssignedIssuesPagesSearchJQL(t *testing.T) {
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			fmt.Fprint(w, `{"accountId":"x"}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/jql" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "me@example.com" || p != "jira_test_token" {
			t.Errorf("basic auth: %q %q %v", u, p, ok)
		}
		var body struct {
			JQL           string   `json:"jql"`
			Fields        []string `json:"fields"`
			NextPageToken string   `json:"nextPageToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.JQL, "assignee = currentUser()") || !strings.Contains(body.JQL, "(statusCategory != Done OR statusCategoryChangedDate >= -14d)") {
			t.Errorf("jql: %q", body.JQL)
		}
		tokens = append(tokens, body.NextPageToken)
		if body.NextPageToken == "" {
			fmt.Fprint(w, `{"issues":[{"key":"ABC-1","fields":{"summary":"Fix X","status":{"name":"In Progress"},"updated":"2026-10-28T09:00:00.000+0000","customfield_10016":3}}],"nextPageToken":"p2"}`)
			return
		}
		fmt.Fprint(w, `{"issues":[{"key":"ABC-2","fields":{"summary":"Fix Y","status":{"name":"To Do"},"updated":"2026-10-27T09:00:00.000+0000","customfield_10016":null}}],"isLast":true}`)
	}))
	defer srv.Close()

	items, err := NewClient().AssignedIssues(context.Background(), srv.URL, "me@example.com", "jira_test_token")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || len(tokens) != 2 || tokens[1] != "p2" {
		t.Fatalf("want 2 items over 2 pages, got %d items, tokens %q", len(items), tokens)
	}
	it := items[0]
	if it.Key != "ABC-1" || it.Title != "Fix X" || it.State != "In Progress" || it.URL != srv.URL+"/browse/ABC-1" {
		t.Fatalf("item: %+v", it)
	}
	if it.Points != 3 || items[1].Points != 0 {
		t.Fatalf("points: want 3 and 0 (null = no estimate), got %v and %v", it.Points, items[1].Points)
	}
	if it.UpdatedAt.IsZero() {
		t.Fatal("updated not parsed")
	}
}

func TestAssignedIssuesErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errorMessages":["jira_SECRET is not valid"]}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := NewClient().AssignedIssues(context.Background(), srv.URL, "me@example.com", "jira_SECRET")
	if err == nil {
		t.Fatal("expected an error on 401")
	}
	if strings.Contains(err.Error(), "jira_SECRET") {
		t.Fatalf("the error must never contain the token or the upstream body: %v", err)
	}
}

func TestMyselfValidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); r.URL.Path != "/rest/api/3/myself" || u != "me@example.com" || p != "jira_test_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"accountId":"x"}`)
	}))
	defer srv.Close()
	c := NewClient()
	if err := c.Myself(context.Background(), srv.URL, "me@example.com", "jira_test_token"); err != nil {
		t.Fatalf("good credentials: %v", err)
	}
	if err := c.Myself(context.Background(), srv.URL, "me@example.com", "wrong"); err == nil {
		t.Fatal("bad credentials must fail")
	}
}

func TestMyselfRejectsASiteThatIsNotJira(t *testing.T) {
	// "atlassian.net" with no team redirects to a marketing page that answers 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html>Atlassian</html>`)
	}))
	defer srv.Close()
	if err := NewClient().Myself(context.Background(), srv.URL, "me@example.com", "jira_test_token"); !errors.Is(err, ErrNotJira) {
		t.Fatalf("a page that is not a Jira account must fail as not Jira: %v", err)
	}
}

func TestMyselfTellsARefusedTokenFromASiteThatIsNotJira(t *testing.T) {
	notJira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // and no tenant_info
	}))
	defer notJira.Close()
	if err := NewClient().Myself(context.Background(), notJira.URL, "me@example.com", "t"); !errors.Is(err, ErrNotJira) {
		t.Fatalf("a site with no cloud id is not Jira: %v", err)
	}

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_edge/tenant_info" {
			fmt.Fprint(w, `{"cloudId":"cloud-1"}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer site.Close()
	c := NewClient()
	c.gateway = site.URL // refuses too
	if err := c.Myself(context.Background(), site.URL, "me@example.com", "t"); !errors.Is(err, ErrRefused) {
		t.Fatalf("Jira refusing the email and token must say so: %v", err)
	}
}

func TestScopedTokenGoesThroughTheGateway(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_edge/tenant_info" {
			fmt.Fprint(w, `{"cloudId":"cloud-1"}`)
			return
		}
		if r.URL.Path == "/rest/api/3/search/jql" {
			fmt.Fprint(w, `{"issues":[]}`) // a site searches as an anonymous visitor
			return
		}
		w.WriteHeader(http.StatusUnauthorized) // a site refuses a scoped token
	}))
	defer site.Close()
	var paths []string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "me@example.com" || p != "scoped_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, `{"accountId":"x","issues":[{"key":"ABC-1","fields":{"summary":"Fix X"}}]}`)
	}))
	defer gateway.Close()

	c := NewClient()
	c.gateway = gateway.URL
	if err := c.Myself(context.Background(), site.URL, "me@example.com", "scoped_token"); err != nil {
		t.Fatalf("a scoped token must pass the check: %v", err)
	}
	items, err := c.AssignedIssues(context.Background(), site.URL, "me@example.com", "scoped_token")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != site.URL+"/browse/ABC-1" {
		t.Fatalf("ticket links stay on the site: %+v", items)
	}
	if strings.Join(paths, " ") != "/ex/jira/cloud-1/rest/api/3/myself /ex/jira/cloud-1/rest/api/3/search/jql" {
		t.Fatalf("gateway paths: %q", paths)
	}
}

func TestNormalizeSite(t *testing.T) {
	for in, want := range map[string]string{
		"example.atlassian.net":                     "https://example.atlassian.net",
		" https://example.atlassian.net/ ":          "https://example.atlassian.net",
		"http://example.atlassian.net/jira/for-you": "https://example.atlassian.net",
	} {
		got, err := NormalizeSite(in)
		if err != nil || got != want {
			t.Errorf("NormalizeSite(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeSite("  "); err == nil {
		t.Error("an empty site must be an error")
	}
}

func TestAssignedIssuesReadsStatusCategory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"issues":[{"key":"ABC-1","fields":{"status":{"name":"In Review","statusCategory":{"key":"indeterminate"}}}}]}`)
	}))
	defer srv.Close()
	items, err := NewClient().AssignedIssues(context.Background(), srv.URL, "me@example.com", "jira_test_token")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Category != "indeterminate" {
		t.Fatalf("status category: %+v", items)
	}
}

func TestAssignedIssuesReadsProjectAndLabels(t *testing.T) {
	var fields []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Fields []string `json:"fields"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fields = body.Fields
		fmt.Fprint(w, `{"issues":[{"key":"ABC-1","fields":{"project":{"name":"Platform"},"labels":["bug","infra"]}}]}`)
	}))
	defer srv.Close()
	items, err := NewClient().AssignedIssues(context.Background(), srv.URL, "me@example.com", "jira_test_token")
	if err != nil {
		t.Fatal(err)
	}
	if f := strings.Join(fields, ","); !strings.Contains(f, "project") || !strings.Contains(f, "labels") {
		t.Fatalf("request must ask for project and labels: %q", f)
	}
	if len(items) != 1 || items[0].Project != "Platform" || strings.Join(items[0].Labels, ",") != "bug,infra" {
		t.Fatalf("project and labels: %+v", items)
	}
}

func TestAssignedIssuesReadsStatusCategoryChangeDate(t *testing.T) {
	var fields []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Fields []string `json:"fields"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fields = body.Fields
		fmt.Fprint(w, `{"issues":[{"key":"ABC-1","fields":{"statuscategorychangedate":"2026-10-29T10:30:00.000+0000"}}]}`)
	}))
	defer srv.Close()
	items, err := NewClient().AssignedIssues(context.Background(), srv.URL, "me@example.com", "jira_test_token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(fields, ","), "statuscategorychangedate") {
		t.Fatalf("request must ask for statuscategorychangedate: %q", fields)
	}
	if len(items) != 1 || !items[0].CategoryChangedAt.Equal(time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("category changed at: %+v", items)
	}
}

func TestTransitionAppliesTheTransitionToTheCategory(t *testing.T) {
	var applied string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			fmt.Fprint(w, `{"accountId":"x"}`)
			return
		}
		if u, p, _ := r.BasicAuth(); u != "me@example.com" || p != "jira_test_token" || r.URL.Path != "/rest/api/3/issue/ABC-1/transitions" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"transitions":[{"id":"11","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}},{"id":"31","to":{"name":"Done","statusCategory":{"key":"done"}}}]}`)
			return
		}
		var body struct {
			Transition struct {
				ID string `json:"id"`
			} `json:"transition"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		applied = body.Transition.ID
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	name, err := NewClient().Transition(context.Background(), srv.URL, "me@example.com", "jira_test_token", "ABC-1", "done")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Done" || applied != "31" {
		t.Fatalf("want Done via 31, got %q via %q", name, applied)
	}
}

func TestTransitionWithNoMatchFails(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted = true
		}
		fmt.Fprint(w, `{"transitions":[{"id":"31","to":{"name":"Done","statusCategory":{"key":"done"}}}]}`)
	}))
	defer srv.Close()
	if _, err := NewClient().Transition(context.Background(), srv.URL, "me@example.com", "jira_test_token", "ABC-1", "indeterminate"); err == nil || posted {
		t.Fatalf("no transition to the category must fail without a write: err=%v posted=%v", err, posted)
	}
}

// A search that keeps handing back the same page token, or a token with no
// issues, must stop with an error rather than loop forever.
func TestAssignedIssuesStuckPagingIsAnError(t *testing.T) {
	for name, page := range map[string]string{
		"same token": `{"issues":[{"key":"ABC-1","fields":{"summary":"Fix X"}}],"nextPageToken":"p1"}`,
		"empty page": `{"issues":[],"nextPageToken":"p9"}`,
	} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/rest/api/3/myself" {
				fmt.Fprint(w, `{"accountId":"x"}`)
				return
			}
			if calls++; calls > 10 {
				t.Errorf("%s: paging did not stop", name)
				fmt.Fprint(w, `{"issues":[]}`)
				return
			}
			fmt.Fprint(w, page)
		}))
		if items, err := NewClient().AssignedIssues(context.Background(), srv.URL, "me@example.com", "jira_test_token"); err == nil {
			t.Errorf("%s: want an error, got %d items", name, len(items))
		}
		srv.Close()
	}
}

// A 200 reply that is not a search result (no "issues") is an error, not an
// empty list that would wipe the stored items.
func TestAssignedIssuesWithoutIssuesListIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			fmt.Fprint(w, `{"accountId":"x"}`)
			return
		}
		fmt.Fprint(w, `{"errorMessages":["something went wrong"]}`)
	}))
	defer srv.Close()
	if items, err := NewClient().AssignedIssues(context.Background(), srv.URL, "me@example.com", "jira_test_token"); err == nil {
		t.Fatalf("want an error, got %+v", items)
	}
}
