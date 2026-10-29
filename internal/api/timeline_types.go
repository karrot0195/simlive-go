package api

// TimelineEntry is the trimmed projection of a Redis timeline member
// returned by the running-channels timeline API.
type TimelineEntry struct {
	Sequence        int64   `json:"sequence"`
	RunningTime     float64 `json:"running_time"`
	Duration        float64 `json:"duration"`
	TimeText        string  `json:"time_text"`
	IsAd            bool    `json:"is_ad"`
	AdType          string  `json:"ad_type,omitempty"`
	AdBreakID       uint32  `json:"ad_break_id,omitempty"`
	AdBreakStart    float64 `json:"ad_break_start,omitempty"`
	AdBreakDuration float64 `json:"ad_break_duration,omitempty"`
}

// ChannelTimeline groups a channel's upcoming timeline entries.
type ChannelTimeline struct {
	ChannelID   string          `json:"channel_id"`
	ChannelName string          `json:"channel_name"`
	Entries     []TimelineEntry `json:"entries"`
}

// TimelineResponse is the body of GET /api/channels/timeline/running.
type TimelineResponse struct {
	Channels    []ChannelTimeline `json:"channels"`
	GeneratedAt float64           `json:"generated_at"`
}