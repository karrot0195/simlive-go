package planner

import (
	"time"

	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/utils"
)

// buildFallbackSchedule repeats the retained schedule from targetDayStart until
// horizon, time-shifting each occurrence so the produced items cover the whole
// target day. Durations come from the retained items; timestamps are formatted
// in tz (the channel content timezone) so the processor's
// ParseESPXTime(..., channel.ContentTimezone) yields the intended instants.
func buildFallbackSchedule(items []models.ESPXScheduleItem, targetDayStart, horizon time.Time, tz string) []models.ESPXScheduleItem {
	if len(items) == 0 || !targetDayStart.Before(horizon) {
		return nil
	}

	loc := fallbackFormatLocation(tz)

	type sequencedItem struct {
		item     models.ESPXScheduleItem
		duration time.Duration
	}

	sequence := make([]sequencedItem, 0, len(items))
	for _, item := range items {
		start, err := utils.ParseESPXTime(item.LocalDateStart, tz)
		if err != nil {
			continue
		}
		end, err := utils.ParseESPXTime(item.LocalDateEnd, tz)
		if err != nil {
			continue
		}
		duration := end.Sub(start)
		if duration <= 0 {
			continue
		}
		sequence = append(sequence, sequencedItem{item: item, duration: duration})
	}
	if len(sequence) == 0 {
		return nil
	}

	result := make([]models.ESPXScheduleItem, 0, len(sequence))
	cursor := targetDayStart
	for cursor.Before(horizon) {
		advanced := false
		for _, entry := range sequence {
			if !cursor.Before(horizon) {
				break
			}
			next := cursor.Add(entry.duration)

			shifted := cloneScheduleItem(entry.item)
			shifted.LocalDateStart = cursor.In(loc).Format(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS)
			shifted.LocalDateEnd = next.In(loc).Format(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS)
			result = append(result, shifted)

			cursor = next
			advanced = true
		}
		if !advanced {
			// Every item was skipped for lack of progress; stop rather than loop.
			break
		}
	}
	return result
}

// fallbackDecision is the outcome of deciding whether a day may be covered from
// the retained schedule.
type fallbackDecision int

const (
	// fallbackGenerate means the day is empty and a retained schedule exists.
	fallbackGenerate fallbackDecision = iota
	// fallbackSkipCovered means the target day already has timeline entries;
	// generating now would duplicate or rewind coverage (FR-007).
	fallbackSkipCovered
	// fallbackUnavailable means the provider had nothing and no retained
	// schedule can be used (FR-011).
	fallbackUnavailable
)

func decideFallback(dayHasCoverage, hasRetained bool) fallbackDecision {
	switch {
	case dayHasCoverage:
		return fallbackSkipCovered
	case !hasRetained:
		return fallbackUnavailable
	default:
		return fallbackGenerate
	}
}

// fallbackFormatLocation mirrors utils.ParseESPXTime's location resolution so
// formatting and parsing round-trip to the same instants.
func fallbackFormatLocation(tz string) *time.Location {
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	if loc, err := time.LoadLocation(configs.TZ); err == nil {
		return loc
	}
	return time.UTC
}
