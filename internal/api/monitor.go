package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"simlive/internal/models"
)

func monitorInterval() time.Duration {
	raw := os.Getenv("MONITOR_INTERVAL")
	if raw == "" {
		return 3 * time.Second
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return time.Duration(n) * time.Second
	}
	return 3 * time.Second
}

func (h *ChannelHandler) WatchChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	var existing models.MonitoredChannel
	if err := h.db.Where("channel_id = ?", id).First(&existing).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{"status": "watched", "channel_id": id})
		return
	} else if err != gorm.ErrRecordNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if err := h.db.Create(&models.MonitoredChannel{ChannelID: id}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to watch"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "watched", "channel_id": id})
}

func (h *ChannelHandler) ListMonitoredChannels(c *gin.Context) {
	var monitored []models.MonitoredChannel
	if err := h.db.Find(&monitored).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	ids := make([]string, 0, len(monitored))
	for _, m := range monitored {
		ids = append(ids, m.ChannelID)
	}
	c.JSON(http.StatusOK, gin.H{"channel_ids": ids})
}

func (h *ChannelHandler) UnwatchChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	if err := h.db.Where("channel_id = ?", id).Delete(&models.MonitoredChannel{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unwatch"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "unwatched", "channel_id": id})
}

func (h *ChannelHandler) StreamTimeline(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no-buffer")

	ticker := time.NewTicker(monitorInterval())
	defer ticker.Stop()

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			payload := h.buildTimelinePayload(c.Request.Context())
			b, _ := json.Marshal(payload)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(b))
			flusher.Flush()
		}
	}
}

var segmentTrackSeen = map[string]map[int64]string{}
var segmentTrackSeenMu sync.Mutex

func (h *ChannelHandler) buildTimelinePayload(ctx context.Context) map[string]interface{} {
	now := time.Now()
	nowUnix := float64(now.Unix())

	var monitored []models.MonitoredChannel
	_ = h.db.Find(&monitored).Error

	monitoredSet := map[string]bool{}
	for _, m := range monitored {
		monitoredSet[m.ChannelID] = true
	}

	var channels []models.PlayoutChannel
	_ = h.db.Where("is_active = ?", true).Order("created_at DESC").Find(&channels).Error

	respChannels := []ChannelTimeline{}
	for _, ch := range channels {
		if !monitoredSet[ch.ID] {
			continue
		}
		key := "simlive:timeline:" + ch.ID
		items, err := h.rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{
			Min: fmtFloat(nowUnix),
			Max: "+inf",
		}).Result()
		if err != nil {
			continue
		}
		entries := []TimelineEntry{}
		seenSeq := map[int64]bool{}
		for _, raw := range items {
			var seg models.SegmentItem
			if err := json.Unmarshal([]byte(raw), &seg); err != nil {
				continue
			}
			if seg.RunningTime < nowUnix {
				continue
			}
			if seenSeq[seg.Sequence] {
				continue
			}
			seenSeq[seg.Sequence] = true
			entries = append(entries, TimelineEntry{
				Sequence:        seg.Sequence,
				RunningTime:     seg.RunningTime,
				Duration:        seg.Duration,
				TimeText:        seg.TimeText,
				IsAd:            seg.IsAd,
				AdType:          seg.AdType,
				AdBreakID:       seg.AdBreakID,
				AdBreakStart:    seg.AdBreakStart,
				AdBreakDuration: seg.AdBreakDuration,
			})
		}
		respChannels = append(respChannels, ChannelTimeline{
			ChannelID:   ch.ID,
			ChannelName: ch.Name,
			Entries:     entries,
		})

		// Segment track recording is handled solely by StartSegmentTrackRecorder
		// (disk-scan based) to keep a single source of truth.
	}
	return map[string]interface{}{
		"channels":     respChannels,
		"generated_at": nowUnix,
	}
}
