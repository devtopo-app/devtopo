package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/azure"
	"github.com/devtopo-app/devtopo/internal/gitlab"
	"github.com/devtopo-app/devtopo/internal/store"
)

// connector finds one connector in /api/connectors.
func connector(t *testing.T, s *Server, name string) connectorStatus {
	t.Helper()
	var all []connectorStatus
	if err := json.Unmarshal(get(t, s, "/api/connectors").Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s in %+v", name, all)
	return connectorStatus{}
}

func TestConnectGitLab(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	var gotSite, gotToken string
	s.gitlabValidate = func(_ context.Context, site, token string) error {
		gotSite, gotToken = site, token
		return nil
	}
	synced := 0
	s.syncGitLab = func(context.Context) (int, error) {
		synced++
		return 0, nil
	}
	if c := connector(t, s, "GitLab"); c.Status != "not configured" {
		t.Fatalf("before: %+v", c)
	}
	rec := do(t, s, "POST", "/api/connectors/gitlab", `{"site":"gitlab.example.com/","token":"glpat-secret"}`)
	if rec.Code != 200 {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body)
	}
	if gotSite != "https://gitlab.example.com" || gotToken != "glpat-secret" || synced != 1 {
		t.Fatalf("site %q token %q synced %d", gotSite, gotToken, synced)
	}
	c := connector(t, s, "GitLab")
	if c.Status != "connected" || c.Values["site"] != "https://gitlab.example.com" {
		t.Fatalf("after: %+v", c)
	}
	if raw := get(t, s, "/api/connectors").Body.String(); strings.Contains(raw, "glpat-secret") {
		t.Fatalf("the token leaked: %s", raw)
	}
}

func TestConnectGitLabSaysWhatFailed(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	for body, code := range map[string]int{`not json`: 400, `{"site":"","token":"t"}`: 400, `{"site":"gitlab.com","token":"  "}`: 400} {
		if rec := do(t, s, "POST", "/api/connectors/gitlab", body); rec.Code != code {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
	for err, want := range map[error]string{gitlab.ErrNotGitLab: "not a GitLab server", gitlab.ErrRefused: "read_api"} {
		s.gitlabValidate = func(context.Context, string, string) error { return err }
		rec := do(t, s, "POST", "/api/connectors/gitlab", `{"site":"gitlab.com","token":"secret-token"}`)
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), want) || strings.Contains(rec.Body.String(), "secret-token") {
			t.Errorf("%v: %d %s", err, rec.Code, rec.Body)
		}
	}
	if c := connector(t, s, "GitLab"); c.Status != "not configured" {
		t.Fatalf("a failed connect must store nothing: %+v", c)
	}
}

func TestConnectAzure(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	var gotBase string
	s.azureValidate = func(_ context.Context, base, _ string) error {
		gotBase = base
		return nil
	}
	synced := 0
	s.syncAzure = func(context.Context) (int, error) {
		synced++
		return 0, nil
	}
	rec := do(t, s, "POST", "/api/connectors/azure", `{"organization":"acme","token":"ado-secret"}`)
	if rec.Code != 200 {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body)
	}
	if gotBase != "https://dev.azure.com/acme" || synced != 1 {
		t.Fatalf("base %q synced %d", gotBase, synced)
	}
	c := connector(t, s, "Azure DevOps")
	if c.Status != "connected" || c.Values["organization"] != "https://dev.azure.com/acme" {
		t.Fatalf("after: %+v", c)
	}
	if raw := get(t, s, "/api/connectors").Body.String(); strings.Contains(raw, "ado-secret") {
		t.Fatalf("the token leaked: %s", raw)
	}
}

func TestConnectAzureSaysWhatFailed(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	for _, body := range []string{`not json`, `{"organization":"","token":"t"}`, `{"organization":"acme","token":""}`} {
		if rec := do(t, s, "POST", "/api/connectors/azure", body); rec.Code != 400 {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
	for err, want := range map[error]string{azure.ErrNotFound: "No Azure DevOps organization", azure.ErrRefused: "Code (Read)", errors.New("network"): "could not verify"} {
		s.azureValidate = func(context.Context, string, string) error { return err }
		rec := do(t, s, "POST", "/api/connectors/azure", `{"organization":"acme","token":"secret-token"}`)
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), want) || strings.Contains(rec.Body.String(), "secret-token") {
			t.Errorf("%v: %d %s", err, rec.Code, rec.Body)
		}
	}
}

func TestDisconnectACodeHostForgetsItsItemsAndMarks(t *testing.T) {
	for _, c := range []struct{ source, tokenKey string }{{"gitlab", "gitlab_token"}, {"azure", "azure_token"}} {
		s := newSeededServer(t, time.Now(), nil, nil)
		if err := s.store.SetSetting(c.tokenKey, "secret"); err != nil {
			t.Fatal(err)
		}
		own := store.ExternalItem{Source: c.source, Kind: "own_pr", ExternalID: "own:u", URL: "u", UpdatedAt: time.Now()}
		keep := store.ExternalItem{Source: "github", Kind: "own_pr", ExternalID: "own:g", URL: "g", UpdatedAt: time.Now()}
		for _, it := range []store.ExternalItem{own, keep} {
			if err := s.store.UpsertExternalItem(it); err != nil {
				t.Fatal(err)
			}
		}
		for _, u := range []string{"u", "g"} {
			if err := s.store.MarkOwnPRDone(u, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if rec := do(t, s, "DELETE", "/api/connectors/"+c.source, ""); rec.Code != 200 {
			t.Fatalf("%s: %d %s", c.source, rec.Code, rec.Body)
		}
		if v, _, _ := s.store.GetSetting(c.tokenKey); v != "" {
			t.Errorf("%s kept its token", c.source)
		}
		if items, _ := s.store.ListExternalItems(c.source); len(items) != 0 {
			t.Errorf("%s kept items: %+v", c.source, items)
		}
		marks, _ := s.store.OwnPRsDone()
		if _, ok := marks["g"]; !ok || len(marks) != 1 {
			t.Errorf("%s marks: %v", c.source, marks)
		}
	}
}

func TestRefreshSyncsTheCodeHosts(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	s.syncGitHub = func(context.Context) (int, error) { return 0, nil }
	s.syncLinear = func(context.Context) (int, error) { return 0, nil }
	s.syncCalendar = func(context.Context) (int, error) { return 0, nil }
	s.syncJira = func(context.Context) (int, error) { return 0, nil }
	s.syncGitLab = func(context.Context) (int, error) { return 2, nil }
	s.syncAzure = func(context.Context) (int, error) { return 3, nil }
	var out map[string]int
	if err := json.Unmarshal(do(t, s, "POST", "/api/refresh", "").Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["gitlab"] != 2 || out["azure"] != 3 {
		t.Fatalf("out: %v", out)
	}
}

func TestPlannableListsEveryCodeHost(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	for _, source := range []string{"gitlab", "azure"} {
		for _, it := range []store.ExternalItem{
			{Source: source, Kind: "review_request", ExternalID: source + "/1", URL: source + "/1", Title: "Review", UpdatedAt: ownNoon},
			{Source: source, Kind: "own_pr", ExternalID: "own:" + source + "/2", URL: source + "/2", Title: "Mine", State: "approved", UpdatedAt: ownNoon},
		} {
			if err := s.store.UpsertExternalItem(it); err != nil {
				t.Fatal(err)
			}
		}
	}
	var items []plannableItem
	if err := json.Unmarshal(do(t, s, "GET", "/api/plannable", "").Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items: %+v", items)
	}
	got := map[string]string{}
	for _, it := range items {
		got[it.RefSource+"/"+it.Kind] = it.RefID
	}
	if got["gitlab/review_request"] != "gitlab/1" || got["azure/own_pr"] != "own:azure/2" {
		t.Fatalf("got: %v", got)
	}
}

func TestTodayNamesYourPRsSource(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	it := ownItem("approved")
	it.Source = "gitlab"
	if err := s.store.UpsertExternalItem(it); err != nil {
		t.Fatal(err)
	}
	if got := todayOf(t, s).YourPRs; len(got) != 1 || got[0].Source != "gitlab" {
		t.Fatalf("got: %+v", got)
	}
}

// A review marked done counts the same, wherever the PR lives.
func TestHistoryCountsAMarkedReviewFromAnyCodeHost(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	_ = s.store.CreatePlan(store.Plan{ID: "p", WindowStart: ownNoon, WindowEnd: ownNoon.Add(time.Hour), Status: "active"})
	sources := []string{"github", "gitlab", "azure", "linear"}
	for _, src := range sources {
		_ = s.store.AddPlanItem(store.PlanItem{ID: src, PlanID: "p", RefSource: src, RefID: src + "/1", Text: src})
		done := ownNoon
		if err := s.store.SetPlanItemDone(src, &done); err != nil {
			t.Fatal(err)
		}
	}
	var d HistoryDay
	if err := json.Unmarshal(get(t, s, "/api/history/day?d="+ownNoon.Format("2006-01-02")).Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Reviews != 3 {
		t.Fatalf("reviews: %d", d.Reviews)
	}
}

func TestItemDoneMarksAReviewFromAnyCodeHost(t *testing.T) {
	s := newSeededServer(t, ownNoon, nil, nil)
	_ = s.store.CreatePlan(store.Plan{ID: "p", WindowStart: ownNoon, WindowEnd: ownNoon.Add(time.Hour), Status: "active"})
	_ = s.store.AddPlanItem(store.PlanItem{ID: "i", PlanID: "p", RefSource: "azure", RefID: "https://x/pull/1"})
	if rec := do(t, s, "POST", "/api/items/i/done", `{"done":true}`); rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestConnectorsShowALastSyncFailure(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	if err := s.store.SetSetting("gitlab_token", "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetSetting("gitlab_sync_error", "gitlab: the token was refused"); err != nil {
		t.Fatal(err)
	}
	if c := connector(t, s, "GitLab"); c.SyncError != "gitlab: the token was refused" {
		t.Fatalf("connector: %+v", c)
	}
	if rec := do(t, s, "DELETE", "/api/connectors/gitlab", ""); rec.Code != 200 {
		t.Fatalf("disconnect: %d", rec.Code)
	}
	if c := connector(t, s, "GitLab"); c.SyncError != "" {
		t.Fatalf("disconnecting must forget it: %+v", c)
	}
}

func TestConnectNowSaysItsFirstSyncFailed(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	s.gitlabValidate = func(context.Context, string, string) error { return nil }
	s.syncGitLab = func(context.Context) (int, error) {
		_ = s.store.SetSetting("gitlab_sync_error", "boom")
		return 0, errors.New("boom")
	}
	rec := do(t, s, "POST", "/api/connectors/gitlab", `{"site":"gitlab.com","token":"t"}`)
	var c connectorStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil || rec.Code != 200 || c.Status != "connected" || c.SyncError != "boom" {
		t.Fatalf("%d %+v %v", rec.Code, c, err)
	}
}

func TestConnectGitLabExplainsABotToken(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	s.gitlabValidate = func(context.Context, string, string) error { return gitlab.ErrBotToken }
	rec := do(t, s, "POST", "/api/connectors/gitlab", `{"site":"gitlab.com","token":"secret-token"}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "personal access token") || !strings.Contains(rec.Body.String(), "project or group") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if c := connector(t, s, "GitLab"); c.Status != "not configured" {
		t.Fatalf("it must store nothing: %+v", c)
	}
}
