package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// keyCookie holds the access key in the browser, and keyParam brings it there
// once, in the address DevTopo opens.
const (
	keyCookie = "devtopo_key"
	keyParam  = "k"
)

// SetAccessKey makes the API answer only a browser that holds key: the one
// DevTopo opened, at an address carrying it. Without a key (tests, until set)
// the API is open to any loopback request, as before.
func (s *Server) SetAccessKey(key string) { s.accessKey = key }

// keyGuard keeps the API to the browser DevTopo opened. Other programs and
// other users on this machine can reach the loopback port, so the Host and
// Origin checks alone do not stop them reading sessions or starting a resume.
//
// A page address carrying ?k=<key> swaps it for a cookie (HttpOnly, so the
// page's scripts never see it; SameSite=Strict, so no other site's request
// carries it) and redirects to the same address without it, out of the
// address bar and history. Every /api/ request, and the /legacy page, then
// needs that cookie, except /api/ping, which a second start uses to find a
// running DevTopo. The page itself and its scripts and images carry no data
// and need none.
func (s *Server) keyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.accessKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		if k := r.URL.Query().Get(keyParam); k != "" {
			if s.keyMatches(k) {
				http.SetCookie(w, &http.Cookie{
					Name: keyCookie, Value: k, Path: "/", MaxAge: 10 * 365 * 24 * 3600,
					HttpOnly: true, SameSite: http.SameSiteStrictMode,
				})
			}
			q := r.URL.Query()
			q.Del(keyParam)
			to := r.URL.Path
			if enc := q.Encode(); enc != "" {
				to += "?" + enc
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		guarded := (strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/ping") || r.URL.Path == "/legacy"
		if guarded {
			c, err := r.Cookie(keyCookie)
			if err != nil || !s.keyMatches(c.Value) {
				http.Error(w, "This tab is not signed in to DevTopo. Open it from DevTopo's icon in the tray or menu bar (Open DevTopo).", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) keyMatches(k string) bool {
	return subtle.ConstantTimeCompare([]byte(k), []byte(s.accessKey)) == 1
}

// noFraming forbids other sites to show DevTopo inside a frame, where a
// transparent overlay could trick clicks onto its buttons.
func noFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
