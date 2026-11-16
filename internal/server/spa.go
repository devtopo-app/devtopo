package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// uiDist holds the built single-page app. The Vite build writes here
// (build.outDir), and it is committed so a clean checkout and CI embed a real
// build. A placeholder index.html keeps this compilable before the first build.
//
//go:embed ui_dist
var uiDist embed.FS

// spaHandler serves the embedded SPA. A request for a real built asset returns
// it; any other path falls back to index.html so client-side routing works -
// except a path under /api/, which 404s rather than returning the app shell
// (an unknown API route must not look like a page).
func spaHandler() http.Handler {
	sub, err := fs.Sub(uiDist, "ui_dist")
	if err != nil {
		panic(err) // the embed path is a compile-time constant; this cannot fail
	}
	httpFS := http.FS(sub)
	fileServer := http.FileServer(httpFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if f, err := httpFS.Open(name); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r) // a real asset: let FileServer set the type
			return
		}
		// Unknown path: serve the shell so the client router can handle it.
		r = r.Clone(r.Context())
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
