package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/devtopo-app/devtopo/internal/ingest"
)

// ReadCodex reads Codex's usage limits from the newest rate_limits Codex wrote
// into its newest rollout under roots (Codex's homes): the primary window, then
// the secondary one when there is one. Codex records them as it works, so no
// token and no network are needed. nil when there are none.
func ReadCodex(roots ...string) []Window {
	files, _ := ingest.DiscoverCodex(roots...)
	var newest string
	var at time.Time
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.ModTime().After(at) {
			newest, at = f, fi.ModTime()
		}
	}
	if newest == "" {
		return nil
	}
	f, err := os.Open(newest)
	if err != nil {
		return nil
	}
	defer f.Close()

	type limit struct {
		UsedPercent   float64 `json:"used_percent"`
		WindowMinutes int     `json:"window_minutes"`
		ResetsAt      int64   `json:"resets_at"`
	}
	var last struct{ Primary, Secondary *limit }
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // rollout lines can be long
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Payload struct {
				Type       string `json:"type"`
				RateLimits *struct {
					Primary, Secondary *limit
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Type == "event_msg" && line.Payload.Type == "token_count" && line.Payload.RateLimits != nil {
			last.Primary, last.Secondary = line.Payload.RateLimits.Primary, line.Payload.RateLimits.Secondary
		}
	}
	var out []Window
	for _, l := range []*limit{last.Primary, last.Secondary} {
		if l != nil {
			out = append(out, Window{Label: codexLabel(l.WindowMinutes), Percent: min(100, max(0, l.UsedPercent)), ResetsAt: time.Unix(l.ResetsAt, 0)})
		}
	}
	return out
}

// codexLabel names a window by its length: "5h", "week", "30d", or minutes.
func codexLabel(mins int) string {
	switch {
	case mins == 7*24*60:
		return "week"
	case mins%(24*60) == 0:
		return fmt.Sprintf("%dd", mins/(24*60))
	case mins%60 == 0:
		return fmt.Sprintf("%dh", mins/60)
	}
	return fmt.Sprintf("%dm", mins)
}
