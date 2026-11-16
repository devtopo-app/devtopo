package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// With an access key set, only a browser holding it in its cookie reaches the
// API; another program on the machine gets 401. The key arrives once in the
// address and is swapped for an HttpOnly, SameSite=Strict cookie.
func TestAccessKeyGuardsTheAPI(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), nil, nil)
	s.SetAccessKey("secret-key")

	if rec := get(t, s, "/api/today"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the key: %d", rec.Code)
	}
	if rec := get(t, s, "/legacy"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the legacy page holds data too: %d", rec.Code)
	}
	if rec := get(t, s, "/api/ping"); rec.Code != http.StatusOK {
		t.Fatalf("a second start must still find DevTopo: %d", rec.Code)
	}
	if rec := get(t, s, "/"); rec.Code != http.StatusOK {
		t.Fatalf("the page itself carries no data: %d", rec.Code)
	}

	// The address DevTopo opens sets the cookie and drops the key from the address.
	rec := get(t, s, "/?k=secret-key")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("redirect: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == keyCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != "secret-key" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", cookie)
	}

	withCookie := func(path, value string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = testHost
		req.AddCookie(&http.Cookie{Name: keyCookie, Value: value})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := withCookie("/api/today", "secret-key"); code != http.StatusOK {
		t.Fatalf("with the key: %d", code)
	}
	if code := withCookie("/api/today", "guess"); code != http.StatusUnauthorized {
		t.Fatalf("with a wrong key: %d", code)
	}
	// A wrong key in the address sets no cookie.
	if rec := get(t, s, "/?k=guess"); len(rec.Result().Cookies()) != 0 {
		t.Fatal("a wrong key must set no cookie")
	}
}

// No other site may show DevTopo in a frame.
func TestPagesForbidFraming(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), nil, nil)
	for _, path := range []string{"/", "/api/today"} {
		rec := get(t, s, path)
		if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
			t.Fatalf("%s: %v", path, rec.Header())
		}
	}
}
