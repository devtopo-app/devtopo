package server

import (
	"encoding/json"
	"net/http"

	"github.com/devtopo-app/devtopo/internal/update"
)

// SetUpdate gives the server the update checker the daemon runs, so the page
// shows its result. Without one the server reports a build that cannot check.
func (s *Server) SetUpdate(c *update.Checker) {
	if c != nil {
		s.update = c
	}
}

// updateEnabled reads the update_check setting: on unless set to "off".
func (s *Server) updateEnabled() bool {
	return update.EnabledIn(s.store.GetSetting)
}

func (s *Server) registerUpdate(mux *http.ServeMux) {
	// GET returns the last result without asking GitHub.
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, s.update.Status())
	})
	// POST asks GitHub now, whether or not automatic checks are on: the user asked.
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, s.update.CheckNow(r.Context()))
	})
	mux.HandleFunc("PUT /api/update/enabled", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
			http.Error(w, `body must be {"enabled": true|false}`, http.StatusBadRequest)
			return
		}
		v := "on"
		if !*body.Enabled {
			v = "off"
		}
		if err := s.store.SetSetting(update.SettingKey, v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSON(w, s.update.Status())
	})
}
