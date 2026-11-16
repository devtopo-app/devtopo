package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
)

const token = "glpat-secret"

// fake answers GitLab's REST paths with canned bodies, by path and query.
type fake struct {
	t      *testing.T
	bodies map[string]string
	status map[string]int
	next   map[string]string // the X-Next-Page a path answers with
	seen   []string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("PRIVATE-TOKEN") != token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	key := r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	f.seen = append(f.seen, key)
	if code, ok := f.status[r.URL.Path]; ok {
		w.WriteHeader(code)
		return
	}
	for _, k := range []string{key, r.URL.Path} {
		if body, ok := f.bodies[k]; ok {
			if n, ok := f.next[k]; ok {
				w.Header().Set("X-Next-Page", n)
			}
			fmt.Fprint(w, body)
			return
		}
	}
	fmt.Fprint(w, "[]")
}

func serve(t *testing.T, f *fake) (*Client, string) {
	t.Helper()
	f.t = t
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return NewClient(), s.URL
}

const me = `{"id":7,"username":"me","name":"Me Myself","bot":false}`

func mr(iid int, title string) string {
	return fmt.Sprintf(`{"id":%d,"iid":%d,"project_id":5,"title":%q,"web_url":"https://gl/g/p/-/merge_requests/%d","updated_at":"2026-10-28T08:00:00Z","state":"opened","references":{"full":"g/p!%d"},"author":{"username":"someone"}}`, 100+iid, iid, title, iid, iid)
}

func at(hour int) time.Time { return time.Date(2026, 10, 28, hour, 0, 0, 0, time.UTC) }

func note(who string, hour int, system bool, body string) string {
	return fmt.Sprintf(`{"author":{"username":%q,"bot":%v},"system":%v,"created_at":"2026-10-28T%02d:00:00.000Z","body":%q}`, who, strings.HasSuffix(who, "_bot"), system, hour, body)
}

func reviewer(who, state string, hour int) string {
	return fmt.Sprintf(`{"user":{"username":%q},"state":%q,"created_at":"2026-10-28T%02d:00:00.000Z"}`, who, state, hour)
}

const mrPath = "/api/v4/projects/5/merge_requests/1"

func ownFrom(t *testing.T, notes, reviewers, approvals string) prs.Item {
	t.Helper()
	f := &fake{bodies: map[string]string{
		"/api/v4/user": me,
		"/api/v4/merge_requests?per_page=100&scope=created_by_me&state=opened": "[" + mr(1, "Mine") + "]",
		mrPath + "/notes":     "[" + notes + "]",
		mrPath + "/reviewers": "[" + reviewers + "]",
		mrPath + "/approvals": `{"approved_by":[` + approvals + `]}`,
	}}
	c, site := serve(t, f)
	items, err := c.OwnOpenPRs(context.Background(), site, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	return items[0]
}

func TestOwnOpenPRsMapsAPlainMR(t *testing.T) {
	it := ownFrom(t, "", "", "")
	url := "https://gl/g/p/-/merge_requests/1"
	if it.Kind != "own_pr" || it.ExternalID != "own:"+url || it.URL != url || it.Title != "Mine" || it.Repo != "g/p" {
		t.Fatalf("fields: %+v", it)
	}
	if it.State != "" || it.Reviewer != "" || !it.UpdatedAt.Equal(at(8)) {
		t.Fatalf("no review: %+v", it)
	}
}

func TestOwnOpenPRsReadsReviewerStates(t *testing.T) {
	for state, want := range map[string]string{"approved": prs.Approved, "requested_changes": prs.ChangesRequested, "reviewed": prs.Commented} {
		it := ownFrom(t, note("alex", 10, false, "see this"), reviewer("alex", state, 9), "")
		if it.State != want || it.Reviewer != "alex" || !it.ReviewedAt.Equal(at(10)) {
			t.Errorf("%s: %+v", state, it)
		}
	}
}

func TestOwnOpenPRsCountsNoUnfinishedReview(t *testing.T) {
	for _, state := range []string{"unreviewed", "unapproved", "review_started"} {
		if it := ownFrom(t, "", reviewer("alex", state, 9), ""); it.State != "" {
			t.Errorf("%s: %+v", state, it)
		}
	}
}

// GitLab comments need no submitted review, so a comment is the best stand-in.
func TestOwnOpenPRsCountsAComment(t *testing.T) {
	it := ownFrom(t, note("sam", 11, false, "why?"), reviewer("sam", "unreviewed", 9), "")
	if it.State != prs.Commented || it.Reviewer != "sam" || !it.ReviewedAt.Equal(at(11)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsCountsAnApprovalFromAnyone(t *testing.T) {
	it := ownFrom(t, "", "", `{"user":{"username":"kim"},"approved_at":"2026-10-28T12:00:00.000Z"}`)
	if it.State != prs.Approved || it.Reviewer != "kim" || !it.ReviewedAt.Equal(at(12)) || !it.UpdatedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsLetsAChangeRequestOutrankItsOwnApproval(t *testing.T) {
	it := ownFrom(t, "", reviewer("kim", "requested_changes", 9), `{"user":{"username":"kim"},"approved_at":"2026-10-28T08:00:00.000Z"}`)
	if it.State != prs.ChangesRequested {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsIgnoresWhoShouldNotCount(t *testing.T) {
	for name, n := range map[string]string{
		"you":              note("me", 10, false, "self"),
		"a bot":            note("project_5_bot", 10, false, "lint"),
		"a system note":    note("alex", 10, true, "changed the title"),
		"a deleted author": `{"author":null,"system":false,"created_at":"2026-10-28T10:00:00.000Z","body":"x"}`,
	} {
		if it := ownFrom(t, n, "", ""); it.State != "" || !it.UpdatedAt.Equal(at(8)) {
			t.Errorf("%s must not count: %+v", name, it)
		}
	}
}

// An unrelated system note must not bring back a PR marked done.
func TestOwnOpenPRsTimesAReviewByItsReviewNote(t *testing.T) {
	notes := note("alex", 15, true, "changed the title") + "," + note("alex", 11, true, "approved this merge request")
	it := ownFrom(t, notes, reviewer("alex", "approved", 9), "")
	if !it.ReviewedAt.Equal(at(11)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsUsesTheMRTimeWithoutANote(t *testing.T) {
	it := ownFrom(t, "", reviewer("alex", "reviewed", 9), "")
	if it.State != prs.Commented || !it.ReviewedAt.Equal(at(8)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsReadsEveryPage(t *testing.T) {
	f := &fake{bodies: map[string]string{
		"/api/v4/user": me,
		"/api/v4/merge_requests?per_page=100&scope=created_by_me&state=opened":        "[" + mr(1, "One") + "]",
		"/api/v4/merge_requests?page=2&per_page=100&scope=created_by_me&state=opened": "[" + mr(2, "Two") + "]",
		mrPath + "/approvals":                           `{"approved_by":[]}`,
		"/api/v4/projects/5/merge_requests/2/approvals": `{"approved_by":[]}`,
	}, next: map[string]string{"/api/v4/merge_requests?per_page=100&scope=created_by_me&state=opened": "2"}}
	c, site := serve(t, f)
	items, err := c.OwnOpenPRs(context.Background(), site, token)
	if err != nil || len(items) != 2 {
		t.Fatalf("items %+v err %v", items, err)
	}
}

// A partial list would make the PRs missing from it look closed.
func TestOwnOpenPRsFailsWhenAReadFails(t *testing.T) {
	for _, path := range []string{mrPath + "/notes", mrPath + "/reviewers", mrPath + "/approvals", "/api/v4/merge_requests"} {
		f := &fake{bodies: map[string]string{
			"/api/v4/user": me,
			"/api/v4/merge_requests?per_page=100&scope=created_by_me&state=opened": "[" + mr(1, "Mine") + "]",
		}, status: map[string]int{path: 500}}
		c, site := serve(t, f)
		if items, err := c.OwnOpenPRs(context.Background(), site, token); err == nil {
			t.Errorf("%s: want the error, got %+v", path, items)
		}
	}
}

func requestsFrom(t *testing.T, reviewers, diffs string) []prs.Item {
	t.Helper()
	f := &fake{bodies: map[string]string{
		"/api/v4/user": me,
		"/api/v4/merge_requests?per_page=100&scope=reviews_for_me&state=opened": "[" + mr(1, "Yours") + "]",
		mrPath + "/reviewers":          "[" + reviewers + "]",
		mrPath + "/diffs?per_page=100": "[" + diffs + "]",
	}}
	c, site := serve(t, f)
	items, err := c.ReviewRequests(context.Background(), site, token)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestReviewRequestsListsWhatWaitsOnYou(t *testing.T) {
	for _, state := range []string{"unreviewed", "unapproved", "review_started"} {
		items := requestsFrom(t, reviewer("me", state, 9), "")
		if len(items) != 1 {
			t.Fatalf("%s: %+v", state, items)
		}
		it := items[0]
		if it.Kind != "review_request" || !it.ReviewRequestedOfMe || it.ExternalID != it.URL || it.Repo != "g/p" || !it.RequestedAt.Equal(at(9)) {
			t.Errorf("%s: %+v", state, it)
		}
	}
}

// GitLab keeps you as a reviewer after you review, unlike a GitHub request.
func TestReviewRequestsLeavesOutWhatYouReviewed(t *testing.T) {
	for _, state := range []string{"reviewed", "approved", "requested_changes"} {
		if items := requestsFrom(t, reviewer("me", state, 9), ""); len(items) != 0 {
			t.Errorf("%s: %+v", state, items)
		}
	}
}

func TestReviewRequestsReadsSizesWithoutGeneratedFiles(t *testing.T) {
	code := `{"new_path":"main.go","diff":"--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,3 @@\n-old\n+new\n+more\n keep\n"}`
	lock := `{"new_path":"go.sum","diff":"@@ -1 +1 @@\n-a\n+b\n"}`
	marked := `{"new_path":"schema.go","generated_file":true,"diff":"@@ -1 +1 @@\n-a\n+b\n"}`
	it := requestsFrom(t, reviewer("me", "unreviewed", 9), code+","+lock+","+marked)[0]
	if it.Additions != 2 || it.Deletions != 1 || it.ChangedFiles != 3 {
		t.Fatalf("sizes: %+v", it)
	}
}

func TestReviewRequestsKeepsAPRWhoseDiffFails(t *testing.T) {
	f := &fake{bodies: map[string]string{
		"/api/v4/user": me,
		"/api/v4/merge_requests?per_page=100&scope=reviews_for_me&state=opened": "[" + mr(1, "Yours") + "]",
		mrPath + "/reviewers": "[" + reviewer("me", "unreviewed", 9) + "]",
	}, status: map[string]int{mrPath + "/diffs": 500}}
	c, site := serve(t, f)
	items, err := c.ReviewRequests(context.Background(), site, token)
	if err != nil || len(items) != 1 || items[0].ChangedFiles != 0 {
		t.Fatalf("items %+v err %v", items, err)
	}
}

func TestReviewsSubmitted(t *testing.T) {
	from := at(6)
	query := "/api/v4/merge_requests?per_page=100&scope=reviews_for_me&state=all&updated_after=2026-10-28T06%3A00%3A00Z"
	body := func(reviewers, notes string) map[string]string {
		return map[string]string{
			"/api/v4/user":        me,
			query:                 "[" + mr(1, "Theirs") + "]",
			mrPath + "/reviewers": "[" + reviewers + "]",
			mrPath + "/notes":     "[" + notes + "]",
			mrPath + "/approvals": `{"approved_by":[]}`,
		}
	}
	run := func(reviewers, notes string) []prs.Item {
		t.Helper()
		c, site := serve(t, &fake{bodies: body(reviewers, notes)})
		items, err := c.ReviewsSubmitted(context.Background(), site, token, from)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	items := run(reviewer("me", "approved", 5), note("me", 10, true, "approved this merge request"))
	url := "https://gl/g/p/-/merge_requests/1"
	if len(items) != 1 || items[0].Kind != "review_done" || items[0].ExternalID != "review:"+url || items[0].URL != url || items[0].Title != "Theirs" || !items[0].UpdatedAt.Equal(at(10)) {
		t.Fatalf("items: %+v", items)
	}
	if items := run(reviewer("me", "unreviewed", 5), note("me", 10, false, "a question")); len(items) != 0 {
		t.Fatalf("no finished review: %+v", items)
	}
	if items := run(reviewer("me", "approved", 5), note("me", 4, true, "approved this merge request")); len(items) != 0 {
		t.Fatalf("before the window: %+v", items)
	}
}

func TestMe(t *testing.T) {
	c, site := serve(t, &fake{bodies: map[string]string{"/api/v4/user": me}})
	if login, err := c.Me(context.Background(), site, token); err != nil || login != "me" {
		t.Fatalf("login %q err %v", login, err)
	}
	if _, err := c.Me(context.Background(), site, "wrong"); !errors.Is(err, ErrRefused) {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestMeSaysWhenTheSiteIsNotGitLab(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>hello</html>") }))
	defer s.Close()
	if _, err := NewClient().Me(context.Background(), s.URL, token); !errors.Is(err, ErrNotGitLab) {
		t.Fatalf("err: %v", err)
	}
	gone := httptest.NewServer(http.NotFoundHandler())
	defer gone.Close()
	if _, err := NewClient().Me(context.Background(), gone.URL, token); !errors.Is(err, ErrNotGitLab) {
		t.Fatalf("404: %v", err)
	}
}

// The token header is not one Go strips, so a redirect must not carry it away.
func TestNeverFollowsARedirect(t *testing.T) {
	got := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("PRIVATE-TOKEN") != "" }))
	defer other.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer s.Close()
	if _, err := NewClient().Me(context.Background(), s.URL, token); err == nil {
		t.Fatal("want an error")
	}
	if got {
		t.Fatal("the token left for another host")
	}
}

func TestErrorsNeverCarryTheToken(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, "boom "+token)
	}))
	defer s.Close()
	_, err := NewClient().OwnOpenPRs(context.Background(), s.URL, token)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err: %v", err)
	}
}

func TestNormalizeSite(t *testing.T) {
	for in, want := range map[string]string{
		"gitlab.com":                     "https://gitlab.com",
		" https://gitlab.com/ ":          "https://gitlab.com",
		"https://gitlab.com/group/proj":  "https://gitlab.com",
		"gitlab.example.com/gitlab/":     "https://gitlab.example.com/gitlab",
		"http://10.0.0.5:8080":           "http://10.0.0.5:8080",
		"https://git.acme.io/api/v4":     "https://git.acme.io",
		"https://gitlab.com/api/v4/user": "https://gitlab.com",
	} {
		if got, err := NormalizeSite(in); err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "://"} {
		if got, err := NormalizeSite(in); err == nil {
			t.Errorf("%q: want an error, got %q", in, got)
		}
	}
}

// Only a wrong address should say it's the wrong address.
func TestMeKeepsATransientFailureApartFromAWrongAddress(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer s.Close()
	_, err := NewClient().Me(context.Background(), s.URL, token)
	if err == nil || errors.Is(err, ErrNotGitLab) || errors.Is(err, ErrRefused) {
		t.Fatalf("err: %v", err)
	}
}

func TestBotNamesDoNotCount(t *testing.T) {
	for _, name := range []string{"project_5_bot", "project_5_bot_abc", "group_1_bot", "renovate-bot", "gitlab-bot", "dependabot", "renovate", "my.bot", "ci_bot2"} {
		if (&user{Username: name}).countsFrom("me") {
			t.Errorf("%s must not count", name)
		}
	}
	for _, name := range []string{"alex", "talbot", "abbot", "botond", "bottomley"} {
		if !(&user{Username: name}).countsFrom("me") {
			t.Errorf("%s must count", name)
		}
	}
}

// A project or group access token acts as a bot, which has no MRs of its own, so it
// would connect fine and then show nothing.
func TestMeRefusesABotToken(t *testing.T) {
	c, site := serve(t, &fake{bodies: map[string]string{"/api/v4/user": `{"id":9,"username":"project_5_bot_abc","bot":true}`}})
	if _, err := c.Me(context.Background(), site, token); !errors.Is(err, ErrBotToken) {
		t.Fatalf("err: %v", err)
	}
	if _, err := c.OwnOpenPRs(context.Background(), site, token); !errors.Is(err, ErrBotToken) {
		t.Fatalf("a token already connected must fail its sync: %v", err)
	}
}

// Real answers from gitlab.com, on an MR with changes requested. A system note about
// asking for a review isn't a review.
func TestOwnOpenPRsReadsARealChangeRequest(t *testing.T) {
	notes := `{"author":{"username":"reviewer-a"},"system":true,"created_at":"2026-11-05T07:28:22.111Z","body":"requested changes"},` +
		`{"author":{"username":"reviewer-a"},"system":false,"created_at":"2026-11-05T07:28:21.998Z","body":"weird PR"},` +
		`{"author":{"username":"me"},"system":true,"created_at":"2026-11-05T07:26:15.596Z","body":"requested review from @reviewer-a"}`
	it := ownFrom(t, notes, `{"user":{"username":"reviewer-a"},"state":"requested_changes","created_at":"2026-11-05T07:26:15.543Z"}`, "")
	want := time.Date(2026, 11, 5, 7, 28, 22, 111000000, time.UTC)
	if it.State != prs.ChangesRequested || it.Reviewer != "reviewer-a" || !it.ReviewedAt.Equal(want) {
		t.Fatalf("got: %+v", it)
	}
}

func TestAskingForAReviewIsNotAReview(t *testing.T) {
	it := ownFrom(t, note("alex", 10, true, "requested review from @sam"), "", "")
	if it.State != "" || !it.UpdatedAt.Equal(at(8)) {
		t.Fatalf("got: %+v", it)
	}
}
