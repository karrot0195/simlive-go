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
	StoragePath string  `json:"storage_path"`
	Duration    float64 `json:"duration"`   // Dùng cho #EXTINF
	StartTime   int64   `json:"start_time"` // Giữ lại để tương thích hiển thị/debug (Unix timestamp)
	TimeText    string  `json:"time_text"`  // Giờ phút

	// --- BỔ SUNG CHO PTS TIMELINE ---
	PtsStart   int64   `json:"pts_start"`   // Giá trị thô từ #EXT-X-PTS-RANGE
	PtsEnd     int64   `json:"pts_end"`     // Giá trị thô từ #EXT-X-PTS-RANGE
	MediaStart float64 `json:"media_start"` // PTS Start quy đổi ra giây (PtsStart / 90000 + PtsOffset)
	MediaEnd   float64 `json:"media_end"`   // PTS End quy đổi ra giây (PtsEnd / 90000 + PtsOffset)

	// PtsOffset là vị trí của segment trên timeline cục bộ của kênh (giây):
	// MediaStart - timelineBase, với timelineBase = MediaStart của segment đầu tiên.
	// Engine dùng nó để viết lại PTS/DTS của file .ts (ffmpeg -copyts -output_ts_offset)
	// sao cho DTS liên tục xuyên suốt VOD + ads mà không phụ thuộc epoch tuyệt đối.
	// nil khi segment cũ chưa có field này (engine fallback sang MediaStart).
	PtsOffset *float64 `json:"pts_offset,omitempty"`
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

type CreateChannelReq struct {
	ChannelID string `json:"channel_id"`
}
