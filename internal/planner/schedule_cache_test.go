package planner

import (
	"os"
	"path/filepath"
	"testing"

	"simlive/internal/models"
	"simlive/internal/utils"
)

func retainedFixture() *RetainedSchedule {
	return &RetainedSchedule{
		ChannelID:    "ch-1",
		ChannelEpgID: "epg-1",
		SourceDate:   "2026-10-29",
		Timezone:     "UTC",
		Items: []models.PartnerScheduleItem{
			makeScheduleItem("A", "2026-10-29 00:00:00", "2026-10-29 01:00:00"),
		},
	}
}

func TestRetainedScheduleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := retainedFixture()

	if err := saveRetainedSchedule(dir, want); err != nil {
		t.Fatalf("saveRetainedSchedule: %v", err)
	}

	got, ok := loadRetainedSchedule(dir, "ch-1", "epg-1")
	if !ok {
		t.Fatalf("expected retained schedule to load")
	}
	if got.Version != retainedScheduleVersion {
		t.Errorf("version = %d, want %d", got.Version, retainedScheduleVersion)
	}
	if got.SourceDate != "2026-10-29" || len(got.Items) != 1 || got.Items[0].ID != "A" {
		t.Errorf("unexpected payload: %+v", got)
	}
	if got.FetchedAt == 0 {
		t.Errorf("expected FetchedAt to be stamped")
	}
}

func TestSaveRetainedScheduleOverwritesAtomically(t *testing.T) {
	dir := t.TempDir()
	if err := saveRetainedSchedule(dir, retainedFixture()); err != nil {
		t.Fatalf("first save: %v", err)
	}

	newer := retainedFixture()
	newer.SourceDate = "2026-10-30"
	newer.Items = []models.PartnerScheduleItem{
		makeScheduleItem("B", "2026-10-30 00:00:00", "2026-10-30 02:00:00"),
	}
	if err := saveRetainedSchedule(dir, newer); err != nil {
		t.Fatalf("second save: %v", err)
	}

	got, ok := loadRetainedSchedule(dir, "ch-1", "epg-1")
	if !ok {
		t.Fatalf("expected retained schedule to load")
	}
	if got.SourceDate != "2026-10-30" || got.Items[0].ID != "B" {
		t.Errorf("expected newer payload, got %+v", got)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the cache file, got %v", names)
	}
}

func TestSaveProviderScheduleIgnoresEmptyItems(t *testing.T) {
	dir := t.TempDir()
	p := &Planner{cacheDir: dir, logger: utils.GetLogger("test-epg-cache")}
	channel := models.PlayoutChannel{ID: "ch-1", ChannelEpgID: "epg-1"}

	p.saveProviderSchedule(channel, "2026-10-29", nil)
	p.saveProviderSchedule(channel, "2026-10-29", []models.PartnerScheduleItem{})

	if _, ok := loadRetainedSchedule(dir, "ch-1", "epg-1"); ok {
		t.Fatalf("expected no cache file after empty provider response")
	}
}

func TestLoadRetainedScheduleRejectsInvalidCache(t *testing.T) {
	valid := retainedFixture()

	tests := []struct {
		name         string
		mutate       func(*RetainedSchedule)
		channelID    string
		channelEpgID string
		corrupt      bool
		raw          string
	}{
		{
			name: "version mismatch",
			raw:  `{"version":99,"channel_id":"ch-1","channel_epg_id":"epg-1","items":[{"id":"A"}]}`,
		},
		{name: "channel mismatch", channelID: "other-channel"},
		{name: "epg id mismatch", channelEpgID: "other-epg"},
		{name: "empty items", mutate: func(s *RetainedSchedule) { s.Items = nil }},
		{name: "corrupt json", corrupt: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := retainedSchedulePath(dir, "ch-1")

			if tt.corrupt || tt.raw != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				payload := tt.raw
				if tt.corrupt {
					payload = "{not-json"
				}
				if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
					t.Fatalf("write cache file: %v", err)
				}
			} else {
				sched := *valid
				sched.Items = append([]models.PartnerScheduleItem(nil), valid.Items...)
				if tt.mutate != nil {
					tt.mutate(&sched)
				}
				if err := saveRetainedSchedule(dir, &sched); err != nil {
					t.Fatalf("save: %v", err)
				}
			}

			channelID := tt.channelID
			if channelID == "" {
				channelID = "ch-1"
			}
			channelEpgID := tt.channelEpgID
			if channelEpgID == "" {
				channelEpgID = "epg-1"
			}

			if _, ok := loadRetainedSchedule(dir, channelID, channelEpgID); ok {
				t.Fatalf("expected invalid cache to resolve to no retained schedule")
			}
		})
	}
}

func TestLoadRetainedScheduleIgnoresPartialTempFile(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, ".epg-cache-123.tmp")
	if err := os.WriteFile(partial, []byte(`{"version":1`), 0o644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	if _, ok := loadRetainedSchedule(dir, "ch-1", "epg-1"); ok {
		t.Fatalf("partial temp file must not be treated as a retained schedule")
	}
}

func TestLoadRetainedScheduleMissingDirDoesNotPanic(t *testing.T) {
	if _, ok := loadRetainedSchedule(filepath.Join(t.TempDir(), "absent"), "ch-1", "epg-1"); ok {
		t.Fatalf("expected no retained schedule for a missing directory")
	}
}
