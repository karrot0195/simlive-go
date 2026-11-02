package planner

import (
	"testing"
	"time"

	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/utils"
)

func makeScheduleItem(id, start, end string) models.PartnerScheduleItem {
	return models.PartnerScheduleItem{
		ID:             id,
		LocalDateStart: start,
		LocalDateEnd:   end,
	}
}

func TestPadScheduleToCoverDayLeadInAndTail(t *testing.T) {
	items := []models.PartnerScheduleItem{
		makeScheduleItem("A", "2026-10-28 08:00:00", "2026-10-28 09:00:00"),
		makeScheduleItem("B", "2026-10-28 09:00:00", "2026-10-28 10:00:00"),
		makeScheduleItem("C", "2026-10-28 10:00:00", "2026-10-28 11:00:00"),
	}

	now, _ := time.ParseInLocation(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS, "2026-10-28 06:00:00", utils.LocalLocation())
	horizon := utils.EndOfDay(now)

	padded := padScheduleToCoverDay(items, now, horizon)

	if len(padded) < len(items) {
		t.Fatalf("expected padded length >= %d, got %d", len(items), len(padded))
	}

	// The first padded item should start at or before now.
	first, _ := utils.ParseLocalTime(padded[0].LocalDateStart)
	if first.After(now) {
		t.Errorf("expected first padded item to start at or before now, got %s", first)
	}

	// The last padded item should end at or after horizon.
	last, _ := utils.ParseLocalTime(padded[len(padded)-1].LocalDateEnd)
	if last.Before(horizon) {
		t.Errorf("expected last padded item to end at or after horizon, got %s", last)
	}

	// Original items should remain in order and unchanged in the middle.
	if padded[2].ID != "A" || padded[3].ID != "B" || padded[4].ID != "C" {
		t.Errorf("expected original items in order A,B,C, got %s,%s,%s", padded[2].ID, padded[3].ID, padded[4].ID)
	}
}

func TestPadScheduleToCoverDayNoLeadInWhenNowAfterFirst(t *testing.T) {
	items := []models.PartnerScheduleItem{
		makeScheduleItem("A", "2026-10-28 08:00:00", "2026-10-28 09:00:00"),
		makeScheduleItem("B", "2026-10-28 09:00:00", "2026-10-28 10:00:00"),
	}

	now, _ := time.ParseInLocation(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS, "2026-10-28 08:30:00", utils.LocalLocation())
	horizon := utils.EndOfDay(now)

	padded := padScheduleToCoverDay(items, now, horizon)

	if padded[0].ID != "A" {
		t.Errorf("expected no lead-in, first item should be A, got %s", padded[0].ID)
	}
}
