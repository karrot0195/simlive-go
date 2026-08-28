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
