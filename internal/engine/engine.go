package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

// channelState keeps the current HLS window for one channel.
type channelState struct {
	mu          sync.Mutex
	firstSeq    int64 // sequence number of the first segment in the window
	lastSeq     int64 // sequence number of the last segment in the window
	lastEndTime int64 // Unix timestamp when the last segment ends
	initialized bool
}

type Engine struct {
	rdb           *redis.Client
	rclone        *storage.RcloneService
	tmpDir        string
	workerPool    chan struct{}
	enableCleanup bool
	states        map[string]*channelState
	statesMu      sync.Mutex
}

func NewEngine(rdb *redis.Client, rclone *storage.RcloneService, maxWorkers int) *Engine {
	return &Engine{
		rdb:           rdb,
		rclone:        rclone,
		tmpDir:        utils.GetEnv("TMP_DIR", os.TempDir()),
		workerPool:    make(chan struct{}, maxWorkers),
		enableCleanup: utils.GetEnv("ENABLE_CLEAN_UP", "false") == "true",
		states:        make(map[string]*channelState),
	}
}

func (e *Engine) StartPlayoutLoop(ctx context.Context, channelID string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	channelDir := filepath.Join(e.tmpDir, channelID)
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		log.Printf("[%s ERROR] Failed to create channel directory: %v", channelID, err)
		return
	}

	// Get or create state for this channel
	e.statesMu.Lock()
	state, ok := e.states[channelID]
	if !ok {
		state = &channelState{}
		e.states[channelID] = state
	}
	e.statesMu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nowFloat := float64(time.Now().UnixNano()) / 1e9
			nowUnix := time.Now().Unix()
			redisKey := fmt.Sprintf("simlive:timeline:%s", channelID)

			// Lookahead 120 seconds for pre‑download, and 300 seconds behind for cleanup.
			min := fmt.Sprintf("%d", nowUnix-300)
			maxLookahead := fmt.Sprintf("%d", nowUnix+120)

			// Get all items in the time window (sorted by score = start time)
			items, err := e.rdb.ZRangeByScore(ctx, redisKey, &redis.ZRangeBy{
				Min: min,
				Max: maxLookahead,
			}).Result()
			if err != nil {
				log.Printf("[%s ERROR] Failed to query Redis: %v", channelID, err)
				continue
			}

			// Parse all segments (including future ones)
			var allSegs []models.SegmentItem
			for _, itemStr := range items {
				var seg models.SegmentItem
				if err := json.Unmarshal([]byte(itemStr), &seg); err == nil {
					allSegs = append(allSegs, seg)
				}
			}
			if len(allSegs) == 0 {
				log.Printf("[%s] No segments found in lookahead window, waiting...", channelID)
				continue
			}

			// Sort by sequence number (which is chronological)
			sort.Slice(allSegs, func(i, j int) bool {
				return allSegs[i].Sequence < allSegs[j].Sequence
			})

			// --- Dynamic Sliding Window Selection ---
			// Filter segments whose end times align with current playhead (keeping 6s past buffer)
			windowSize := 5
			var candidateSegs []models.SegmentItem
			for _, seg := range allSegs {
				segEndTime := float64(seg.StartTime) + seg.Duration
				if segEndTime >= (nowFloat - 6.0) {
					candidateSegs = append(candidateSegs, seg)
				}
			}

			// Fallback if near beginning/end of available lookahead buffer
			if len(candidateSegs) < windowSize {
				if len(allSegs) <= windowSize {
					candidateSegs = allSegs
				} else {
					candidateSegs = allSegs[len(allSegs)-windowSize:]
				}
			} else if len(candidateSegs) > windowSize {
				candidateSegs = candidateSegs[:windowSize]
			}

			// State maintenance & window change logging
			state.mu.Lock()
			candidateFirst := candidateSegs[0].Sequence
			candidateLast := candidateSegs[len(candidateSegs)-1].Sequence
			lastSeg := candidateSegs[len(candidateSegs)-1]
			lastEndTimeInt := lastSeg.StartTime + int64(lastSeg.Duration)

			if !state.initialized {
				state.firstSeq = candidateFirst
				state.lastSeq = candidateLast
				state.lastEndTime = lastEndTimeInt
				state.initialized = true
				log.Printf("[%s] Initialized window: firstSeq=%d, lastSeq=%d, lastEndTime=%s",
					channelID, state.firstSeq, state.lastSeq, utils.TimeNumToText(state.lastEndTime))
			} else if candidateFirst != state.firstSeq || candidateLast != state.lastSeq {
				state.firstSeq = candidateFirst
				state.lastSeq = candidateLast
				state.lastEndTime = lastEndTimeInt
				log.Printf("[%s] Slid window: firstSeq=%d, lastSeq=%d, new lastEndTime=%s",
					channelID, state.firstSeq, state.lastSeq, utils.TimeNumToText(state.lastEndTime))
			}

			playlistSegs := candidateSegs
			state.mu.Unlock()

			// --- Download all segments in the lookahead window (including future ones) ---
			// Build a map of filenames to keep in local cache
			activeMap := make(map[string]bool)
			for _, seg := range allSegs {
				fileName := fmt.Sprintf("seg_%d.ts", seg.Sequence)
				activeMap[fileName] = true
			}

			// Sync local cache: remove files not in activeMap
			e.syncLocalCacheDir(channelDir, activeMap)

			// Download missing segments (worker pool)
			var wg sync.WaitGroup
			for _, seg := range allSegs {
				fileName := fmt.Sprintf("seg_%d.ts", seg.Sequence)
				localFilePath := filepath.Join(channelDir, fileName)
				if _, err := os.Stat(localFilePath); err == nil {
					continue // already exists
				}
				wg.Add(1)
				go func(s models.SegmentItem, dest string) {
					defer wg.Done()
					e.workerPool <- struct{}{}
					defer func() { <-e.workerPool }()

					if err := e.rclone.CopyTo(s.StoragePath, dest); err != nil {
						log.Printf("[%s ERROR] Download segment %d failed: %v", channelID, s.Sequence, err)
					}
				}(seg, localFilePath)
			}
			wg.Wait()

			// --- Generate HLS playlist with the current window ---
			if len(playlistSegs) > 0 {
				e.generateHLSPlaylist(ctx, channelID, playlistSegs)
			} else {
				log.Printf("[%s] No segments in playlist window, skipping playlist generation", channelID)
			}

			// --- Cleanup old Redis entries if enabled ---
			if e.enableCleanup {
				removed, err := e.rdb.ZRemRangeByScore(ctx, redisKey, "-inf", fmt.Sprintf("%d", nowUnix-300)).Result()
				if err != nil {
					log.Printf("[%s ERROR] Failed to cleanup old Redis items: %v", channelID, err)
				} else if removed > 0 {
					log.Printf("[%s] Cleaned up %d old items from Redis (older than %s)",
						channelID, removed, utils.TimeNumToText(nowUnix-300))
				}
			}
		}
	}
}

func (e *Engine) syncLocalCacheDir(dir string, activeFiles map[string]bool) {
	if len(activeFiles) == 0 {
		return
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if filepath.Ext(name) == ".ts" && !activeFiles[name] {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func (e *Engine) generateHLSPlaylist(ctx context.Context, channelID string, segments []models.SegmentItem) {
	if len(segments) == 0 {
		return
	}

	// Sort by sequence (should already be)
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Sequence < segments[j].Sequence
	})

	// Compute target duration (max segment duration, rounded up)
	maxDuration := 10.0
	for _, seg := range segments {
		if seg.Duration > maxDuration {
			maxDuration = seg.Duration + 1
		}
	}

	playlist := "#EXTM3U\n"
	playlist += "#EXT-X-VERSION:3\n"
	playlist += fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(maxDuration))
	// Tell players to start at the live edge
	playlist += "#EXT-X-START:TIME-OFFSET=0\n"
	playlist += fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n\n", segments[0].Sequence)

	for _, seg := range segments {
		playlist += fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration)
		playlist += fmt.Sprintf("seg_%d.ts\n", seg.Sequence)
	}

	channelDir := filepath.Join(e.tmpDir, channelID)
	playlistPath := filepath.Join(channelDir, "playlist.m3u8")
	tmpPlaylistPath := playlistPath + ".tmp"

	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	} else {
		log.Printf("[%s ERROR] Failed writing playlist.m3u8: %v", channelID, err)
	}

	// Persist to Redis with 24h TTL
	redisHLSKey := fmt.Sprintf("simlive:hls:%s", channelID)
	if err := e.rdb.Set(ctx, redisHLSKey, playlist, 24*time.Hour).Err(); err != nil {
		log.Printf("[%s ERROR] Failed storing HLS playlist to Redis: %v", channelID, err)
	}
}
