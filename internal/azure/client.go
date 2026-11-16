// Package azure reads a developer's pull requests through the Azure DevOps REST
// API, with the user's own personal access token (Code: Read is all it needs). It
// works with Azure DevOps Services and a self-hosted Azure DevOps Server. It is
// strictly read-only. The token is never logged or returned.
package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
	"github.com/devtopo-app/devtopo/internal/store"
)

// ErrNotFound and ErrRefused tell the user which part of a connect failed: there
// is no such organization, or it refused the token.
var (
	ErrNotFound = errors.New("azure: no such organization")
	ErrRefused  = errors.New("azure: the token was refused")
)

const (
	apiVersion = "7.1"
	// pageSize is how many PRs one call asks for.
	pageSize = 100
	// anonymous is who the service says you are when it didn't accept the token.
	anonymous = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	// doneScan caps the PRs looked at per project for reviews you finished lately,
	// and doneThreads the threads read for them in one pass. They only feed a count,
	// and a busy organization delays or refuses a client that asks too much.
	doneScan    = 50
	doneThreads = 30
	// remembered is how long a client keeps who you are and your projects, so one
	// sync's fetches ask once.
	remembered = time.Minute
)

// Client calls the REST API of whichever organization it is given.
type Client struct {
	hc *http.Client

	mu       sync.Mutex
	memoKey  string
	memoAt   time.Time
	me, name string
	projs    []string
}

// memo returns what is remembered for this organization and token, when still fresh.
func (c *Client) memo(base, token string) (me, name string, projs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.memoKey != base+"\x00"+token || time.Since(c.memoAt) > remembered {
		c.memoKey, c.memoAt, c.me, c.name, c.projs = base+"\x00"+token, time.Now(), "", "", nil
	}
	return c.me, c.name, c.projs
}

// NewClient builds the production client. It never follows a redirect: a bad token
// is sent to a sign-in page, which must read as refused, not as an answer.
func NewClient() *Client {
	return &Client{hc: &http.Client{
		// Generous: a busy organization delays answers before it refuses them.
		Timeout:       45 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// NormalizeOrganization turns what a user pastes (a name, a browser URL) into the
// organization's base URL with no trailing slash. A self-hosted server keeps its
// path, which names its collection.
func NormalizeOrganization(org string) (string, error) {
	org = strings.TrimSpace(org)
	invalid := errors.New("azure: invalid organization")
	if org == "" {
		return "", invalid
	}
	if !strings.Contains(org, "/") && !strings.Contains(org, ".") {
		if strings.ContainsAny(org, " \t") {
			return "", invalid
		}
		return "https://dev.azure.com/" + org, nil
	}
	if !strings.Contains(org, "://") {
		org = "https://" + org
	}
	u, err := url.Parse(org)
	if err != nil || u.Hostname() == "" {
		return "", invalid
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case u.Hostname() == "dev.azure.com":
		if parts[0] == "" {
			return "", invalid
		}
		return u.Scheme + "://" + u.Host + "/" + parts[0], nil
	case strings.HasSuffix(u.Hostname(), ".visualstudio.com"):
		return u.Scheme + "://" + u.Host, nil
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

// statusError is an answer that wasn't usable. It holds no body, which could echo
// a secret.
type statusError struct{ code int }

func (e statusError) Error() string { return "azure: status " + strconv.Itoa(e.code) }

func (c *Client) get(ctx context.Context, base, token, path string, q url.Values, out any) error {
	target := base + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return errors.New("azure: bad request")
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+token)))
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNonAuthoritativeInfo, // the sign-in page
		resp.StatusCode >= 300 && resp.StatusCode < 400:    // on its way there
		return fmt.Errorf("%w (status %d)", ErrRefused, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w (status %d)", ErrNotFound, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return statusError{resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: unreadable answer", ErrRefused)
	}
	return nil
}

// versioned adds the API version every documented call needs to its query.
func versioned(q url.Values) url.Values {
	if q == nil {
		q = url.Values{}
	}
	q.Set("api-version", apiVersion)
	return q
}

// Me checks the token and returns its owner's id and name.
func (c *Client) Me(ctx context.Context, base, token string) (id, name string, err error) {
	if id, name, _ = c.memo(base, token); id != "" {
		return id, name, nil
	}
	var out struct {
		AuthenticatedUser struct {
			ID   string `json:"id"`
			Name string `json:"providerDisplayName"`
		} `json:"authenticatedUser"`
	}
	// connectionData is undocumented, and the official clients call it with no
	// version: one above what it supports would be refused.
	if err := c.get(ctx, base, token, "/_apis/connectionData", nil, &out); err != nil {
		return "", "", err
	}
	if id = out.AuthenticatedUser.ID; id == "" || id == anonymous {
		return "", "", ErrRefused
	}
	c.mu.Lock()
	c.me, c.name = id, out.AuthenticatedUser.Name
	c.mu.Unlock()
	return id, out.AuthenticatedUser.Name, nil
}

// projects lists the projects that have a repository the token can see. Listing
// repositories needs only Code: Read, where listing projects needs another scope.
func (c *Client) projects(ctx context.Context, base, token string) ([]string, error) {
	if _, _, known := c.memo(base, token); known != nil {
		return known, nil
	}
	var out struct {
		Value []struct {
			IsDisabled bool `json:"isDisabled"`
			Project    struct {
				Name string `json:"name"`
			} `json:"project"`
		} `json:"value"`
	}
	if err := c.get(ctx, base, token, "/_apis/git/repositories", versioned(nil), &out); err != nil {
		return nil, err
	}
	names := []string{}
	seen := map[string]bool{}
	for _, r := range out.Value {
		if n := r.Project.Name; n != "" && !seen[n] && !r.IsDisabled {
			seen[n] = true
			names = append(names, n)
		}
	}
	c.mu.Lock()
	c.projs = names
	c.mu.Unlock()
	return names, nil
}

type identity struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	IsContainer bool   `json:"isContainer"`
}

// countsFrom reports whether the identity is another person: not the viewer, a
// team, or a service. Identities have no bot flag; pipelines post as a Build Service.
func (i identity) countsFrom(viewer string) bool {
	return i.ID != "" && i.ID != viewer && !i.IsContainer && !strings.Contains(i.DisplayName, "Build Service")
}

type reviewer struct {
	identity
	Vote        int  `json:"vote"`
	HasDeclined bool `json:"hasDeclined"`
}

type pullRequest struct {
	ID         int    `json:"pullRequestId"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Created    string `json:"creationDate"`
	Closed     string `json:"closedDate"`
	Repository struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Project struct {
			Name string `json:"name"`
		} `json:"project"`
	} `json:"repository"`
	Reviewers []reviewer `json:"reviewers"`
}

func (p pullRequest) created() time.Time {
	t, _ := time.Parse(time.RFC3339, p.Created)
	return t.UTC()
}

func (p pullRequest) repo() string { return p.Repository.Project.Name + "/" + p.Repository.Name }

func (p pullRequest) url(base string) string {
	return prURL(base, p.Repository.Project.Name, p.Repository.Name, p.ID)
}

// prURL is where a PR opens in the browser; the API's own url field is its REST one.
func prURL(base, project, repo string, id int) string {
	return base + "/" + url.PathEscape(project) + "/_git/" + url.PathEscape(repo) + "/pullrequest/" + strconv.Itoa(id)
}

// search reads every page of a PR search in one project.
func (c *Client) search(ctx context.Context, base, token, project string, criteria url.Values) ([]pullRequest, error) {
	var all []pullRequest
	for {
		q := url.Values{"$top": {strconv.Itoa(pageSize)}, "$skip": {strconv.Itoa(len(all))}}
		for k, v := range criteria {
			q["searchCriteria."+k] = v
		}
		var page struct {
			Value []pullRequest `json:"value"`
		}
		if err := c.get(ctx, base, token, "/"+url.PathEscape(project)+"/_apis/git/pullrequests", versioned(q), &page); err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		if len(page.Value) < pageSize {
			return all, nil
		}
	}
}

// searchAll runs a PR search in every project. Any failure fails it: a partial list
// would make what's missing look closed.
func (c *Client) searchAll(ctx context.Context, base, token string, criteria url.Values) ([]pullRequest, error) {
	projects, err := c.projects(ctx, base, token)
	if err != nil {
		return nil, err
	}
	var all []pullRequest
	for _, p := range projects {
		found, err := c.search(ctx, base, token, p, criteria)
		if err != nil {
			return nil, err
		}
		all = append(all, found...)
	}
	return all, nil
}

// property is one of a thread's properties, whose value may be a string or a number.
type property struct {
	Value any `json:"$value"`
}

func (p property) String() string { return fmt.Sprint(p.Value) }

type thread struct {
	Published  string              `json:"publishedDate"`
	IsDeleted  bool                `json:"isDeleted"`
	Properties map[string]property `json:"properties"`
	Comments   []struct {
		Author    identity `json:"author"`
		Type      string   `json:"commentType"`
		Published string   `json:"publishedDate"`
		IsDeleted bool     `json:"isDeleted"`
	} `json:"comments"`
}

func (c *Client) threads(ctx context.Context, base, token string, p pullRequest) ([]thread, error) {
	var out struct {
		Value []thread `json:"value"`
	}
	path := "/_apis/git/repositories/" + url.PathEscape(p.Repository.ID) + "/pullRequests/" + strconv.Itoa(p.ID) + "/threads"
	if err := c.get(ctx, base, token, path, versioned(nil), &out); err != nil {
		return nil, err
	}
	return out.Value, nil
}

// normID makes an id comparable: threads give some without their dashes.
func normID(id string) string { return strings.ToLower(strings.ReplaceAll(id, "-", "")) }

// spoke reads from threads when each identity last voted and last commented, by
// normID, with the name of those who commented.
func spoke(threads []thread) (voted, commented map[string]time.Time, names map[string]identity) {
	voted, commented, names = map[string]time.Time{}, map[string]time.Time{}, map[string]identity{}
	later := func(m map[string]time.Time, id, at string) {
		if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(m[id]) {
			m[id] = t.UTC()
		}
	}
	for _, th := range threads {
		if th.IsDeleted {
			continue
		}
		if th.Properties["CodeReviewThreadType"].String() == "VoteUpdate" {
			later(voted, normID(th.Properties["CodeReviewVotedByTfId"].String()), th.Published)
		}
		for _, cm := range th.Comments {
			if cm.Type == "text" && !cm.IsDeleted {
				later(commented, normID(cm.Author.ID), cm.Published)
				names[normID(cm.Author.ID)] = cm.Author
			}
		}
	}
	return voted, commented, names
}

// reviewsOf reads each person's latest word: a vote from the reviewer list, which
// a push can reset, or a comment, the nearest thing to a submitted review. Threads
// say when; the PR's own time stands in when they don't.
func reviewsOf(p pullRequest, threads []thread, viewer string) []prs.Review {
	voted, commented, names := spoke(threads)
	var out []prs.Review
	took := map[string]bool{}
	for _, r := range p.Reviewers {
		if r.Vote == 0 || !r.countsFrom(viewer) {
			continue
		}
		state := prs.Approved
		if r.Vote < 0 {
			state = prs.ChangesRequested
		}
		id := normID(r.ID)
		at := voted[id]
		if commented[id].After(at) {
			at = commented[id]
		}
		if at.IsZero() {
			at = p.created()
		}
		took[id] = true
		out = append(out, prs.Review{Reviewer: r.DisplayName, State: state, At: at})
	}
	for id, at := range commented {
		if who := names[id]; !took[id] && who.countsFrom(viewer) {
			out = append(out, prs.Review{Reviewer: who.DisplayName, State: prs.Commented, At: at})
		}
	}
	return out
}

// OwnOpenPRs returns your active PRs with their review state. Any failed read
// fails it, since a partial list would make the PRs missing from it look closed,
// and closed ones count as done.
func (c *Client) OwnOpenPRs(ctx context.Context, base, token string) ([]prs.Item, error) {
	me, _, err := c.Me(ctx, base, token)
	if err != nil {
		return nil, err
	}
	found, err := c.searchAll(ctx, base, token, url.Values{"creatorId": {me}, "status": {"active"}})
	if err != nil {
		return nil, err
	}
	items := make([]prs.Item, 0, len(found))
	for _, p := range found {
		threads, err := c.threads(ctx, base, token, p)
		if err != nil {
			return nil, err
		}
		u := p.url(base)
		it := prs.Item{Kind: "own_pr", ExternalID: store.OwnPRPrefix + u, Title: p.Title, URL: u, Repo: p.repo(), UpdatedAt: p.created()}
		it.ApplyReviews(reviewsOf(p, threads, me))
		items = append(items, it)
	}
	return items, nil
}

// ReviewRequests returns the active PRs waiting on your vote. The API gives no line
// counts, so their size stays unknown.
func (c *Client) ReviewRequests(ctx context.Context, base, token string) ([]prs.Item, error) {
	me, _, err := c.Me(ctx, base, token)
	if err != nil {
		return nil, err
	}
	found, err := c.searchAll(ctx, base, token, url.Values{"reviewerId": {me}, "status": {"active"}})
	if err != nil {
		return nil, err
	}
	items := make([]prs.Item, 0, len(found))
	for _, p := range found {
		waiting := false
		for _, r := range p.Reviewers {
			waiting = waiting || (r.ID == me && r.Vote == 0 && !r.HasDeclined)
		}
		if waiting {
			u := p.url(base)
			items = append(items, prs.Item{Kind: "review_request", ExternalID: u, Title: p.Title, URL: u, Repo: p.repo(), ReviewRequestedOfMe: true, UpdatedAt: p.created()})
		}
	}
	return items, nil
}

// ReviewsSubmitted returns the PRs you voted on since from, each at your latest
// vote, whatever state the PR is in now. Only a vote counts: a vote is the closest
// thing to GitHub's submitted review. It looks at your doneScan newest PRs per
// project, reads threads only of those still able to hold a new vote, and at most
// doneThreads of them.
func (c *Client) ReviewsSubmitted(ctx context.Context, base, token string, from time.Time) ([]prs.Item, error) {
	me, _, err := c.Me(ctx, base, token)
	if err != nil {
		return nil, err
	}
	projects, err := c.projects(ctx, base, token)
	if err != nil {
		return nil, err
	}
	items := []prs.Item{}
	reads := 0
	for _, project := range projects {
		var page struct {
			Value []pullRequest `json:"value"`
		}
		q := url.Values{"$top": {strconv.Itoa(doneScan)}, "$skip": {"0"}, "searchCriteria.reviewerId": {me}, "searchCriteria.status": {"all"}}
		if err := c.get(ctx, base, token, "/"+url.PathEscape(project)+"/_apis/git/pullrequests", versioned(q), &page); err != nil {
			return nil, err
		}
		for _, p := range page.Value {
			if closed, err := time.Parse(time.RFC3339, p.Closed); err == nil && closed.Before(from) {
				continue
			}
			votedOn := false
			for _, r := range p.Reviewers {
				votedOn = votedOn || (r.ID == me && r.Vote != 0)
			}
			if !votedOn || reads >= doneThreads {
				continue
			}
			reads++
			threads, err := c.threads(ctx, base, token, p)
			if err != nil {
				return nil, err
			}
			voted, _, _ := spoke(threads)
			if at := voted[normID(me)]; !at.IsZero() && !at.Before(from) {
				u := p.url(base)
				items = append(items, prs.Item{Kind: "review_done", ExternalID: prs.ReviewDonePrefix + u, Title: p.Title, URL: u, Repo: p.repo(), UpdatedAt: at})
			}
		}
	}
	return items, nil
}
