package utils

import (
	"fmt"
	"os"
	"simlive/internal/configs"
	"time"
)

// LocalLocation returns the runtime's local timezone. Resolution order:
//  1. the TZ environment variable if it names a valid IANA zone,
//  2. the process's local location (time.Now().Location()),
//  3. configs.TZ as a fallback.
//
// configs.TZ is only used as a last-resort backup, never as the primary choice.
func LocalLocation() *time.Location {
	if tz := os.Getenv("TZ"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if loc := time.Now().Location(); loc != nil && loc.String() != "UTC" {
		return loc
	}
	if loc, err := time.LoadLocation(configs.TZ); err == nil {
		return loc
	}
	return time.UTC
}

// LocalTZOffset returns the local timezone as a "+HH:MM" offset string, e.g. "+07:00".
func LocalTZOffset() string {
	loc := LocalLocation()
	now := time.Now().In(loc)
	_, offset := now.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	return fmt.Sprintf("%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
}

func ParsePartnerTime(timeStr, tzStr string) (time.Time, error) {
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		loc, err = time.LoadLocation(configs.TZ)
		if err != nil {
			loc = time.UTC
		}
	}

	if t, err := time.ParseInLocation(configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS, timeStr, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

// TimeNumToText converts a Unix timestamp (seconds) to local time text (HH:MM:SS), e.g. 01:23:45
func TimeNumToText(timeNum int64) string {
	return time.Unix(timeNum, 0).In(LocalLocation()).Format("15:04:05")
}

func EndOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location())
}

// StartOfDay returns the local midnight of t (00:00:00.000000000 in t's location).
func StartOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// GetTimeLocation returns the IANA *time.Location for tzStr, or UTC if the IANA name
