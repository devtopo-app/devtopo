package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// MaxNotes is how many characters of the release notes are kept.
const MaxNotes = 2000

// Release is the part of a GitHub release DevTopo shows.
type Release struct {
	Tag         string
	URL         string
	Notes       string
	PublishedAt time.Time
}

// GitHub reads a repository's latest release from the public REST API,
// unauthenticated.
type GitHub struct {
	HTTP      *http.Client
	BaseURL   string // https://api.github.com; a test server in tests
	UserAgent string
}

// NewGitHub is the client for api.github.com with a short timeout.
func NewGitHub(userAgent string) *GitHub {
	return &GitHub{HTTP: &http.Client{Timeout: 15 * time.Second}, BaseURL: "https://api.github.com", UserAgent: userAgent}
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)

// Latest is the newest published release of repo (owner/name). GitHub's
// /releases/latest leaves out drafts and prereleases. Errors are short and
// never carry the response body.
func (g *GitHub) Latest(ctx context.Context, repo string) (Release, error) {
	if !repoRe.MatchString(repo) || strings.Contains(repo, "..") {
		return Release{}, errors.New("no valid repository to check")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.BaseURL+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	ua := g.UserAgent
	if ua == "" {
		ua = "devtopo-update-check"
	}
	req.Header.Set("User-Agent", ua)
	hc := g.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Release{}, errors.New("could not reach GitHub")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, errors.New("no published release yet")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return Release{}, errors.New("GitHub rate limit reached; try again later")
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	var body struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Body        string    `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, errors.New("could not read GitHub's answer")
	}
	if body.TagName == "" {
		return Release{}, errors.New("the latest release has no tag")
	}
	return Release{Tag: body.TagName, URL: body.HTMLURL, Notes: trimNotes(body.Body), PublishedAt: body.PublishedAt}, nil
}

func trimNotes(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > MaxNotes {
		return strings.TrimSpace(string(r[:MaxNotes])) + "…"
	}
	return s
}
