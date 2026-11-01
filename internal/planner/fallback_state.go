package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"simlive/internal/configs"
	"simlive/internal/utils"
)

// FallbackState records one fallback-filled target day for a channel.
// See specs/010-epg-cache-fallback/contracts/fallback-state-redis.md.
type FallbackState struct {
	TargetDate  string `json:"target_date"`
	SourceDate  string `json:"source_date"`
	SourceEpgID string `json:"source_epg_id"`
	DayStart    int64  `json:"day_start"`
	DayEnd      int64  `json:"day_end"`
	CreatedAt   int64  `json:"created_at"`
}

func fallbackKeyFor(channelID string) string {
	return fmt.Sprintf("planner:fallback:%s", channelID)
}

// fallbackDayField is the hash field for a target day (YYYY-MM-DD).
func fallbackDayField(day time.Time) string {
	return day.In(utils.LocalLocation()).Format(configs.DATE_FORMAT_YYYY_MM_DD)
}

// MarkFallbackDay records that day was covered from a retained schedule and
// refreshes the hash TTL so active operators keep seeing the label.
func (p *Planner) MarkFallbackDay(ctx context.Context, channelID string, state FallbackState) error {
	if p.rdb == nil {
		return nil
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	key := fallbackKeyFor(channelID)
	if err := p.rdb.HSet(ctx, key, state.TargetDate, string(payload)).Err(); err != nil {
		return err
	}
	ttl := time.Duration(utils.GetPlannerSettings().RedisExpiryHours) * time.Hour
	return p.rdb.Expire(ctx, key, ttl).Err()
}

// HasFallbackDay returns the recorded fallback state for day, if any.
// Missing or malformed fields are treated as "not a fallback day".
func (p *Planner) HasFallbackDay(ctx context.Context, channelID string, day time.Time) (FallbackState, bool) {
	if p.rdb == nil {
		return FallbackState{}, false
	}
	raw, err := p.rdb.HGet(ctx, fallbackKeyFor(channelID), fallbackDayField(day)).Result()
	if err != nil {
		return FallbackState{}, false
	}
	return parseFallbackState([]byte(raw), fallbackDayField(day))
}

// ClearFallbackDay removes the marker for day. Callers must only invoke this
// after real data has replaced the fallback coverage.
func (p *Planner) ClearFallbackDay(ctx context.Context, channelID string, day time.Time) error {
	if p.rdb == nil {
		return nil
	}
	return p.rdb.HDel(ctx, fallbackKeyFor(channelID), fallbackDayField(day)).Err()
}

// parseFallbackState decodes a hash value and accepts it only when it belongs
// to expectedDay and carries a usable day window.
func parseFallbackState(raw []byte, expectedDay string) (FallbackState, bool) {
	var state FallbackState
	if err := json.Unmarshal(raw, &state); err != nil {
		return FallbackState{}, false
	}
	if state.TargetDate != "" && state.TargetDate != expectedDay {
		return FallbackState{}, false
	}
	if state.DayStart <= 0 || state.DayEnd <= state.DayStart {
		return FallbackState{}, false
	}
	return state, true
}

// recoveryCutoff is the lowest timeline score that recovery may purge:
// max(now-WindowBufferSeconds, dayStart). Already-played segments below it are
// preserved so the live playlist never rewinds.
func recoveryCutoff(now, dayStart time.Time, bufferSeconds int) int64 {
	cutoff := now.Unix() - int64(bufferSeconds)
	if dayStartUnix := dayStart.Unix(); dayStartUnix > cutoff {
		cutoff = dayStartUnix
	}
	return cutoff
}
