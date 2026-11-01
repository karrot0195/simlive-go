package planner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"simlive/internal/models"
	"simlive/internal/utils"
)

// retainedScheduleVersion is the only schema version the planner accepts.
// Unknown versions are treated as "no retained schedule".
const retainedScheduleVersion = 1

// RetainedSchedule is the most recent non-empty provider schedule for a
// channel, persisted so planning can continue while the provider is empty or
// failing. See specs/010-epg-cache-fallback/data-model.md.
type RetainedSchedule struct {
	Version      int                       `json:"version"`
	ChannelID    string                    `json:"channel_id"`
	ChannelEpgID string                    `json:"channel_epg_id"`
	SourceDate   string                    `json:"source_date"`
	FetchedAt    int64                     `json:"fetched_at"`
	Timezone     string                    `json:"timezone"`
	Items        []models.ESPXScheduleItem `json:"items"`
}

func retainedSchedulePath(cacheDir, channelID string) string {
	return filepath.Join(cacheDir, fmt.Sprintf("%s.json", channelID))
}

// loadRetainedSchedule reads and validates a channel's retained schedule.
// Any failure (missing, unreadable, corrupt, version/identity mismatch, empty
// items) resolves to "no retained schedule" so the planner never crashes on a
// bad cache file.
func loadRetainedSchedule(cacheDir, channelID, channelEpgID string) (*RetainedSchedule, bool) {
	if cacheDir == "" || channelID == "" {
		return nil, false
	}
	raw, err := os.ReadFile(retainedSchedulePath(cacheDir, channelID))
	if err != nil {
		return nil, false
	}

	var sched RetainedSchedule
	if err := json.Unmarshal(raw, &sched); err != nil {
		return nil, false
	}
	if sched.Version != retainedScheduleVersion {
		return nil, false
	}
	if sched.ChannelID != channelID {
		return nil, false
	}
	if sched.ChannelEpgID != channelEpgID {
		return nil, false
	}
	if len(sched.Items) == 0 {
		return nil, false
	}
	return &sched, true
}

// saveRetainedSchedule writes the retained schedule atomically: the payload is
// written to a temp file in the cache directory and renamed over the target, so
// readers never observe a torn file.
func saveRetainedSchedule(cacheDir string, sched *RetainedSchedule) error {
	if cacheDir == "" || sched == nil {
		return fmt.Errorf("epg cache: empty cache dir or schedule")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("epg cache: mkdir %s: %w", cacheDir, err)
	}

	sched.Version = retainedScheduleVersion
	if sched.FetchedAt == 0 {
		sched.FetchedAt = time.Now().Unix()
	}

	payload, err := json.Marshal(sched)
	if err != nil {
		return fmt.Errorf("epg cache: marshal: %w", err)
	}

	tmp, err := os.CreateTemp(cacheDir, ".epg-cache-*.tmp")
	if err != nil {
		return fmt.Errorf("epg cache: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("epg cache: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("epg cache: close temp: %w", err)
	}
	if err := os.Rename(tmpName, retainedSchedulePath(cacheDir, sched.ChannelID)); err != nil {
		return fmt.Errorf("epg cache: rename: %w", err)
	}
	return nil
}

func (p *Planner) epgCacheDir() string {
	if p.cacheDir != "" {
		return p.cacheDir
	}
	return utils.GetSettings().EpgCacheDir
}

func (p *Planner) loadRetainedSchedule(channel models.PlayoutChannel) (*RetainedSchedule, bool) {
	return loadRetainedSchedule(p.epgCacheDir(), channel.ID, channel.ChannelEpgID)
}

// saveProviderSchedule retains a non-empty provider response for channel.
// Failures are logged and never abort the planning run.
func (p *Planner) saveProviderSchedule(channel models.PlayoutChannel, dateStr string, items []models.ESPXScheduleItem) {
	if len(items) == 0 {
		return
	}
	sched := &RetainedSchedule{
		ChannelID:    channel.ID,
		ChannelEpgID: channel.ChannelEpgID,
		SourceDate:   dateStr,
		FetchedAt:    time.Now().Unix(),
		Timezone:     channel.ContentTimezone,
		Items:        items,
	}
	if err := saveRetainedSchedule(p.epgCacheDir(), sched); err != nil {
		p.logger.Error("[Planner] failed to save retained schedule", "channel", channel.ID, "date", dateStr, "error", err)
	}
}
