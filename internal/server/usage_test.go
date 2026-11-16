package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/usage"
)

func TestAPIUsageServesWindows(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	s.usage = func(ctx context.Context) usage.Result {
		return usage.Result{Windows: []usage.Window{{Label: "5h", Percent: 12.5, ResetsAt: tnow.Add(4 * time.Hour)}}}
	}
	rec := get(t, s, "/api/usage")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Windows []struct {
			Label    string
			Percent  float64
			ResetsAt time.Time
		}
		Status string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Windows) != 1 || got.Windows[0].Label != "5h" || got.Windows[0].Percent != 12.5 || !got.Windows[0].ResetsAt.Equal(tnow.Add(4*time.Hour)) || got.Status != "" {
		t.Fatalf("payload: %s", rec.Body.String())
	}
}

func TestAPIUsageServesStatus(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	s.usage = func(ctx context.Context) usage.Result { return usage.Result{Status: usage.StatusExpired} }
	rec := get(t, s, "/api/usage")
	if !strings.Contains(rec.Body.String(), `"Status":"sign in to Claude Code again"`) || !strings.Contains(rec.Body.String(), `"Windows":[]`) {
		t.Fatalf("payload: %s", rec.Body.String())
	}
}
