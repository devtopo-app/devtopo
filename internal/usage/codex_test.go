package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tokenCount(rateLimits string) string {
	return `{"timestamp":"2026-10-29T09:00:00Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":` + rateLimits + `}}` + "\n"
}

// Codex's limits come from the newest rate_limits in its newest rollout, one
// window per limit labelled by its length.
func TestReadCodex(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "sessions", "2026", "10", "29")
	os.MkdirAll(day, 0o755)
	old := filepath.Join(day, "rollout-2026-10-29T08-00-00-a.jsonl")
	os.WriteFile(old, []byte(tokenCount(`{"primary":{"used_percent":99,"window_minutes":300,"resets_at":1}}`)), 0o644)
	os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	newest := filepath.Join(day, "rollout-2026-10-29T07-00-00-b.jsonl") // older name, newer write
	os.WriteFile(newest, []byte(
		tokenCount(`{"primary":{"used_percent":1,"window_minutes":300,"resets_at":1}}`)+
			tokenCount(`{"limit_id":"codex","primary":{"used_percent":12.5,"window_minutes":300,"resets_at":1792591200},"secondary":{"used_percent":140,"window_minutes":10080,"resets_at":1792900000},"plan_type":"plus"}`)+
			tokenCount(`null`)+
			`{"timestamp":"2026-10-29T09:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[]}}`+"\n"), 0o644)

	got := ReadCodex(root)
	want := []Window{
		{Label: "5h", Percent: 12.5, ResetsAt: time.Unix(1792591200, 0)},
		{Label: "week", Percent: 100, ResetsAt: time.Unix(1792900000, 0)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i].Label != want[i].Label || got[i].Percent != want[i].Percent || !got[i].ResetsAt.Equal(want[i].ResetsAt) {
			t.Fatalf("window %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestReadCodexWithoutCodex(t *testing.T) {
	if got := ReadCodex(t.TempDir()); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestCodexLabel(t *testing.T) {
	for mins, want := range map[int]string{300: "5h", 10080: "week", 43200: "30d", 1440: "1d", 90: "90m"} {
		if got := codexLabel(mins); got != want {
			t.Errorf("%d: got %q want %q", mins, got, want)
		}
	}
}

// With two Codex homes the newest rollout across both gives the limits.
func TestReadCodexAcrossHomes(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, r := range []struct {
		root string
		pct  string
		age  time.Duration
	}{{a, "10", time.Hour}, {b, "20", 0}} {
		day := filepath.Join(r.root, "sessions", "2026", "10", "29")
		os.MkdirAll(day, 0o755)
		p := filepath.Join(day, "rollout-2026-10-29T08-00-00-x.jsonl")
		os.WriteFile(p, []byte(tokenCount(`{"primary":{"used_percent":`+r.pct+`,"window_minutes":300,"resets_at":1}}`)), 0o644)
		os.Chtimes(p, time.Now().Add(-r.age), time.Now().Add(-r.age))
	}
	if got := ReadCodex(a, b); len(got) != 1 || got[0].Percent != 20 {
		t.Fatalf("got %+v", got)
	}
}
