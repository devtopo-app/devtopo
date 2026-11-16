package azure

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
)

const (
	token = "ado-secret"
	meID  = "11111111-1111-1111-1111-111111111111"
	alex  = "22222222-2222-2222-2222-222222222222"
	sam   = "33333333-3333-3333-3333-333333333333"
)

// fake answers Azure DevOps' REST paths with canned bodies. Keys are a path, or a
// path and the search criteria it was asked with.
type fake struct {
	bodies map[string]string
	status map[string]int
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+token))
	if r.Header.Get("Authorization") != want {
		// As the real service does for a bad token: off to sign in.
		http.Redirect(w, r, "https://login.example/signin", http.StatusFound)
		return
	}
	if code, ok := f.status[r.URL.Path]; ok {
		w.WriteHeader(code)
		return
	}
	q := r.URL.Query()
	key := r.URL.Path
	for _, k := range []string{"searchCriteria.reviewerId", "searchCriteria.creatorId"} {
		if q.Get(k) != "" {
			key += "?" + strings.TrimPrefix(k, "searchCriteria.") + "&status=" + q.Get("searchCriteria.status") + "&skip=" + q.Get("$skip")
		}
	}
	w.Header().Set("Content-Type", "application/json")
	for _, k := range []string{key, r.URL.Path} {
		if body, ok := f.bodies[k]; ok {
			fmt.Fprint(w, body)
			return
		}
	}
	fmt.Fprint(w, `{"value":[],"count":0}`)
}

func serve(t *testing.T, f *fake) (*Client, string) {
	t.Helper()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return NewClient(), s.URL
}

func at(hour int) time.Time { return time.Date(2026, 10, 28, hour, 0, 0, 0, time.UTC) }

func stamp(hour int) string { return at(hour).Format(time.RFC3339) }

const (
	connection = `{"authenticatedUser":{"id":"` + meID + `","providerDisplayName":"Me Myself"}}`
	repos      = `{"value":[{"id":"r1","name":"api","project":{"id":"p1","name":"Shop"}},{"id":"r2","name":"web","project":{"id":"p1","name":"Shop"}}],"count":2}`
	threadPath = "/_apis/git/repositories/r1/pullRequests/9/threads"
	prsPath    = "/Shop/_apis/git/pullrequests"
)

func reviewerOf(id, name string, vote int) string {
	return fmt.Sprintf(`{"id":%q,"displayName":%q,"uniqueName":"%s@x","vote":%d}`, id, name, name, vote)
}

func pr(status string, reviewers ...string) string {
	return fmt.Sprintf(`{"repository":{"id":"r1","name":"api","project":{"id":"p1","name":"Shop"}},"pullRequestId":9,"status":%q,"title":"Fix X","creationDate":%q,"createdBy":{"id":"someone"},"reviewers":[%s]}`, status, stamp(6), strings.Join(reviewers, ","))
}

func list(items ...string) string {
	return `{"value":[` + strings.Join(items, ",") + `],"count":` + fmt.Sprint(len(items)) + `}`
}

func voteThread(id string, vote, hour int) string {
	return fmt.Sprintf(`{"id":1,"publishedDate":%q,"comments":[{"author":{"id":"svc","displayName":"[x]\\Service","isContainer":true},"content":"voted","publishedDate":%q,"commentType":"system"}],"properties":{"CodeReviewThreadType":{"$type":"System.String","$value":"VoteUpdate"},"CodeReviewVotedByTfId":{"$type":"System.String","$value":%q},"CodeReviewVoteResult":{"$type":"System.String","$value":"%d"}},"isDeleted":false}`, stamp(hour), stamp(hour), id, vote)
}

func commentThread(id, name string, hour int) string {
	return fmt.Sprintf(`{"id":2,"publishedDate":%q,"comments":[{"author":{"id":%q,"displayName":%q,"uniqueName":"%s@x"},"content":"why?","publishedDate":%q,"commentType":"text"}],"status":"active","isDeleted":false}`, stamp(hour), id, name, name, stamp(hour))
}

func ownFrom(t *testing.T, reviewers []string, threads ...string) prs.Item {
	t.Helper()
	c, base := serve(t, &fake{bodies: map[string]string{
		"/_apis/connectionData":                     connection,
		"/_apis/git/repositories":                   repos,
		prsPath + "?creatorId&status=active&skip=0": list(pr("active", reviewers...)),
		threadPath: list(threads...),
	}})
	items, err := c.OwnOpenPRs(context.Background(), base, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	return items[0]
}

func TestOwnOpenPRsMapsAPlainPR(t *testing.T) {
	it := ownFrom(t, nil)
	if it.Kind != "own_pr" || !strings.HasPrefix(it.ExternalID, "own:http") || it.Title != "Fix X" || it.Repo != "Shop/api" || !strings.HasSuffix(it.URL, "/Shop/_git/api/pullrequest/9") {
		t.Fatalf("fields: %+v", it)
	}
	if it.ExternalID != "own:"+it.URL || it.State != "" || !it.UpdatedAt.Equal(at(6)) {
		t.Fatalf("no review: %+v", it)
	}
}

func TestOwnOpenPRsMapsVotes(t *testing.T) {
	for vote, want := range map[int]string{10: prs.Approved, 5: prs.Approved, -5: prs.ChangesRequested, -10: prs.ChangesRequested} {
		it := ownFrom(t, []string{reviewerOf(alex, "Alex", vote)}, voteThread(alex, vote, 11))
		if it.State != want || it.Reviewer != "Alex" || !it.ReviewedAt.Equal(at(11)) || !it.UpdatedAt.Equal(at(11)) {
			t.Errorf("vote %d: %+v", vote, it)
		}
	}
}

func TestOwnOpenPRsLeavesOutAVoteOfZero(t *testing.T) {
	if it := ownFrom(t, []string{reviewerOf(alex, "Alex", 0)}, voteThread(alex, 10, 8)); it.State != "" {
		t.Fatalf("a vote taken back must not count: %+v", it)
	}
}

// Azure DevOps has no submitted reviews, so a comment is the best stand-in.
func TestOwnOpenPRsCountsAComment(t *testing.T) {
	it := ownFrom(t, []string{reviewerOf(alex, "Alex", 0)}, commentThread(sam, "Sam", 12))
	if it.State != prs.Commented || it.Reviewer != "Sam" || !it.ReviewedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsAVoteOutranksTheVotersComment(t *testing.T) {
	it := ownFrom(t, []string{reviewerOf(alex, "Alex", 10)}, voteThread(alex, 10, 9), commentThread(alex, "Alex", 12))
	if it.State != prs.Approved || !it.ReviewedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsMostUrgentWins(t *testing.T) {
	it := ownFrom(t, []string{reviewerOf(alex, "Alex", 10), reviewerOf(sam, "Sam", -10)}, voteThread(alex, 10, 12), voteThread(sam, -10, 9))
	if it.State != prs.ChangesRequested || it.Reviewer != "Sam" || !it.ReviewedAt.Equal(at(9)) || !it.UpdatedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsIgnoresWhoShouldNotCount(t *testing.T) {
	deleted := strings.Replace(commentThread(sam, "Sam", 10), `"commentType":"text"`, `"commentType":"text","isDeleted":true`, 1)
	system := strings.Replace(commentThread(sam, "Sam", 10), `"commentType":"text"`, `"commentType":"system"`, 1)
	gone := strings.Replace(commentThread(sam, "Sam", 10), `"isDeleted":false`, `"isDeleted":true`, 1)
	build := commentThread("b", "Shop Build Service (acme)", 10)
	for name, th := range map[string]string{
		"you": commentThread(meID, "Me", 10), "a deleted comment": deleted, "a system comment": system, "a deleted thread": gone, "a build service": build,
	} {
		if it := ownFrom(t, nil, th); it.State != "" {
			t.Errorf("%s must not count: %+v", name, it)
		}
	}
	team := `{"id":"t","displayName":"Team","vote":10,"isContainer":true}`
	if it := ownFrom(t, []string{team}); it.State != "" {
		t.Errorf("a team's rolled-up vote must not count: %+v", it)
	}
	if it := ownFrom(t, []string{reviewerOf(meID, "Me", 10)}); it.State != "" {
		t.Errorf("your own vote must not count: %+v", it)
	}
}

func TestOwnOpenPRsUsesThePRTimeWithoutAVoteThread(t *testing.T) {
	it := ownFrom(t, []string{reviewerOf(alex, "Alex", 10)})
	if it.State != prs.Approved || !it.ReviewedAt.Equal(at(6)) {
		t.Fatalf("got: %+v", it)
	}
}

// A partial list would make the PRs missing from it look closed.
func TestOwnOpenPRsFailsWhenAReadFails(t *testing.T) {
	for _, path := range []string{threadPath, prsPath, "/_apis/git/repositories", "/_apis/connectionData"} {
		c, base := serve(t, &fake{bodies: map[string]string{
			"/_apis/connectionData":                     connection,
			"/_apis/git/repositories":                   repos,
			prsPath + "?creatorId&status=active&skip=0": list(pr("active")),
		}, status: map[string]int{path: 500}})
		if items, err := c.OwnOpenPRs(context.Background(), base, token); err == nil {
			t.Errorf("%s: want the error, got %+v", path, items)
		}
	}
}

func TestOwnOpenPRsReadsEveryPage(t *testing.T) {
	full := make([]string, pageSize)
	for i := range full {
		full[i] = pr("active")
	}
	c, base := serve(t, &fake{bodies: map[string]string{
		"/_apis/connectionData":                                             connection,
		"/_apis/git/repositories":                                           repos,
		prsPath + "?creatorId&status=active&skip=0":                         list(full...),
		prsPath + fmt.Sprintf("?creatorId&status=active&skip=%d", pageSize): list(pr("active")),
	}})
	items, err := c.OwnOpenPRs(context.Background(), base, token)
	if err != nil || len(items) != pageSize+1 {
		t.Fatalf("got %d items, err %v", len(items), err)
	}
}

func requestsFrom(t *testing.T, prItems ...string) []prs.Item {
	t.Helper()
	c, base := serve(t, &fake{bodies: map[string]string{
		"/_apis/connectionData":                      connection,
		"/_apis/git/repositories":                    repos,
		prsPath + "?reviewerId&status=active&skip=0": list(prItems...),
	}})
	items, err := c.ReviewRequests(context.Background(), base, token)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestReviewRequestsListsWhatWaitsOnYou(t *testing.T) {
	items := requestsFrom(t, pr("active", reviewerOf(meID, "Me", 0)))
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	it := items[0]
	if it.Kind != "review_request" || !it.ReviewRequestedOfMe || it.ExternalID != it.URL || it.Repo != "Shop/api" || it.ChangedFiles != 0 {
		t.Fatalf("fields: %+v", it)
	}
}

func TestReviewRequestsLeavesOutWhatYouAnswered(t *testing.T) {
	declined := `{"id":"` + meID + `","displayName":"Me","vote":0,"hasDeclined":true}`
	for name, r := range map[string]string{"approved": reviewerOf(meID, "Me", 10), "rejected": reviewerOf(meID, "Me", -10), "declined": declined, "someone else only": reviewerOf(alex, "Alex", 0)} {
		if items := requestsFrom(t, pr("active", r)); len(items) != 0 {
			t.Errorf("%s: %+v", name, items)
		}
	}
}

func TestReviewsSubmitted(t *testing.T) {
	run := func(from time.Time, item string, threads ...string) []prs.Item {
		t.Helper()
		c, base := serve(t, &fake{bodies: map[string]string{
			"/_apis/connectionData":                   connection,
			"/_apis/git/repositories":                 repos,
			prsPath + "?reviewerId&status=all&skip=0": list(item),
			threadPath: list(threads...),
		}})
		items, err := c.ReviewsSubmitted(context.Background(), base, token, from)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	items := run(at(7), pr("completed", reviewerOf(meID, "Me", 10)), voteThread(meID, 10, 10))
	if len(items) != 1 || items[0].Kind != "review_done" || items[0].ExternalID != "review:"+items[0].URL || items[0].Title != "Fix X" || !items[0].UpdatedAt.Equal(at(10)) {
		t.Fatalf("items: %+v", items)
	}
	if items := run(at(11), pr("active", reviewerOf(meID, "Me", 10)), voteThread(meID, 10, 10)); len(items) != 0 {
		t.Fatalf("before the window: %+v", items)
	}
	if items := run(at(7), pr("active", reviewerOf(meID, "Me", 0)), commentThread(meID, "Me", 10)); len(items) != 0 {
		t.Fatalf("no vote: %+v", items)
	}
	if items := run(at(7), pr("active", reviewerOf(meID, "Me", 10))); len(items) != 0 {
		t.Fatalf("no vote time in the window: %+v", items)
	}
}

func TestMe(t *testing.T) {
	c, base := serve(t, &fake{bodies: map[string]string{"/_apis/connectionData": connection}})
	if id, name, err := c.Me(context.Background(), base, token); err != nil || id != meID || name != "Me Myself" {
		t.Fatalf("id %q name %q err %v", id, name, err)
	}
	if _, _, err := c.Me(context.Background(), base, "wrong"); !errors.Is(err, ErrRefused) {
		t.Fatalf("wrong token: %v", err)
	}
}

// A bad token can also get a sign-in page with a 203, or the anonymous user.
func TestMeRefusesWhatIsNotASignedInUser(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"sign-in page": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNonAuthoritativeInfo)
			fmt.Fprint(w, "<html>sign in</html>")
		},
		"anonymous": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"authenticatedUser":{"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}}`)
		},
		"unauthorized": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
	} {
		s := httptest.NewServer(h)
		if _, _, err := NewClient().Me(context.Background(), s.URL, token); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
		s.Close()
	}
}

func TestMeSaysWhenTheOrganizationIsNotThere(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	defer gone.Close()
	if _, _, err := NewClient().Me(context.Background(), gone.URL, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err: %v", err)
	}
}

func TestNeverFollowsARedirect(t *testing.T) {
	got := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = true }))
	defer other.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer s.Close()
	if _, _, err := NewClient().Me(context.Background(), s.URL, token); err == nil {
		t.Fatal("want an error")
	}
	if got {
		t.Fatal("followed the redirect")
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

func TestPRURLEscapesNames(t *testing.T) {
	got := prURL("https://dev.azure.com/acme", "My Project", "my repo", 9)
	if got != "https://dev.azure.com/acme/My%20Project/_git/my%20repo/pullrequest/9" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeOrganization(t *testing.T) {
	for in, want := range map[string]string{
		"acme":                        "https://dev.azure.com/acme",
		" acme ":                      "https://dev.azure.com/acme",
		"dev.azure.com/acme":          "https://dev.azure.com/acme",
		"https://dev.azure.com/acme/": "https://dev.azure.com/acme",
		"https://dev.azure.com/acme/Shop/_git/api": "https://dev.azure.com/acme",
		"https://acme.visualstudio.com":            "https://acme.visualstudio.com",
		"https://acme.visualstudio.com/Shop":       "https://acme.visualstudio.com",
		"https://tfs.corp.example/tfs/Main/":       "https://tfs.corp.example/tfs/Main",
	} {
		if got, err := NormalizeOrganization(in); err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "https://dev.azure.com", "https://dev.azure.com/", "a b"} {
		if got, err := NormalizeOrganization(in); err == nil {
			t.Errorf("%q: want an error, got %q", in, got)
		}
	}
}

// connectionData is undocumented, and official clients call it with no version: one
// above what it supports would be refused, which would stop everything.
func TestMeAsksForNoAPIVersion(t *testing.T) {
	var query string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		fmt.Fprint(w, connection)
	}))
	defer s.Close()
	if _, _, err := NewClient().Me(context.Background(), s.URL, token); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(query, "api-version") {
		t.Fatalf("query: %q", query)
	}
}

func TestOtherCallsAskForAnAPIVersion(t *testing.T) {
	var query string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		fmt.Fprint(w, repos)
	}))
	defer s.Close()
	if _, err := NewClient().projects(context.Background(), s.URL, token); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "api-version=7.1") {
		t.Fatalf("query: %q", query)
	}
}

func TestProjectsSkipsDisabledRepositories(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"value":[{"name":"a","isDisabled":true,"project":{"name":"Old"}},{"name":"b","project":{"name":"Live"}}]}`)
	}))
	defer s.Close()
	got, err := NewClient().projects(context.Background(), s.URL, token)
	if err != nil || len(got) != 1 || got[0] != "Live" {
		t.Fatalf("got %v err %v", got, err)
	}
}

// Each fetch of a sync needs who you are and your projects: one client asks once.
func TestAClientAsksWhoAndWhereOnce(t *testing.T) {
	counts := map[string]int{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts[r.URL.Path]++
		switch r.URL.Path {
		case "/_apis/connectionData":
			fmt.Fprint(w, connection)
		case "/_apis/git/repositories":
			fmt.Fprint(w, repos)
		default:
			fmt.Fprint(w, `{"value":[]}`)
		}
	}))
	defer s.Close()
	c := NewClient()
	for range 3 {
		if _, err := c.ReviewRequests(context.Background(), s.URL, token); err != nil {
			t.Fatal(err)
		}
	}
	if counts["/_apis/connectionData"] != 1 || counts["/_apis/git/repositories"] != 1 {
		t.Fatalf("counts: %v", counts)
	}
}

func TestAClientAsksAgainForAnotherToken(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, connection)
	}))
	defer s.Close()
	c := NewClient()
	for _, tk := range []string{"one", "two"} {
		if _, _, err := c.Me(context.Background(), s.URL, tk); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("calls: %d", calls)
	}
}

// A PR closed before the window can't hold a new vote, so it costs no call.
func TestReviewsSubmittedReadsNoThreadsOfPRsClosedBefore(t *testing.T) {
	threads := 0
	closed := strings.Replace(pr("completed", reviewerOf(meID, "Me", 10)), `"status"`, `"closedDate":"`+stamp(5)+`","status"`, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_apis/connectionData":
			fmt.Fprint(w, connection)
		case r.URL.Path == "/_apis/git/repositories":
			fmt.Fprint(w, repos)
		case strings.HasSuffix(r.URL.Path, "/threads"):
			threads++
			fmt.Fprint(w, `{"value":[]}`)
		default:
			fmt.Fprint(w, list(closed))
		}
	}))
	defer s.Close()
	items, err := NewClient().ReviewsSubmitted(context.Background(), s.URL, token, at(7))
	if err != nil || len(items) != 0 || threads != 0 {
		t.Fatalf("items %+v err %v threads %d", items, err, threads)
	}
}

func TestReviewsSubmittedCapsItsThreadReads(t *testing.T) {
	threads := 0
	many := make([]string, doneScan)
	for i := range many {
		many[i] = pr("active", reviewerOf(meID, "Me", 10))
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_apis/connectionData":
			fmt.Fprint(w, connection)
		case r.URL.Path == "/_apis/git/repositories":
			fmt.Fprint(w, `{"value":[{"project":{"name":"A"}},{"project":{"name":"B"}},{"project":{"name":"C"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/threads"):
			threads++
			fmt.Fprint(w, `{"value":[]}`)
		default:
			fmt.Fprint(w, list(many...))
		}
	}))
	defer s.Close()
	if _, err := NewClient().ReviewsSubmitted(context.Background(), s.URL, token, at(7)); err != nil {
		t.Fatal(err)
	}
	if threads != doneThreads {
		t.Fatalf("threads: %d, want %d", threads, doneThreads)
	}
}
