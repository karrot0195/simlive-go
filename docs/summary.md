package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"simlive/internal/models"
	"simlive/internal/storage"
)

type Engine struct {
	rdb        *redis.Client
	rclone     *storage.RcloneService
	tmpDir     string
	workerPool chan struct{}
}

func NewEngine(rdb *redis.Client, rclone *storage.RcloneService, maxWorkers int) *Engine {
	return &Engine{
		rdb:        rdb,
		rclone:     rclone,
		tmpDir:     "/tmp/simlive",
		workerPool: make(chan struct{}, maxWorkers),
	}
}

func (e *Engine) StartPlayoutLoop(ctx context.Context, channelID string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	channelDir := filepath.Join(e.tmpDir, channelID)
	_ = os.MkdirAll(channelDir, os.ModePerm)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().Unix()
			redisKey := fmt.Sprintf("simlive:timeline:%s", channelID)

			min := fmt.Sprintf("%d", now-12)
			max := fmt.Sprintf("%d", now+30)

			// 1. Fetch current window from Redis
			items, err := e.rdb.ZRangeByScore(ctx, redisKey, &redis.ZRangeBy{
				Min: min,
				Max: max,
			}).Result()

			if err != nil || len(items) == 0 {
				log.Printf("[%s] No segments available in timeline, fallback to slate", channelID)
				continue
			}

			var activeSegments []models.SegmentItem
			activeMap := make(map[string]bool)

			for _, itemStr := range items {
				var seg models.SegmentItem
				if err := json.Unmarshal([]byte(itemStr), &seg); err == nil {
					activeSegments = append(activeSegments, seg)
					fileName := fmt.Sprintf("seg_%d.ts", seg.Sequence)
					activeMap[fileName] = true
				}
			}

			// 2. Window Differential Logic: Clean up local disk (remove passed/out-of-window files)
			e.syncLocalCacheDir(channelDir, activeMap)

			// 3. Download missing upcoming segments via Worker Pool
			var wg sync.WaitGroup
			for _, seg := range activeSegments {
				fileName := fmt.Sprintf("seg_%d.ts", seg.Sequence)
				localFilePath := filepath.Join(channelDir, fileName)

				// Skip if segment already exists locally
				if _, err := os.Stat(localFilePath); err == nil {
					continue
				}

				wg.Add(1)
				go func(s models.SegmentItem, dest string) {
					defer wg.Done()
					e.workerPool <- struct{}{}        // Acquire worker slot
					defer func() { <-e.workerPool }() // Release worker slot

					if err := e.rclone.CopyTo(s.StoragePath, dest); err != nil {
						log.Printf("[%s ERROR] Failed downloading segment %d: %v", channelID, s.Sequence, err)
					}
				}(seg, localFilePath)
			}
			wg.Wait()

			// 4. Generate M3U8 dynamic live playlist
			e.generateHLSPlaylist(ctx, channelID, activeSegments)

			// 5. Purge expired historical Redis score records
			e.rdb.ZRemRangeByScore(ctx, redisKey, "-inf", fmt.Sprintf("%d", now-60))
		}
	}
}

// syncLocalCacheDir removes local files that are no longer part of the sliding window
func (e *Engine) syncLocalCacheDir(dir string, activeFiles map[string]bool) {
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

// generateHLSPlaylist builds the live m3u8 string and caches it directly to Redis
func (e *Engine) generateHLSPlaylist(ctx context.Context, channelID string, segments []models.SegmentItem) {
	if len(segments) == 0 {
		return
	}

	maxDuration := 10.0
	for _, seg := range segments {
		if seg.Duration > maxDuration {
			maxDuration = seg.Duration + 1
		}
	}

	playlist := "#EXTM3U\n"
	playlist += "#EXT-X-VERSION:3\n"
	playlist += fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(maxDuration))
	playlist += fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n\n", segments[0].Sequence)

	for _, seg := range segments {
		playlist += fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration)
		playlist += fmt.Sprintf("seg_%d.ts\n", seg.Sequence)
	}

	redisHLSKey := fmt.Sprintf("simlive:hls:%s", channelID)
	e.rdb.Set(ctx, redisHLSKey, playlist, 10*time.Second)
}