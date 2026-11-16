package server

import (
	"net/http"

	"github.com/devtopo-app/devtopo/internal/usage"
)

// handleUsage serves GET /api/usage: the Claude subscription limits for the
// status bar, or a Status saying why there are none, and Codex's limits.
// Windows and Codex are [] rather than null so the UI can map them.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	res := s.usage(r.Context())
	if res.Windows == nil {
		res.Windows = []usage.Window{}
	}
	codex := s.codexUsage()
	if codex == nil {
		codex = []usage.Window{}
	}
	s.writeJSON(w, struct {
		usage.Result
		Codex []usage.Window
	}{res, codex})
}
