package models

import (
	"time"
)

type PlayoutChannel struct {
	ID              string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	Name            string    `gorm:"type:varchar(255)" json:"name"`
	BaseName        string    `gorm:"type:varchar(255)" json:"base_name"`
	ContentTimezone string    `gorm:"type:varchar(50)" json:"content_timezone"`
	ChannelEpgID    string    `gorm:"type:varchar(36)" json:"channel_epg_id"`
	IsActive        bool      `gorm:"default:true" json:"is_active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// User Request Payload
type CreateChannelReq struct {
	ChannelID string `json:"channel_id" binding:"required"`
	IsActive  bool   `json:"is_active"`
}

// SegmentItem - Cấu trúc lưu trong Redis Sorted Set
type SegmentItem struct {
	Sequence    int64   `json:"sequence"`
	StoragePath string  `json:"storage_path"` // e.g. espx-video-lib/PCwdH9LA.../L_0/segment_000.ts
	Duration    float64 `json:"duration"`     // tính bằng giây
	StartTime   int64   `json:"start_time"`   // Unix timestamp (Giây)
	TimeText    string  `json:"time_text"`    // e.g. 01:23:45
}

// Response structs cho ESPX APIs
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
