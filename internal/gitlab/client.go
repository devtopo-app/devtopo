// Package gitlab reads a developer's merge requests through GitLab's REST v4 API,
// on gitlab.com or a self-hosted server, with the user's own access token. It is
// strictly read-only. The token is never logged or returned.
package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
	"github.com/devtopo-app/devtopo/internal/store"
)

// ErrNotGitLab, ErrRefused and ErrBotToken tell the user which part of a connect
// failed: the address is not a GitLab server, it refused the token, or the token
// acts as a bot, which has no merge requests or reviews of its own.
var (
	ErrNotGitLab = errors.New("gitlab: not a GitLab server")
	ErrRefused   = errors.New("gitlab: the token was refused")
	ErrBotToken  = errors.New("this is a project or group access token, which acts as a bot - use a personal access token")
)

// Client calls the REST API of whichever server it is given.
type Client struct {
	hc *http.Client
}

// NewClient builds the production client. It never follows a redirect: Go only
// strips its own auth headers on one, so the token header could leave for another
// host. A redirected call just fails.
func NewClient() *Client {
	return &Client{hc: &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// NormalizeSite turns what a user pastes (a host, a browser URL) into a base URL
// with no trailing slash. gitlab.com has no path of its own, so a project's URL
// works there; a self-hosted server may live under one, so it is kept. The scheme
// is https unless the user wrote another.
func NormalizeSite(site string) (string, error) {
	site = strings.TrimSpace(site)
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	u, err := url.Parse(site)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("gitlab: invalid site")
	}
	path := strings.TrimRight(u.Path, "/")
	if i := strings.Index(path, "/api/v4"); i >= 0 {
		path = path[:i]
	}
	if u.Hostname() == "gitlab.com" {
		path = ""
	}
	return u.Scheme + "://" + u.Host + path, nil
}

// statusError is a non-200 answer. It holds no body, which could echo a secret.
type statusError struct{ code int }

func (e statusError) Error() string { return "gitlab: status " + strconv.Itoa(e.code) }

// get reads one path into out and returns the next page's number, "" on the last.
func (c *Client) get(ctx context.Context, site, token, path string, q url.Values, out any) (string, error) {
	target := site + "/api/v4" + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", errors.New("gitlab: bad request")
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("%w (status %d)", ErrRefused, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", statusError{resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return "", fmt.Errorf("%w: unreadable answer", ErrNotGitLab)
	}
	return resp.Header.Get("X-Next-Page"), nil
}

type user struct {
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

// botName matches the names bots go by: GitLab's token bots, like project_5_bot,
// and the usual ones, like renovate-bot. The user objects on notes and reviewers
// carry no bot flag, so a name is often all there is to go on.
var botName = regexp.MustCompile(`(?i)(^|[-_.])bot($|[-_.\d])|^(dependabot|renovate)`)

// countsFrom reports whether the user is another person, not a bot or the viewer.
func (u *user) countsFrom(viewer string) bool {
	return u != nil && u.Username != "" && u.Username != viewer && !u.Bot && !botName.MatchString(u.Username)
}

// Me checks the token and returns its owner's username.
func (c *Client) Me(ctx context.Context, site, token string) (string, error) {
	var me user
	if _, err := c.get(ctx, site, token, "/user", nil, &me); err != nil {
		var status statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return "", fmt.Errorf("%w: %v", ErrNotGitLab, err)
		}
		return "", err
	}
	if me.Username == "" {
		return "", ErrNotGitLab
	}
	if me.Bot {
		return "", ErrBotToken
	}
	return me.Username, nil
}

// mergeRequest is one MR from the list endpoint.
type mergeRequest struct {
	IID        int    `json:"iid"`
	ProjectID  int    `json:"project_id"`
	Title      string `json:"title"`
	WebURL     string `json:"web_url"`
	UpdatedAt  string `json:"updated_at"`
	State      string `json:"state"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
}

func (m mergeRequest) path(tail string) string {
	return "/projects/" + strconv.Itoa(m.ProjectID) + "/merge_requests/" + strconv.Itoa(m.IID) + tail
}

// repo is the project's path, from a reference like group/project!12.
func (m mergeRequest) repo() string {
	repo, _, _ := strings.Cut(m.References.Full, "!")
	return repo
}

func (m mergeRequest) updated() time.Time {
	t, _ := time.Parse(time.RFC3339, m.UpdatedAt)
	return t.UTC()
}

// list reads every page of a merge request search. A later page failing fails it:
// a partial list would make what's missing look closed.
func (c *Client) list(ctx context.Context, site, token string, q url.Values) ([]mergeRequest, error) {
	q.Set("per_page", "100")
	var all []mergeRequest
	for {
		var page []mergeRequest
		next, err := c.get(ctx, site, token, "/merge_requests", q, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next == "" {
			return all, nil
		}
		q.Set("page", next)
	}
}

type reviewerState struct {
	User      *user  `json:"user"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

func (c *Client) reviewers(ctx context.Context, site, token string, m mergeRequest) ([]reviewerState, error) {
	var out []reviewerState
	_, err := c.get(ctx, site, token, m.path("/reviewers"), nil, &out)
	return out, err
}

type mrNote struct {
	Author    *user  `json:"author"`
	System    bool   `json:"system"`
	CreatedAt string `json:"created_at"`
	Body      string `json:"body"`
}

func (c *Client) notes(ctx context.Context, site, token string, m mergeRequest) ([]mrNote, error) {
	var out []mrNote
	q := url.Values{"per_page": {"100"}, "sort": {"desc"}, "order_by": {"created_at"}}
	_, err := c.get(ctx, site, token, m.path("/notes"), q, &out)
	return out, err
}

type approval struct {
	User       *user  `json:"user"`
	ApprovedAt string `json:"approved_at"`
}

// approvals lists who approved the MR. A server that doesn't offer it just has
// none: reviewer states and notes still tell the story.
func (c *Client) approvals(ctx context.Context, site, token string, m mergeRequest) ([]approval, error) {
	var out struct {
		ApprovedBy []approval `json:"approved_by"`
	}
	_, err := c.get(ctx, site, token, m.path("/approvals"), nil, &out)
	var status statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return nil, nil
	}
	return out.ApprovedBy, err
}

// reviewNote matches the system notes a review leaves. Other system notes, like a
// changed title, aren't a review, so they must not bring back a PR marked done.
var reviewNote = regexp.MustCompile(`(?i)\b(approved this|requested changes|left review|reviewed)\b`)

// doneStates are the reviewer states of a finished review, as an own PR's state.
var doneStates = map[string]string{"approved": prs.Approved, "requested_changes": prs.ChangesRequested, "reviewed": prs.Commented}

// reviewsOf reads the latest word of each person on an MR. A reviewer's state
// says what they did and a note when, falling back on the MR's own time. GitLab
// needs no submitted review to comment, so any comment counts as Commented.
func (c *Client) reviewsOf(ctx context.Context, site, token, viewer string, m mergeRequest) ([]prs.Review, error) {
	notes, err := c.notes(ctx, site, token, m)
	if err != nil {
		return nil, err
	}
	reviewers, err := c.reviewers(ctx, site, token, m)
	if err != nil {
		return nil, err
	}
	approvals, err := c.approvals(ctx, site, token, m)
	if err != nil {
		return nil, err
	}
	by := map[string]*prs.Review{}
	person := func(who string) *prs.Review {
		if by[who] == nil {
			by[who] = &prs.Review{Reviewer: who}
		}
		return by[who]
	}
	seen := func(r *prs.Review, at string) {
		if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(r.At) {
			r.At = t.UTC()
		}
	}
	for _, n := range notes {
		if !n.Author.countsFrom(viewer) || (n.System && !reviewNote.MatchString(n.Body)) {
			continue
		}
		r := person(n.Author.Username)
		seen(r, n.CreatedAt)
		if !n.System {
			r.State = prs.Commented
		}
	}
	for _, a := range approvals {
		if a.User.countsFrom(viewer) {
			r := person(a.User.Username)
			seen(r, a.ApprovedAt)
			r.State = prs.Approved
		}
	}
	// Last, so what a reviewer's state says wins over an earlier approval.
	for _, rv := range reviewers {
		if state, ok := doneStates[rv.State]; ok && rv.User.countsFrom(viewer) {
			person(rv.User.Username).State = state
		}
	}
	out := make([]prs.Review, 0, len(by))
	for _, r := range by {
		if r.State == "" {
			continue // only system notes, with no state behind them
		}
		if r.At.IsZero() {
			r.At = m.updated()
		}
		out = append(out, *r)
	}
	return out, nil
}

// OwnOpenPRs returns your open MRs with their review state. Any failed read fails
// it, since a partial list would make the MRs missing from it look closed, and
// closed ones count as done.
func (c *Client) OwnOpenPRs(ctx context.Context, site, token string) ([]prs.Item, error) {
	me, err := c.Me(ctx, site, token)
	if err != nil {
		return nil, err
	}
	mrs, err := c.list(ctx, site, token, url.Values{"scope": {"created_by_me"}, "state": {"opened"}})
	if err != nil {
		return nil, err
	}
	items := make([]prs.Item, 0, len(mrs))
	for _, m := range mrs {
		reviews, err := c.reviewsOf(ctx, site, token, me, m)
		if err != nil {
			return nil, err
		}
		it := prs.Item{Kind: "own_pr", ExternalID: store.OwnPRPrefix + m.WebURL, Title: m.Title, URL: m.WebURL, Repo: m.repo(), UpdatedAt: m.updated()}
		it.ApplyReviews(reviews)
		items = append(items, it)
	}
	return items, nil
}

// ReviewRequests returns the open MRs waiting on your review. GitLab keeps you as
// a reviewer after you review, so those are left out; a new request resets your
// state, which brings the MR back.
func (c *Client) ReviewRequests(ctx context.Context, site, token string) ([]prs.Item, error) {
	me, err := c.Me(ctx, site, token)
	if err != nil {
		return nil, err
	}
	mrs, err := c.list(ctx, site, token, url.Values{"scope": {"reviews_for_me"}, "state": {"opened"}})
	if err != nil {
		return nil, err
	}
	items := make([]prs.Item, 0, len(mrs))
	for _, m := range mrs {
		reviewers, err := c.reviewers(ctx, site, token, m)
		if err != nil {
			return nil, err
		}
		var mine *reviewerState
		for i := range reviewers {
			if u := reviewers[i].User; u != nil && u.Username == me {
				mine = &reviewers[i]
			}
		}
		if mine == nil || doneStates[mine.State] != "" {
			continue
		}
		it := prs.Item{Kind: "review_request", ExternalID: m.WebURL, Title: m.Title, URL: m.WebURL, Repo: m.repo(), ReviewRequestedOfMe: true, UpdatedAt: m.updated()}
		if t, err := time.Parse(time.RFC3339, mine.CreatedAt); err == nil {
			it.RequestedAt = t.UTC()
		}
		it.Additions, it.Deletions, it.ChangedFiles = c.size(ctx, site, token, m)
		items = append(items, it)
	}
	return items, nil
}

// size counts an MR's changed lines from its diffs, leaving out generated files.
// A size is only nice to have, so a failed read just leaves it unknown.
func (c *Client) size(ctx context.Context, site, token string, m mergeRequest) (additions, deletions, files int) {
	var diffs []struct {
		NewPath   string `json:"new_path"`
		Diff      string `json:"diff"`
		Generated bool   `json:"generated_file"`
	}
	if _, err := c.get(ctx, site, token, m.path("/diffs"), url.Values{"per_page": {"100"}}, &diffs); err != nil {
		return 0, 0, 0
	}
	for _, d := range diffs {
		if d.Generated || prs.IsGenerated(d.NewPath) {
			continue
		}
		add, del := countLines(d.Diff)
		additions += add
		deletions += del
	}
	return additions, deletions, len(diffs)
}

// countLines counts added and removed lines, from the first hunk on, so a file's
// +++ and --- headers don't count.
func countLines(diff string) (additions, deletions int) {
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case inHunk && strings.HasPrefix(line, "+"):
			additions++
		case inHunk && strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return additions, deletions
}

// ReviewsSubmitted returns the MRs you finished reviewing since from, each at your
// latest word on it, whatever state the MR is in now.
func (c *Client) ReviewsSubmitted(ctx context.Context, site, token string, from time.Time) ([]prs.Item, error) {
	me, err := c.Me(ctx, site, token)
	if err != nil {
		return nil, err
	}
	mrs, err := c.list(ctx, site, token, url.Values{"scope": {"reviews_for_me"}, "state": {"all"}, "updated_after": {from.UTC().Format(time.RFC3339)}})
	if err != nil {
		return nil, err
	}
	items := []prs.Item{}
	for _, m := range mrs {
		reviewers, err := c.reviewers(ctx, site, token, m)
		if err != nil {
			return nil, err
		}
		reviewed := false
		for _, rv := range reviewers {
			reviewed = reviewed || (rv.User != nil && rv.User.Username == me && doneStates[rv.State] != "")
		}
		if !reviewed {
			continue
		}
		at, err := c.yourLatest(ctx, site, token, me, m)
		if err != nil {
			return nil, err
		}
		if at.Before(from) {
			continue
		}
		items = append(items, prs.Item{Kind: "review_done", ExternalID: prs.ReviewDonePrefix + m.WebURL, Title: m.Title, URL: m.WebURL, Repo: m.repo(), UpdatedAt: at})
	}
	return items, nil
}

// yourLatest is when you last spoke on an MR, or its own time when no note says.
func (c *Client) yourLatest(ctx context.Context, site, token, me string, m mergeRequest) (time.Time, error) {
	notes, err := c.notes(ctx, site, token, m)
	if err != nil {
		return time.Time{}, err
	}
	approvals, err := c.approvals(ctx, site, token, m)
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	see := func(at string) {
		if t, err := time.Parse(time.RFC3339, at); err == nil && t.After(latest) {
			latest = t.UTC()
		}
	}
	for _, n := range notes {
		if n.Author != nil && n.Author.Username == me && (!n.System || reviewNote.MatchString(n.Body)) {
			see(n.CreatedAt)
		}
	}
	for _, a := range approvals {
		if a.User != nil && a.User.Username == me {
			see(a.ApprovedAt)
		}
	}
	if latest.IsZero() {
		return m.updated(), nil
	}
	return latest, nil
}
