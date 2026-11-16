package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A page on another site open in the same browser can POST to the daemon on
// localhost. Writes from a foreign Origin must be refused before they touch the
// store, or that page could replace a stored key or calendar URL.

func TestCrossSiteWriteIsRefused(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.linearValidate = func(context.Context, string) error { return nil }
	r := httptest.NewRequest("POST", "/api/connectors/linear", strings.NewReader(`{"apiKey":"lin_api_test"}`))
	r.Host = testHost
	r.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatalf("cross-site write must be refused with 403, got %d", rec.Code)
	}
	if _, ok, _ := s.store.GetSetting("linear_api_key"); ok {
		t.Fatal("a refused write must not store anything")
	}
}

func TestSameOriginWriteIsAllowed(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.linearValidate = func(context.Context, string) error { return nil }
	r := httptest.NewRequest("POST", "/api/connectors/linear", strings.NewReader(`{"apiKey":"lin_api_test"}`))
	r.Host = "127.0.0.1:8787"
	r.Header.Set("Origin", "http://127.0.0.1:8787")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("same-origin write must pass, got %d: %s", rec.Code, rec.Body.String())
	}
}

// testHost is the Host the daemon sees from its own page in the browser.
const testHost = "127.0.0.1:8787"

// DNS rebinding also lets a foreign page read: its own name, pointed at
// 127.0.0.1, arrives as the Host. Only loopback names may read anything.
func TestReadFromAForeignHostIsRefused(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	for _, path := range []string{"/api/today", "/api/connectors", "/"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "evil.example:8787"
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, r)
		if rec.Code != 403 {
			t.Errorf("GET %s from a foreign host must be refused with 403, got %d", path, rec.Code)
		}
	}
}

func TestReadFromALoopbackHostIsAllowed(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	for _, h := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787", "localhost", "[::1]"} {
		r := httptest.NewRequest("GET", "/api/ping", nil)
		r.Host = h
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Errorf("GET from %q must pass, got %d: %s", h, rec.Code, rec.Body.String())
		}
	}
}

// DNS rebinding: an attacker's name pointed at 127.0.0.1 makes Origin and Host
// agree, so the host itself must be a loopback name.
func TestRebindingWriteIsRefused(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	r := httptest.NewRequest("DELETE", "/api/plans/p1", nil)
	r.Host = "rebind.evil:8787"
	r.Header.Set("Origin", "http://rebind.evil:8787")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatalf("a rebound write must be refused with 403, got %d", rec.Code)
	}
}
