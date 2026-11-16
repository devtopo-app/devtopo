package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/watch"
)

// agentCards puts on each card the names of its sub-agents working now, by
// the same windows that make the session itself read running.
func agentCards(v *TodayView, sessions []store.Session, now time.Time, open map[string]int) {
	byID := make(map[string]store.Session, len(sessions))
	for _, se := range sessions {
		byID[se.ID] = se
	}
	for _, lane := range []*Lane{&v.Running, &v.Waiting, &v.Idle} {
		for i := range lane.Cards {
			se := byID[lane.Cards[i].ID]
			lane.Cards[i].FinishedAt = se.LastEventAt
			lane.Cards[i].BackgroundTasks = open[se.TranscriptPath]
			for _, sa := range se.Subagents {
				if sa.LastEventAt.After(lane.Cards[i].FinishedAt) {
					lane.Cards[i].FinishedAt = sa.LastEventAt
				}
				if watch.DeriveStatus(now, sa.LastEventAt, sa.Tail) == watch.StatusRunning {
					lane.Cards[i].RunningAgents = append(lane.Cards[i].RunningAgents, agentName(sa.Path))
				}
			}
		}
	}
}

// agentName is a sub-agent's name from the .meta.json Claude Code writes next
// to its transcript, else its description, else "sub-agent".
func agentName(transcript string) string {
	var meta struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if b, err := os.ReadFile(strings.TrimSuffix(transcript, filepath.Ext(transcript)) + ".meta.json"); err == nil {
		_ = json.Unmarshal(b, &meta)
	}
	switch {
	case meta.Name != "":
		return meta.Name
	case meta.Description != "":
		return meta.Description
	}
	return "sub-agent"
}
