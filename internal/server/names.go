package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

// maxSessionName bounds a session name; a card title has no room for more.
const maxSessionName = 120

// handleRenameSession stores the name the user typed for a session (trimmed;
// empty goes back to the default title).
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if utf8.RuneCountInString(name) > maxSessionName {
		http.Error(w, "name too long", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if _, ok, err := s.store.GetSession(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.SetSessionName(id, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, map[string]string{"Name": name})
}

// nameCards puts each session's user-given name on its card, and the title its
// agent gave it (Codex's thread names).
func nameCards(v *TodayView, names, titles map[string]string) {
	for _, lane := range []*Lane{&v.Running, &v.Waiting, &v.Idle} {
		for i := range lane.Cards {
			lane.Cards[i].Name = names[lane.Cards[i].ID]
			lane.Cards[i].Title = titles[lane.Cards[i].ID]
		}
	}
}
