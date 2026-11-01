package planner

import (
	"testing"
	"time"

	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/utils"
)

func mustParse(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS, value, time.UTC)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func assertContinuous(t *testing.T, items []models.PartnerScheduleItem, tz string) {
	t.Helper()
	for i := 1; i < len(items); i++ {
		prevEnd, err := utils.ParsePartnerTime(items[i-1].LocalDateEnd, tz)
		if err != nil {
			t.Fatalf("parse prev end: %v", err)
		}
		nextStart, err := utils.ParsePartnerTime(items[i].LocalDateStart, tz)
		if err != nil {
			t.Fatalf("parse next start: %v", err)
		}
		if !prevEnd.Equal(nextStart) {
			t.Fatalf("gap/overlap at %d: %s != %s", i, prevEnd, nextStart)
		}
	}
}

func TestBuildFallbackScheduleCoversWholeTargetDay(t *testing.T) {
	tz := "UTC"
	source := []models.PartnerScheduleItem{
		makeScheduleItem("A", "2026-10-28 10:00:00", "2026-10-28 11:00:00"),
		makeScheduleItem("B", "2026-10-28 11:00:00", "2026-10-28 12:00:00"),
	}

	targetDayStart := mustParse(t, "2026-10-30 00:00:00")
	horizon := utils.EndOfDay(targetDayStart)

	got := buildFallbackSchedule(source, targetDayStart, horizon, tz)
	if len(got) == 0 {
		t.Fatalf("expected fallback coverage")
	}

	first, _ := utils.ParsePartnerTime(got[0].LocalDateStart, tz)
	if !first.Equal(targetDayStart) {
		t.Errorf("first item starts at %s, want %s", first, targetDayStart)
	}

	last, _ := utils.ParsePartnerTime(got[len(got)-1].LocalDateEnd, tz)
	if last.Before(horizon) {
		t.Errorf("last item ends at %s, want >= horizon %s", last, horizon)
	}

	assertContinuous(t, got, tz)
}

func TestBuildFallbackScheduleRepeatsSourceAcrossCycles(t *testing.T) {
	tz := "UTC"
	source := []models.PartnerScheduleItem{
		makeScheduleItem("A", "2026-10-28 00:00:00", "2026-10-28 01:00:00"),
		makeScheduleItem("B", "2026-10-28 01:00:00", "2026-10-28 02:00:00"),
	}

	targetDayStart := mustParse(t, "2026-10-30 00:00:00")
	horizon := utils.EndOfDay(targetDayStart)

	got := buildFallbackSchedule(source, targetDayStart, horizon, tz)
	if len(got) < 12 {
		t.Fatalf("expected repeated cycles to fill the day, got %d items", len(got))
	}
	if got[0].ID != "A" || got[1].ID != "B" || got[2].ID != "A" {
		t.Errorf("expected repeating A,B order, got %s,%s,%s", got[0].ID, got[1].ID, got[2].ID)
	}
	assertContinuous(t, got, tz)
}

func TestBuildFallbackScheduleSkipsNonPositiveDurations(t *testing.T) {
	tz := "UTC"
	source := []models.PartnerScheduleItem{
		makeScheduleItem("ZERO", "2026-10-28 00:00:00", "2026-10-28 00:00:00"),
		makeScheduleItem("NEGATIVE", "2026-10-28 02:00:00", "2026-10-28 01:00:00"),
		makeScheduleItem("VALID", "2026-10-28 03:00:00", "2026-10-28 04:00:00"),
	}

	targetDayStart := mustParse(t, "2026-10-30 00:00:00")
	horizon := targetDayStart.Add(4 * time.Hour)

	got := buildFallbackSchedule(source, targetDayStart, horizon, tz)
	if len(got) == 0 {
		t.Fatalf("expected fallback coverage")
	}
	for _, item := range got {
		if item.ID != "VALID" {
			t.Errorf("unexpected item %q in fallback output", item.ID)
		}
	}
	assertContinuous(t, got, tz)
}

func TestBuildFallbackScheduleStopsWithoutProgress(t *testing.T) {
	tz := "UTC"
	source := []models.PartnerScheduleItem{
		makeScheduleItem("ZERO", "2026-10-28 00:00:00", "2026-10-28 00:00:00"),
	}

	targetDayStart := mustParse(t, "2026-10-30 00:00:00")
	if got := buildFallbackSchedule(source, targetDayStart, utils.EndOfDay(targetDayStart), tz); len(got) != 0 {
		t.Fatalf("expected no coverage when every source item has no duration, got %d", len(got))
	}
}

func TestBuildFallbackScheduleShiftsAcrossMonthBoundary(t *testing.T) {
	tz := "UTC"
	source := []models.PartnerScheduleItem{
		makeScheduleItem("A", "2026-01-31 20:00:00", "2026-01-31 21:00:00"),
	}

	targetDayStart := mustParse(t, "2026-02-01 00:00:00")
	horizon := targetDayStart.Add(2 * time.Hour)

	got := buildFallbackSchedule(source, targetDayStart, horizon, tz)
	if len(got) < 2 {
		t.Fatalf("expected repeated coverage across the month boundary, got %d", len(got))
	}
	if got[0].LocalDateStart != "2026-02-01 00:00:00" {
		t.Errorf("first shift = %q, want 2026-02-01 00:00:00", got[0].LocalDateStart)
	}
	if got[1].LocalDateStart != "2026-02-01 01:00:00" {
		t.Errorf("second shift = %q, want 2026-02-01 01:00:00", got[1].LocalDateStart)
	}
	assertContinuous(t, got, tz)
}

func TestDecideFallback(t *testing.T) {
	tests := []struct {
		name           string
		dayHasCoverage bool
		hasRetained    bool
		want           fallbackDecision
	}{
		{name: "empty day with cache generates", dayHasCoverage: false, hasRetained: true, want: fallbackGenerate},
		{name: "covered day is skipped", dayHasCoverage: true, hasRetained: true, want: fallbackSkipCovered},
		{name: "covered day is skipped even without cache", dayHasCoverage: true, hasRetained: false, want: fallbackSkipCovered},
		{name: "empty day without cache is unrecoverable", dayHasCoverage: false, hasRetained: false, want: fallbackUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideFallback(tt.dayHasCoverage, tt.hasRetained); got != tt.want {
				t.Fatalf("decideFallback(%v, %v) = %v, want %v", tt.dayHasCoverage, tt.hasRetained, got, tt.want)
			}
		})
	}
}
