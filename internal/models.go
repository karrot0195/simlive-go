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

// SegmentItem - Cấu trúc lưu trong Redis Sorted Set
type SegmentItem struct {
	Sequence    int64   `json:"sequence"`
	StoragePath string  `json:"storage_path"` // e.g. espx-video-lib/PCwdH9LA.../L_0/segment_000.ts
	Duration    float64 `json:"duration"`     // tính bằng giây
	StartTime   int64   `json:"start_time"`   // Unix timestamp (Giây)
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

type ESPXProgrammeStream struct {
	StreamID string `json:"stream_id"`
}

type ESPXScheduleItem struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Duration       string `json:"duration"`         // Cần parse string -> int/float
	LocalDateStart string `json:"local_date_start"` // "2026-08-23 00:00:00.000"
	LocalDateEnd   string `json:"local_date_end"`
	Programme      struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		ShortURL         string `json:"short_url"` // ID CỦA PROGRAMME, KHÔNG DÙNG CÁI NÀY LÀM PATH
		ProgrammeStreams []ESPXProgrammeStream
	} `json:"programme"`
}

type ESPXSchedulesResponse struct {
	Date       string             `json:"date"`
	TotalItems int                `json:"total_items"`
	Data       []ESPXScheduleItem `json:"data"`
}
