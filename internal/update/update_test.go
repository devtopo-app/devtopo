package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.4", "v1.2.3", true},
		{"1.3.0", "v1.2.9", true},
		{"v2.0.0", "1.99.99", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.2", "v1.2.3", false},
		{"v1.2.3", "v1.2.3-rc.1", true}, // a release is above its prerelease
		{"v1.2.3-rc.1", "v1.2.3", false},
		{"v1.2.3-rc.2", "v1.2.3-rc.1", true},
		{"v1.2.3-rc.10", "v1.2.3-rc.9", true}, // numeric identifiers compare as numbers
		{"v1.2.3-beta", "v1.2.3-alpha", true},
		{"v1.2.3+build.5", "v1.2.3", false}, // build metadata is ignored
		{"v1.10.0", "v1.9.0", true},
		{"v1.2.4", "dev", false},
		{"v1.2.4", "", false},
		{"nightly", "v1.0.0", false},
		{"v1.2", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestValidVersion(t *testing.T) {
	for v, want := range map[string]bool{"v0.1.0": true, "1.0.0-rc.1": true, "dev": false, "": false, "v1": false} {
		if got := Valid(v); got != want {
			t.Errorf("Valid(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestDetectInstall(t *testing.T) {
	cases := []struct {
		path, method, hint string
	}{
		{"/opt/homebrew/Cellar/pulsetool/0.3.0/bin/pulsetool", "homebrew", "brew upgrade pulsetool"},
		{"/usr/local/Cellar/pulsetool/0.3.0/bin/pulsetool", "homebrew", "brew upgrade pulsetool"},
		{"/home/linuxbrew/.linuxbrew/Cellar/other-name/1.0/bin/x", "homebrew", "brew upgrade other-name"},
		{"/Users/me/brew/Cellar/pulsetool/0.3.0/bin/pulsetool", "homebrew", "brew upgrade pulsetool"},
		{"/opt/homebrew/Caskroom/pulsetool/0.3.0/PulseTool.app/Contents/MacOS/pulsetool", "homebrew", "brew upgrade --cask pulsetool"},
		{"/opt/homebrew/bin/pulsetool", "homebrew", "brew upgrade pulsetool"},
		{`C:\Users\me\scoop\apps\pulsetool\0.3.0\pulsetool.exe`, "scoop", "scoop update pulsetool"},
		{`C:\Users\me\scoop\apps\pulsetool\current\pulsetool.exe`, "scoop", "scoop update pulsetool"},
		{`D:\Scoop\Apps\pulsetool\current\pulsetool.exe`, "scoop", "scoop update pulsetool"},
		{`C:\Program Files\PulseTool\pulsetool.exe`, "direct", DirectHint},
		{"/usr/local/bin/pulsetool", "direct", DirectHint},
		{"", "direct", DirectHint},
	}
	for _, c := range cases {
		in := DetectInstall(c.path)
		if in.Method != c.method || in.Hint != c.hint {
			t.Errorf("DetectInstall(%q) = %+v, want %s / %q", c.path, in, c.method, c.hint)
		}
	}
}

const releaseJSON = `{"tag_name":"v1.4.0","html_url":"https://github.com/example/tool/releases/tag/v1.4.0",
"published_at":"2026-10-20T10:00:00Z","body":"Fixes and things","draft":false,"prerelease":false}`

func TestFetchLatest(t *testing.T) {
	var gotPath, gotAccept, gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAccept, gotUA = r.URL.Path, r.Header.Get("Accept"), r.Header.Get("User-Agent")
		w.Write([]byte(releaseJSON))
	}))
	defer ts.Close()

	g := &GitHub{HTTP: ts.Client(), BaseURL: ts.URL, UserAgent: "tool/v1.0.0"}
	rel, err := g.Latest(context.Background(), "example/tool")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/example/tool/releases/latest" {
		t.Errorf("path %q", gotPath)
	}
	if gotAccept != "application/vnd.github+json" || gotUA != "tool/v1.0.0" {
		t.Errorf("headers: accept %q ua %q", gotAccept, gotUA)
	}
	if rel.Tag != "v1.4.0" || rel.URL != "https://github.com/example/tool/releases/tag/v1.4.0" || rel.Notes != "Fixes and things" {
		t.Errorf("release %+v", rel)
	}
	if !rel.PublishedAt.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("published %v", rel.PublishedAt)
	}
}

func TestFetchLatestTrimsNotes(t *testing.T) {
	long := strings.Repeat("a", 5000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.0.0","body":"` + long + `"}`))
	}))
	defer ts.Close()
	rel, err := (&GitHub{HTTP: ts.Client(), BaseURL: ts.URL}).Latest(context.Background(), "example/tool")
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(rel.Notes)); n > MaxNotes+1 {
		t.Errorf("notes not trimmed: %d runes", n)
	}
}

func TestFetchLatestErrorsAreShort(t *testing.T) {
	for code, want := range map[int]string{404: "no published release", 403: "rate limit", 500: "500"} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, strings.Repeat("upstream detail ", 50), code)
		}))
		_, err := (&GitHub{HTTP: ts.Client(), BaseURL: ts.URL}).Latest(context.Background(), "example/tool")
		ts.Close()
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "upstream detail") {
			t.Errorf("%d: error %v, want it to say %q and not echo the body", code, err, want)
		}
	}
}

func TestFetchLatestRejectsBadRepo(t *testing.T) {
	g := &GitHub{HTTP: http.DefaultClient, BaseURL: "http://127.0.0.1:1"}
	for _, repo := range []string{"", "noslash", "a/b/c", "../x/y", "a b/c"} {
		if _, err := g.Latest(context.Background(), repo); err == nil {
			t.Errorf("repo %q accepted", repo)
		}
	}
}

func fixed(rel Release, err error, calls *int32) func(context.Context, string) (Release, error) {
	return func(context.Context, string) (Release, error) {
		atomic.AddInt32(calls, 1)
		return rel, err
	}
}

func TestCheckerFindsUpdate(t *testing.T) {
	var calls int32
	c := NewChecker("v1.0.0", "example/tool", func() bool { return true })
	c.Fetch = fixed(Release{Tag: "v1.4.0", URL: "https://example.invalid/r", Notes: "n"}, nil, &calls)
	c.Install = Install{Method: "scoop", Name: "tool", Hint: "scoop update tool"}
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	c.Now = func() time.Time { return now }

	st := c.CheckNow(context.Background())
	if !st.Supported || !st.Enabled || !st.Available || st.Latest != "v1.4.0" || st.Current != "v1.0.0" {
		t.Fatalf("status %+v", st)
	}
	if st.URL != "https://example.invalid/r" || st.Install != "scoop" || st.Hint != "scoop update tool" || st.Error != "" {
		t.Fatalf("status %+v", st)
	}
	if st.CheckedAt == nil || !st.CheckedAt.Equal(now) {
		t.Fatalf("checked at %v", st.CheckedAt)
	}
	if got := c.Status(); !got.Available || got.Latest != "v1.4.0" {
		t.Fatalf("cached status %+v", got)
	}
}

func TestCheckerUpToDate(t *testing.T) {
	var calls int32
	c := NewChecker("v1.4.0", "example/tool", func() bool { return true })
	c.Fetch = fixed(Release{Tag: "v1.4.0"}, nil, &calls)
	if st := c.CheckNow(context.Background()); st.Available || st.Latest != "v1.4.0" {
		t.Fatalf("status %+v", st)
	}
}

func TestCheckerKeepsLastGoodOnError(t *testing.T) {
	var calls int32
	c := NewChecker("v1.0.0", "example/tool", func() bool { return true })
	c.Fetch = fixed(Release{Tag: "v1.1.0"}, nil, &calls)
	c.CheckNow(context.Background())
	c.Fetch = fixed(Release{}, errors.New("could not reach GitHub"), &calls)
	st := c.CheckNow(context.Background())
	if st.Error != "could not reach GitHub" || !st.Available || st.Latest != "v1.1.0" {
		t.Fatalf("status %+v", st)
	}
}

func TestCheckerUnsupported(t *testing.T) {
	for _, c := range []*Checker{
		NewChecker("dev", "example/tool", func() bool { return true }),
		NewChecker("v1.0.0", "", func() bool { return true }),
		NewChecker("garbage", "example/tool", func() bool { return true }),
	} {
		var calls int32
		c.Fetch = fixed(Release{Tag: "v9.0.0"}, nil, &calls)
		st := c.CheckNow(context.Background())
		if st.Supported || st.Available || calls != 0 {
			t.Errorf("unsupported checker fetched or reported: %+v calls=%d", st, calls)
		}
		// It waits for ctx rather than returning at once: a daemon worker that
		// returns stops the daemon.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		err := c.RunLoop(ctx, time.Millisecond)
		cancel()
		if err != nil {
			t.Errorf("RunLoop on unsupported: %v", err)
		}
		if calls != 0 {
			t.Errorf("unsupported RunLoop fetched")
		}
	}
}

func TestRunLoopChecksAndRespectsEnabled(t *testing.T) {
	var calls int32
	var on atomic.Bool
	on.Store(false)
	c := NewChecker("v1.0.0", "example/tool", on.Load)
	c.Fetch = fixed(Release{Tag: "v1.1.0"}, nil, &calls)
	c.StartDelay = 0

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.RunLoop(ctx, 5*time.Millisecond) }()

	time.Sleep(40 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("disabled loop fetched %d times", n)
	}
	on.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("RunLoop returned %v", err)
	}
	if atomic.LoadInt32(&calls) == 0 || !c.Status().Available {
		t.Fatalf("enabled loop did not check: %+v", c.Status())
	}
}

func TestEnabledSetting(t *testing.T) {
	get := func(v string, ok bool) func(string) (string, bool, error) {
		return func(k string) (string, bool, error) {
			if k != SettingKey {
				t.Fatalf("read %q", k)
			}
			return v, ok, nil
		}
	}
	if !EnabledIn(get("", false)) {
		t.Error("unset must default to on")
	}
	if EnabledIn(get("off", true)) {
		t.Error("off must disable")
	}
	if !EnabledIn(get("on", true)) {
		t.Error("on must enable")
	}
	if !EnabledIn(func(string) (string, bool, error) { return "", false, errors.New("x") }) {
		t.Error("a read error keeps the default")
	}
}
