package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/devtopo-app/devtopo/internal/azure"
	"github.com/devtopo-app/devtopo/internal/github"
	"github.com/devtopo-app/devtopo/internal/gitlab"
	"github.com/devtopo-app/devtopo/internal/jira"
	"github.com/devtopo-app/devtopo/internal/prs"
)

// connectorStatus is one connector's state for the Settings page. It carries no
// credential - only a name, a coarse status, a human detail, and the stored
// values that are not secret (Jira's site and email), so they can be changed.
type connectorStatus struct {
	Name   string
	Status string
	Detail string
	Values map[string]string `json:",omitempty"`
	// Account is who a connector is connected as (GitHub's login), when known.
	Account string `json:",omitempty"`
	// SyncError is why a code host's last sync failed, when it did.
	SyncError string `json:",omitempty"`
}

func (s *Server) handleConnectors(w http.ResponseWriter, r *http.Request) {
	state, login := s.githubStatus(r.Context(), false)
	s.writeJSON(w, []connectorStatus{
		{Name: "GitHub", Status: state, Account: login, Detail: "reviews requested of you, and your PR contribution"},
		{Name: "Linear", Status: s.linearStatus(), Detail: "your assigned issues, as plannable tasks"},
		{Name: "Calendar", Status: s.calendarStatus(), Detail: "Calendars from Google, Outlook and Microsoft Teams, Apple or any other, read through their private iCal address"},
		{Name: "Jira", Status: s.jiraStatus(), Detail: "your assigned issues, as plannable tasks", Values: s.jiraValues()},
		{Name: "GitLab", Status: s.tokenStatus("gitlab_token"), Detail: "merge requests waiting on your review, and your own", Values: s.codeHostValues("site", "gitlab_site"), SyncError: s.syncError("gitlab")},
		{Name: "Azure DevOps", Status: s.tokenStatus("azure_token"), Detail: "pull requests waiting on your review, and your own", Values: s.codeHostValues("organization", "azure_org"), SyncError: s.syncError("azure")},
	})
}

// jiraValues returns the stored Jira site and email; never the token.
func (s *Server) jiraValues() map[string]string {
	site, _, _ := s.store.GetSetting("jira_site")
	email, _, _ := s.store.GetSetting("jira_email")
	return map[string]string{"site": site, "email": email}
}

// handleDisconnect forgets a connector's credentials and the items it synced,
// so nothing of it is left. The loops treat empty credentials as not set.
func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	keys := map[string][]string{
		"linear":   {"linear_api_key"},
		"jira":     {"jira_api_token", "jira_site", "jira_email"},
		"calendar": {"calendar_ical_url"},
		"gitlab":   {"gitlab_token", "gitlab_site", "gitlab_sync_error"},
		"azure":    {"azure_token", "azure_org", "azure_sync_error"},
	}[r.PathValue("name")]
	if keys == nil {
		http.Error(w, "unknown connector", http.StatusNotFound)
		return
	}
	for _, k := range keys {
		if err := s.store.SetSetting(k, ""); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	var err error
	if name := r.PathValue("name"); name == "calendar" {
		err = s.store.RemoveAllCalendarFeeds()
	} else {
		err = s.store.ReplaceExternalItems(name, nil)
	}
	if err == nil {
		err = prs.Prune(s.store)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, map[string]string{"Status": "not configured"})
}

// jiraStatus reports connected when an API token is stored (it is only stored
// together with the site and email). It never reads the token out.
func (s *Server) jiraStatus() string {
	if v, ok, err := s.store.GetSetting("jira_api_token"); err == nil && ok && v != "" {
		return "connected"
	}
	return "not configured"
}

// handleConnectJira stores the Jira site, email and API token (the token is
// write-only: it is never returned). The daemon's Jira loop re-reads them on
// its next pass.
func (s *Server) handleConnectJira(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Site     string `json:"site"`
		Email    string `json:"email"`
		APIToken string `json:"apiToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	site, err := jira.NormalizeSite(body.Site)
	if err != nil || strings.TrimSpace(body.Email) == "" || strings.TrimSpace(body.APIToken) == "" {
		http.Error(w, "site, email and apiToken required", http.StatusBadRequest)
		return
	}
	// Verify the credentials work before storing them, so "connected" is honest.
	// The error carries no token and no upstream body.
	if err := s.jiraValidate(r.Context(), site, body.Email, body.APIToken); err != nil {
		msg := "could not verify the Jira credentials - check them and try again"
		switch {
		case errors.Is(err, jira.ErrNotJira):
			msg = site + " is not a Jira site - use the address you see in the browser, like your-team.atlassian.net"
		case errors.Is(err, jira.ErrRefused):
			msg = "Jira refused the email and token - the token must belong to that email and have read:jira-work and read:jira-user"
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}
	// The token goes last: jiraStatus and Sync treat a stored token as connected.
	for _, kv := range [][2]string{{"jira_site", site}, {"jira_email", body.Email}, {"jira_api_token", body.APIToken}} {
		if err := s.store.SetSetting(kv[0], kv[1]); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// Sync now so the user's issues appear immediately (see handleConnectLinear
	// for why a sync failure does not fail the connect).
	if s.syncJira != nil {
		_, _ = s.syncJira(r.Context())
	}
	s.writeJSON(w, connectorStatus{Name: "Jira", Status: "connected"})
}

// tokenStatus reports connected when a code host's token is stored, which happens
// last when connecting. It never reads the token out.
func (s *Server) tokenStatus(key string) string {
	if v, ok, err := s.store.GetSetting(key); err == nil && ok && v != "" {
		return "connected"
	}
	return "not configured"
}

// codeHostValues returns a code host's stored address, for changing it; never its token.
func (s *Server) codeHostValues(field, key string) map[string]string {
	v, _, _ := s.store.GetSetting(key)
	return map[string]string{field: v}
}

// syncError is why a code host's last sync failed, "" when it didn't.
func (s *Server) syncError(source string) string {
	v, _, _ := s.store.GetSetting(source + "_sync_error")
	return v
}

// storeCodeHost saves a checked address and token and syncs at once. The token goes
// last: tokenStatus and Sync treat a stored token as connected. A failed sync doesn't
// fail the connect (see handleConnectLinear), but the answer says why it failed.
func (s *Server) storeCodeHost(w http.ResponseWriter, r *http.Request, name, source, addressKey, address, tokenKey, token string, sync func(context.Context) (int, error)) {
	for _, kv := range [][2]string{{addressKey, address}, {tokenKey, token}} {
		if err := s.store.SetSetting(kv[0], kv[1]); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if sync != nil {
		_, _ = sync(r.Context())
	}
	s.writeJSON(w, connectorStatus{Name: name, Status: "connected", SyncError: s.syncError(source)})
}

// handleConnectGitLab checks and stores the GitLab address and access token (the
// token is write-only). The error carries no token and no upstream body.
func (s *Server) handleConnectGitLab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Site  string `json:"site"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(body.Token)
	site, err := gitlab.NormalizeSite(body.Site)
	if err != nil || token == "" {
		http.Error(w, "site and token required", http.StatusBadRequest)
		return
	}
	if err := s.gitlabValidate(r.Context(), site, token); err != nil {
		msg := "could not verify the GitLab token - check it and try again"
		switch {
		case errors.Is(err, gitlab.ErrNotGitLab):
			msg = site + " is not a GitLab server we can reach - use the address you see in the browser, like gitlab.com"
		case errors.Is(err, gitlab.ErrBotToken):
			msg = "This is a project or group access token, which acts as a bot and has no merge requests of its own - create a personal access token under your own profile"
		case errors.Is(err, gitlab.ErrRefused):
			msg = "GitLab refused the token - it must be a personal access token with the read_api scope"
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}
	s.storeCodeHost(w, r, "GitLab", "gitlab", "gitlab_site", site, "gitlab_token", token, s.syncGitLab)
}

// handleConnectAzure checks and stores the Azure DevOps organization and personal
// access token (the token is write-only). The error carries no token and no
// upstream body.
func (s *Server) handleConnectAzure(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Organization string `json:"organization"`
		Token        string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(body.Token)
	base, err := azure.NormalizeOrganization(body.Organization)
	if err != nil || token == "" {
		http.Error(w, "organization and token required", http.StatusBadRequest)
		return
	}
	if err := s.azureValidate(r.Context(), base, token); err != nil {
		msg := "could not verify the Azure DevOps token - check it and try again"
		switch {
		case errors.Is(err, azure.ErrNotFound):
			msg = "No Azure DevOps organization at " + base + " - use the name you see in dev.azure.com/your-organization"
		case errors.Is(err, azure.ErrRefused):
			msg = "Azure DevOps refused the token - it must belong to this organization and have the Code (Read) scope"
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}
	s.storeCodeHost(w, r, "Azure DevOps", "azure", "azure_org", base, "azure_token", token, s.syncAzure)
}

// githubStatus reports whether the gh CLI DevTopo reads GitHub through is
// installed and logged in, and as whom - not whether anything was synced yet,
// since an account with no open PRs or review requests is still connected. gh
// is run once and the answer cached; recheck runs it again.
func (s *Server) githubStatus(ctx context.Context, recheck bool) (state, login string) {
	s.ghMu.Lock()
	defer s.ghMu.Unlock()
	if s.ghStatus == nil {
		return github.StateMissing, ""
	}
	if recheck || s.ghState == "" {
		s.ghState, s.ghLogin = s.ghStatus(ctx)
	}
	return s.ghState, s.ghLogin
}

// handleGitHubCheck runs the gh check again (after the user installed gh or
// logged in, without restarting DevTopo) and, when connected, syncs at once.
func (s *Server) handleGitHubCheck(w http.ResponseWriter, r *http.Request) {
	state, login := s.githubStatus(r.Context(), true)
	if state == github.StateConnected && s.syncGitHub != nil {
		_, _ = s.syncGitHub(r.Context())
	}
	s.writeJSON(w, connectorStatus{Name: "GitHub", Status: state, Account: login})
}

// linearStatus reports connected when an API key is stored. It never reads the
// key out to a response.
func (s *Server) linearStatus() string {
	if v, ok, err := s.store.GetSetting("linear_api_key"); err == nil && ok && v != "" {
		return "connected"
	}
	return "not configured"
}

// handleConnectLinear stores the user's Linear API key (write-only: it is never
// returned). The daemon's Linear loop re-reads it on its next pass.
func (s *Server) handleConnectLinear(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.APIKey) == "" {
		http.Error(w, "apiKey required", http.StatusBadRequest)
		return
	}
	// Verify the key works before storing it, so "connected" is honest. The
	// error carries no key and no upstream body.
	if err := s.linearValidate(r.Context(), body.APIKey); err != nil {
		http.Error(w, "could not verify the Linear key - check it and try again", http.StatusBadGateway)
		return
	}
	if err := s.store.SetSetting("linear_api_key", body.APIKey); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Sync now so the user's issues appear immediately. A transient failure here
	// does not fail the connect - the key is validated and stored, and the
	// background loop (and the next planner open) will catch up.
	if s.syncLinear != nil {
		_, _ = s.syncLinear(r.Context())
	}
	s.writeJSON(w, connectorStatus{Name: "Linear", Status: "connected"})
}

// handleRefresh syncs every configured connector once and returns a per-source
// count. The Planner calls it on open so the candidate list and capacity are
// fresh. One source failing does not fail the others or the request: a buddy
// shows what it can.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	out := map[string]int{}
	for name, sync := range map[string]func(context.Context) (int, error){
		"github": s.syncGitHub, "linear": s.syncLinear, "calendar": s.syncCalendar, "jira": s.syncJira,
		"gitlab": s.syncGitLab, "azure": s.syncAzure,
	} {
		if sync == nil {
			continue
		}
		if n, err := sync(r.Context()); err == nil {
			out[name] = n
		}
	}
	s.writeJSON(w, out)
}
