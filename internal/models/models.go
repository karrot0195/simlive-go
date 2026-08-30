package models

import (
	"time"
)

// PlayoutChannel - PostgreSQL Single Table
type PlayoutChannel struct {
	ID              string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	Name            string    `gorm:"type:varchar(255);not null" json:"name"`
	BaseName        string    `gorm:"type:varchar(255)" json:"base_name"`
	ContentTimezone string    `gorm:"type:varchar(50)" json:"content_timezone"`
	ChannelEpgID    string    `gorm:"type:varchar(36)" json:"channel_epg_id"`
	Status          string    `gorm:"type:varchar(20);default:'ACTIVE'" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SegmentItem - Structure stored in the Redis Sorted Set
type SegmentItem struct {
	Sequence    int64   `json:"sequence"`
	StoragePath string  `json:"storage_path"`
	Duration    float64 `json:"duration"`   // Used for #EXTINF
	StartTime   int64   `json:"start_time"` // Kept for display/debug compatibility (Unix timestamp)
	TimeText    string  `json:"time_text"`  // HH:mm

	// --- PTS TIMELINE ADDITIONS ---
	PtsStart   int64   `json:"pts_start"`   // Raw value from #EXT-X-PTS-RANGE
	PtsEnd     int64   `json:"pts_end"`     // Raw value from #EXT-X-PTS-RANGE
	MediaStart float64 `json:"media_start"` // PTS Start converted to seconds (PtsStart / 90000 + PtsOffset)
	MediaEnd   float64 `json:"media_end"`   // PTS End converted to seconds (PtsEnd / 90000 + PtsOffset)

	// PtsOffset is the segment's position on the channel's local timeline (seconds):
	// MediaStart - timelineBase, where timelineBase = MediaStart of the first segment.
	// The engine uses it to rewrite the .ts PTS/DTS (ffmpeg -copyts -output_ts_offset)
	// so DTS stays continuous across VOD + ads without depending on the absolute epoch.
	// nil on legacy segments that lack this field (engine falls back to MediaStart).
	PtsOffset *float64 `json:"pts_offset,omitempty"`

	// Rendition is the active quality rung this segment was prepared under
	// (e.g. "360p", "720p", "1080p"). The planner writes it on every member
	// (VOD + ads) and the engine uses it to dispatch the segment to the
	// correct per-rung directory ({TMP_DIR}/{ch}/{rend}/seg_N.ts) + media
	// playlist. Empty == legacy member without rendition tagging; the engine
	// rescues it to DefaultRendition ("360p"). Omitted from JSON when empty
	// so members written before M1 round-trip unchanged.
	Rendition string `json:"rendition,omitempty"`

	// --- SCTE-35 ad-break markers (see docs/300826-implement-scte35-cue-markers.md) ---
	//
	// IsAd marks this member as belonging to a planner-injected ad break
	// (storage path is the ads-v3 bucket). The engine uses the edge between
	// IsAd=false -> IsAd=true (CUE-OUT) and IsAd=true -> IsAd=false
	// (CUE-IN) to emit #EXT-X-DATERANGE tags with SCTE35-OUT/IN payloads.
	//
	// AdBreakID is the stable identifier for the whole break
	// (e.g. "adbreak-1725014400"); the same id is written on every ad
	// segment in the break, so the engine emits a single matching CUE-OUT
	// / CUE-IN pair. Derived from the break's start second so the id is
	// stable across planner replans.
	//
	// AdBreakStart is the break's start on the local timeline (unix
	// seconds). The engine formats it as RFC3339 UTC for the DATERANGE
	// START-DATE attribute. AdBreakDuration is the break's length in
	// seconds; used both for the DATERANGE DURATION attribute and as the
	// SCTE-35 splice_insert BreakDuration (multiplied by 90000 for the
	// 90kHz tick field).
	//
	// All four fields are omitempty so VOD members (the >95% case)
	// round-trip to JSON unchanged and pre-existing timeline members
	// remain valid without a flush.
	IsAd            bool    `json:"is_ad,omitempty"`
	AdBreakID       string  `json:"ad_break_id,omitempty"`
	AdBreakStart    float64 `json:"ad_break_start,omitempty"`
	AdBreakDuration float64 `json:"ad_break_duration,omitempty"`
}

// Response structs for ESPX APIs
type ESPXChannelListResp struct {
	Data []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		BaseName        string `json:"base_name"`
		ContentTimezone string `json:"content_timezone"`
	} `json:"data"`
}

type ESPXChannelDetailResp struct {
	ID         string `json:"id"`
	ChannelEpg struct {
		ID string `json:"id"`
	} `json:"channelEpg"`
}

type ESPXScheduleResp struct {
	Data []struct {
		ID             string `json:"id"`
		LocalDateStart string `json:"local_date_start"`
		LocalDateEnd   string `json:"local_date_end"`
		Programme      struct {
			ID       string `json:"id"`
			ShortURL string `json:"short_url"` // Stream ID
		} `json:"programme"`
	} `json:"data"`
}

type ESPXProgrammeStream struct {
	StreamID string `json:"stream_id"`
}

type ESPXScheduleItem struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Duration       string `json:"duration"`         // Needs parsing string -> int/float
	LocalDateStart string `json:"local_date_start"` // "2026-08-23 00:00:00.000"
	LocalDateEnd   string `json:"local_date_end"`
	Programme      struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		ShortURL         string `json:"short_url"` // PROGRAMME ID, DO NOT USE THIS AS THE PATH
		ProgrammeStreams []ESPXProgrammeStream
	} `json:"programme"`
}

type ESPXSchedulesResponse struct {
	Date       string             `json:"date"`
	TotalItems int                `json:"total_items"`
	Data       []ESPXScheduleItem `json:"data"`
}

type CreateChannelReq struct {
	ChannelID string `json:"channel_id"`
}
