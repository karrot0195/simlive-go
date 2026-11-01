package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"simlive/internal/models"
	"simlive/internal/utils"
)

// scanPreparedRenditions returns a map of segment sequence -> rendition list for
// the segments currently present on disk for a channel.
func scanPreparedRenditions(channelID string) map[int64][]string {
	tmpDir := utils.GetSettings().TMPDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	out := map[int64][]string{}
	for _, r := range []string{"360p", "720p", "1080p"} {
		dir := filepath.Join(tmpDir, channelID, r)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".ts") {
				continue
			}
			base := strings.TrimSuffix(name, ".ts")
			base = strings.TrimPrefix(base, "seg_")
			n, err := strconv.ParseInt(base, 10, 64)
			if err != nil {
				continue
			}
			out[n] = append(out[n], r)
		}
	}
	return out
}

// scanPreparedSequences returns the segment sequences currently present on disk
// for a channel (union across rendition dirs), mirroring GetPreparedSegments.
func scanPreparedSequences(channelID string) []int64 {
	renditions := scanPreparedRenditions(channelID)
	out := make([]int64, 0, len(renditions))
	for seq := range renditions {
		out = append(out, seq)
	}
	return out
}

// SegmentTrackEntry is a single persisted line in a channel's segment track log.
type SegmentTrackEntry struct {
	ChannelID  string   `json:"channel_id"`
	Sequence   int64    `json:"sequence"`
	Status     string   `json:"status"`
	Timestamp  int64    `json:"timestamp"`
	IsAd       bool     `json:"is_ad"`
	Renditions []string `json:"renditions,omitempty"`
	Message    string   `json:"message,omitempty"`
}

const segmentTrackMaxEntries = 1000

// segmentTrackPath returns the per-channel JSONL log file path.
func segmentTrackPath(channelID string) string {
	tmpDir := utils.GetSettings().TMPDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	return filepath.Join(tmpDir, "logs", channelID+".jsonl")
}

// AppendSegmentTrackEntry appends a single entry to the channel's log file,
// trimming to at most segmentTrackMaxEntries (oldest removed) when exceeded.
func AppendSegmentTrackEntry(channelID string, entry SegmentTrackEntry) error {
	path := segmentTrackPath(channelID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	b, err := json.Marshal(entry)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	f.Close()

	// Trim if over cap.
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= segmentTrackMaxEntries {
		return nil
	}
	// keep last segmentTrackMaxEntries
	keep := lines[len(lines)-segmentTrackMaxEntries:]
	out := strings.Join(keep, "\n") + "\n"
	return os.WriteFile(path, []byte(out), 0o644)
}

// ReadSegmentTrack reads the last `limit` entries (oldest-first) for a channel.
// Returns an empty slice (not an error) when the file is missing.
func ReadSegmentTrack(channelID string, limit int) ([]SegmentTrackEntry, error) {
	if limit <= 0 || limit > segmentTrackMaxEntries {
		limit = 100
		if limit > segmentTrackMaxEntries {
			limit = segmentTrackMaxEntries
		}
	}
	path := segmentTrackPath(channelID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []SegmentTrackEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []SegmentTrackEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e SegmentTrackEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		all = append(all, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	// Dedup by sequence, keeping the latest entry (most complete renditions).
	bySeq := map[int64]SegmentTrackEntry{}
	order := []int64{}
	for _, e := range all {
		if _, ok := bySeq[e.Sequence]; !ok {
			order = append(order, e.Sequence)
		}
		bySeq[e.Sequence] = e
	}
	deduped := make([]SegmentTrackEntry, 0, len(order))
	for _, s := range order {
		deduped = append(deduped, bySeq[s])
	}
	all = deduped

	if len(all) <= limit {
		if all == nil {
			all = []SegmentTrackEntry{}
		}
		return all, nil
	}
	start := len(all) - limit
	return all[start:], nil
}

// clearSegmentTrackData removes the log file and resets the in-memory seen set
// for a channel. Shared by the DELETE endpoint and channel start/stop.
func clearSegmentTrackData(channelID string) {
	path := segmentTrackPath(channelID)
	_ = os.Remove(path)
	segmentTrackSeenMu.Lock()
	delete(segmentTrackSeen, channelID)
	segmentTrackSeenMu.Unlock()
}

// ClearSegmentTrack handles DELETE /api/channels/:id/segment-track: removes the
// log file and resets the in-memory seen set so entries can be re-recorded.
func (h *ChannelHandler) ClearSegmentTrack(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	path := segmentTrackPath(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear segment track"})
		return
	}
	// Keep the seen set intact so already-recorded (currently prepared)
	// segments are not immediately re-appended after a clear; only new
	// segments get logged.
	c.Status(http.StatusNoContent)
}

// GetSegmentTrack handles GET /api/channels/:id/segment-track?limit=N.
func (h *ChannelHandler) GetSegmentTrack(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > segmentTrackMaxEntries {
		limit = segmentTrackMaxEntries
	}
	entries, err := ReadSegmentTrack(id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read segment track"})
		return
	}
	if entries == nil {
		entries = []SegmentTrackEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"channel_id": id, "entries": entries})
}

// StartSegmentTrackRecorder runs a background loop that records segment track
// entries for all active channels from their Redis timelines, independent of
// SSE clients (so accumulation happens even when the UI is closed).
func StartSegmentTrackRecorder(db *gorm.DB, rdb *redis.Client) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			var channels []models.PlayoutChannel
			if err := db.Where("is_active = ?", true).Find(&channels).Error; err != nil {
				continue
			}
			for _, ch := range channels {
				// Single source of truth: prepared segments on disk.
				renditions := scanPreparedRenditions(ch.ID)

				// Look up running times from the channel timeline in Redis so the
				// track's Time column reflects the segment's running time.
				runningTimes := map[int64]float64{}
				if rdb != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					items, err := rdb.ZRange(ctx, "simlive:timeline:"+ch.ID, 0, -1).Result()
					cancel()
					if err == nil {
						for _, raw := range items {
							var seg models.SegmentItem
							if err := json.Unmarshal([]byte(raw), &seg); err == nil {
								runningTimes[seg.Sequence] = seg.RunningTime
							}
						}
					}
				}

				segmentTrackSeenMu.Lock()
				if segmentTrackSeen[ch.ID] == nil {
					segmentTrackSeen[ch.ID] = map[int64]string{}
				}
				seen := segmentTrackSeen[ch.ID]
				segmentTrackSeenMu.Unlock()
				for seq, rends := range renditions {
					key := strings.Join(rends, ",")
					segmentTrackSeenMu.Lock()
					if prev, ok := seen[seq]; ok && prev == key {
						segmentTrackSeenMu.Unlock()
						continue
					}
					seen[seq] = key
					segmentTrackSeenMu.Unlock()
					ts := time.Now().Unix()
					if rt, ok := runningTimes[seq]; ok && rt > 0 {
						ts = int64(rt)
					}
					if err := AppendSegmentTrackEntry(ch.ID, SegmentTrackEntry{
						ChannelID:  ch.ID,
						Sequence:   seq,
						Status:     "prepared",
						Timestamp:  ts,
						Renditions: rends,
					}); err != nil {
						utils.GetLogger("api").VWarning("segment track append failed channel=%s seq=%d err=%v", ch.ID, seq, err)
					}
				}
			}
		}
	}()
}
