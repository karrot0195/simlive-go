package models

import "time"

type MonitoredChannel struct {
	ChannelID string    `gorm:"primaryKey;column:channel_id" json:"channel_id"`
	CreatedAt time.Time `json:"created_at"`
}