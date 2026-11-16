package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestStickersAPI(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), nil, nil)
	list := func(day string) []store.Sticker {
		var out []store.Sticker
		rec := do(t, s, "GET", "/api/stickers?d="+day, "")
		if rec.Code != 200 {
			t.Fatalf("list: %d %s", rec.Code, rec.Body)
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	if got := list("2026-10-30"); len(got) != 0 {
		t.Fatalf("none yet: %+v", got)
	}

	if rec := do(t, s, "PUT", "/api/stickers/a", `{"kind":"done","pic":"pills","name":" Vitamins ","x":20,"y":90,"w":104}`); rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "PUT", "/api/stickers/a/count", `{"day":"2026-10-30","count":1}`); rec.Code != 200 {
		t.Fatalf("count: %d %s", rec.Code, rec.Body)
	}
	got := list("2026-10-30")
	if len(got) != 1 || got[0].Name != "Vitamins" || got[0].Kind != "done" || got[0].Count != 1 || got[0].X != 20 {
		t.Fatalf("listed: %+v", got)
	}

	for _, bad := range []struct{ method, path, body string }{
		{"PUT", "/api/stickers/b", `{"kind":"timer","pic":"water","name":"x","w":104}`}, // unknown kind
		{"PUT", "/api/stickers/b", `{"kind":"counter","pic":"","name":"x","w":104}`},    // no picture
		{"PUT", "/api/stickers/b", `{"kind":"counter","pic":"water","name":"x","w":5}`}, // too small
		{"PUT", "/api/stickers/a/count", `{"day":"30.10.2026","count":1}`},              // not a date
		{"PUT", "/api/stickers/a/count", `{"day":"2026-10-30","count":-1}`},             // below zero
	} {
		if rec := do(t, s, bad.method, bad.path, bad.body); rec.Code != 400 {
			t.Errorf("%s %s %s: want 400, got %d", bad.method, bad.path, bad.body, rec.Code)
		}
	}

	if rec := do(t, s, "DELETE", "/api/stickers/a", ""); rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if got := list("2026-10-30"); len(got) != 0 {
		t.Fatalf("deleted: %+v", got)
	}
	if rec := do(t, s, "GET", "/api/stickers?d=today", ""); rec.Code != 400 {
		t.Fatalf("a bad day must be refused, got %d", rec.Code)
	}
}
