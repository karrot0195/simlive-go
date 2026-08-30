package utils

import (
	"fmt"
	"simlive/internal/configs"
	"time"
)

func ParseESPXTime(timeStr, tzStr string) (time.Time, error) {
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		loc = GetLoc()
	}

	if t, err := time.ParseInLocation(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS, timeStr, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

func GetLoc() *time.Location {
	return time.FixedZone(configs.TZ, configs.TZ_OFFSET)
}

// TimeNumToText converts a Unix timestamp (seconds) to local time text (HH:MM:SS), e.g. 01:23:45
func TimeNumToText(timeNum int64) string {
	return time.Unix(timeNum, 0).In(GetLoc()).Format("15:04:05")
}

func EndOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location())
}

// StartOfDay returns the local midnight of t (00:00:00.000000000 in t's location).
func StartOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// GetLocFor returns the IANA *time.Location for tzStr, or UTC if the IANA name
// is not recognised. Used so the planner can compute "today"/"tomorrow" in the
// channel's content timezone (e.g. "Asia/Saigon") rather than the server's zone.
func GetLocFor(tzStr string) *time.Location {
	if tzStr == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tzStr); err == nil {
		return loc
	}
	return time.UTC
}
