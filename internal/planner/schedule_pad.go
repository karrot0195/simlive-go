package planner

import (
	"time"

	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/utils"
)

// enableTemporaryScheduleGapFill enables the temporary pre-processing that
// duplicates head/tail schedule items so the timeline covers from now until
// the end of the processing day.
// TODO: remove this once the real schedule-gap strategy is implemented.
const enableTemporaryScheduleGapFill = true

// padScheduleToCoverDay is a temporary helper that extends the EPG schedule by
// duplicating tail items before the first item and head items after the last
// item, so the timeline covers from now until the end of the day.
// Time fields are rewritten so the existing processor can handle the duplicated
// items without any other changes.
func padScheduleToCoverDay(items []models.PartnerScheduleItem, now, horizon time.Time) []models.PartnerScheduleItem {
	if !enableTemporaryScheduleGapFill || len(items) == 0 {
		return items
	}

	firstStart, err := utils.ParseLocalTime(items[0].LocalDateStart)
	if err != nil {
		return items
	}
	lastEnd, err := utils.ParseLocalTime(items[len(items)-1].LocalDateEnd)
	if err != nil {
		return items
	}

	// (1) now < first item start -> take tail items and place them before the first item.
	if now.Before(firstStart) {
		items = prependLeadIn(items, now, firstStart)
	}

	// Recalculate after prepending.
	lastEnd, err = utils.ParseLocalTime(items[len(items)-1].LocalDateEnd)
	if err != nil {
		return items
	}

	// (2) last item end < end of day -> take head items and place them after the last item.
	if lastEnd.Before(horizon) {
		items = appendTail(items, horizon)
	}

	return items
}

// prependLeadIn duplicates a slice of tail schedule items and prepends them so
// the block ends at firstStart. Items are walked from the end until the total
// duration covers the gap.
func prependLeadIn(items []models.PartnerScheduleItem, now, firstStart time.Time) []models.PartnerScheduleItem {
	gap := firstStart.Sub(now)
	if gap <= 0 {
		return items
	}

	n := len(items)
	var selected []models.PartnerScheduleItem
	var total time.Duration

	for i := n - 1; i >= 0; i-- {
		item := items[i]
		start, _ := utils.ParseLocalTime(item.LocalDateStart)
		end, _ := utils.ParseLocalTime(item.LocalDateEnd)
		dur := end.Sub(start)
		if dur <= 0 {
			continue
		}
		total += dur

		// Prepend to keep chronological order within the block.
		selected = append([]models.PartnerScheduleItem{item}, selected...)
		if total >= gap {
			break
		}
	}

	if len(selected) == 0 {
		return items
	}

	// Shift each selected item backwards so the block ends at firstStart.
	shifted := make([]models.PartnerScheduleItem, len(selected))
	currentEnd := firstStart
	for i := len(selected) - 1; i >= 0; i-- {
		original := selected[i]
		start, _ := utils.ParseLocalTime(original.LocalDateStart)
		end, _ := utils.ParseLocalTime(original.LocalDateEnd)
		dur := end.Sub(start)

		newEnd := currentEnd
		newStart := newEnd.Add(-dur)

		shifted[i] = cloneScheduleItem(original)
		shifted[i].LocalDateStart = newStart.Format(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS)
		shifted[i].LocalDateEnd = newEnd.Format(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS)

		currentEnd = newStart
	}

	return append(shifted, items...)
}

// appendTail duplicates a slice of head schedule items and appends them so the
// block starts at the current lastEnd and continues until the gap to the
// horizon is covered.
func appendTail(items []models.PartnerScheduleItem, horizon time.Time) []models.PartnerScheduleItem {
	lastEnd, err := utils.ParseLocalTime(items[len(items)-1].LocalDateEnd)
	if err != nil {
		return items
	}
	gap := horizon.Sub(lastEnd)
	if gap <= 0 {
		return items
	}

	var selected []models.PartnerScheduleItem
	var total time.Duration
	n := len(items)

	for total < gap {
		advanced := false
		for i := 0; i < n; i++ {
			item := items[i]
			start, _ := utils.ParseLocalTime(item.LocalDateStart)
			end, _ := utils.ParseLocalTime(item.LocalDateEnd)
			dur := end.Sub(start)
			if dur <= 0 {
				continue
			}
			total += dur
			selected = append(selected, item)
			advanced = true
			if total >= gap {
				break
			}
		}
		if !advanced {
			// Avoid an infinite loop when every item has zero/negative duration.
			break
		}
	}

	if len(selected) == 0 {
		return items
	}

	shifted := make([]models.PartnerScheduleItem, len(selected))
	currentStart := lastEnd
	for i, original := range selected {
		start, _ := utils.ParseLocalTime(original.LocalDateStart)
		end, _ := utils.ParseLocalTime(original.LocalDateEnd)
		dur := end.Sub(start)

		newStart := currentStart
		newEnd := newStart.Add(dur)

		shifted[i] = cloneScheduleItem(original)
		shifted[i].LocalDateStart = newStart.Format(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS)
		shifted[i].LocalDateEnd = newEnd.Format(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS)

		currentStart = newEnd
	}

	return append(items, shifted...)
}

func cloneScheduleItem(item models.PartnerScheduleItem) models.PartnerScheduleItem {
	clone := item
	clone.Programme.ProgrammeStreams = make([]models.PartnerProgrammeStream, len(item.Programme.ProgrammeStreams))
	copy(clone.Programme.ProgrammeStreams, item.Programme.ProgrammeStreams)
	return clone
}
