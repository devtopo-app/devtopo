package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/devtopo-app/devtopo/internal/store"
)

// maxStickerName bounds a sticker's name; it shows in a small tooltip.
const maxStickerName = 40

// validDay reports whether d is a YYYY-MM-DD date.
func validDay(d string) bool {
	_, err := time.Parse("2006-01-02", d)
	return err == nil
}

// handleListStickers returns every sticker with its count on ?d=YYYY-MM-DD,
// the browser's own date.
func (s *Server) handleListStickers(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("d")
	if !validDay(day) {
		http.Error(w, "d must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	stickers, err := s.store.ListStickers(day)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, stickers)
}

// handlePutSticker adds the sticker with the path's id, or updates it.
func (s *Server) handlePutSticker(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Kind string  `json:"kind"`
		Pic  string  `json:"pic"`
		Name string  `json:"name"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
		W    float64 `json:"w"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(raw.Name)
	switch {
	case raw.Kind != "counter" && raw.Kind != "done":
		http.Error(w, "kind must be counter or done", http.StatusBadRequest)
	case raw.Pic == "" || len(raw.Pic) > 32:
		http.Error(w, "pic required", http.StatusBadRequest)
	case utf8.RuneCountInString(name) > maxStickerName:
		http.Error(w, "name too long", http.StatusBadRequest)
	case raw.W < 48 || raw.W > 400:
		http.Error(w, "w must be 48 to 400", http.StatusBadRequest)
	default:
		st := store.Sticker{ID: r.PathValue("id"), Kind: raw.Kind, Pic: raw.Pic, Name: name, X: raw.X, Y: raw.Y, W: raw.W}
		if err := s.store.PutSticker(st); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSON(w, st)
	}
}

// handleStickerCount stores a sticker's count on one day.
func (s *Server) handleStickerCount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Day   string `json:"day"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if !validDay(body.Day) || body.Count < 0 {
		http.Error(w, "day must be YYYY-MM-DD and count 0 or more", http.StatusBadRequest)
		return
	}
	if err := s.store.SetStickerCount(r.PathValue("id"), body.Day, body.Count); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, body)
}

// handleDeleteSticker removes a sticker and its counts.
func (s *Server) handleDeleteSticker(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSticker(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, map[string]string{"ID": r.PathValue("id")})
}
