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

func (e *Engine) CleanChannelState(ctx context.Context, channelID string) {
	channelDir := filepath.Join(e.tmpDir, channelID)
	if err := os.RemoveAll(channelDir); err != nil {
		log.Printf("[%s WARN] Failed to remove local directory: %v", channelID, err)
	}

	_ = os.MkdirAll(channelDir, os.ModePerm)

	// KHÔNG XÓA simlive:timeline key do Planner nạp.
	// Chỉ xóa cache HLS playlist để tái tạo lại từ đầu.
	redisHLSKey := fmt.Sprintf("simlive:hls:%s", channelID)
	e.rdb.Del(ctx, redisHLSKey)

	log.Printf("[%s ENGINE] Cleaned up local cache directory and HLS key on startup", channelID)
}

func (e *Engine) StartPlayoutLoop(ctx context.Context, channelID string) {
	e.CleanChannelState(ctx, channelID)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	channelDir := filepath.Join(e.tmpDir, channelID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().Unix()
			redisKey := fmt.Sprintf("simlive:timeline:%s", channelID)

			min := fmt.Sprintf("%d", now-120)
			maxLookahead := fmt.Sprintf("%d", now+60)

			items, err := e.rdb.ZRangeByScore(ctx, redisKey, &redis.ZRangeBy{
				Min: min,
				Max: maxLookahead,
			}).Result()

			if err != nil || len(items) == 0 {
				minScore, _ := e.rdb.ZRangeWithScores(ctx, redisKey, 0, 0).Result()
				maxScore, _ := e.rdb.ZRangeWithScores(ctx, redisKey, -1, -1).Result()

				firstScore := 0.0
				lastScore := 0.0
				if len(minScore) > 0 {
					firstScore = minScore[0].Score
				}
				if len(maxScore) > 0 {
					lastScore = maxScore[0].Score
				}

				log.Printf("[%s] No segments in window [%s, %s] | Redis Score Range: [%.0f -> %.0f] | Current Time: %d | Fallback to slate",
					channelID, min, maxLookahead, firstScore, lastScore, now)
				continue
			}

			var activeSegments []models.SegmentItem
			var m3u8Segments []models.SegmentItem
			activeMap := make(map[string]bool)

			for _, itemStr := range items {
				var seg models.SegmentItem
				if err := json.Unmarshal([]byte(itemStr), &seg); err == nil {
					// 1. Tải trước các file trong phạm vi 120s khẩn cấp
					activeSegments = append(activeSegments, seg)
					fileName := fmt.Sprintf("seg_%d.ts", seg.Sequence)
					activeMap[fileName] = true

					// 2. SLIDING WINDOW HLS: Chỉ đưa 3-5 segments gần nhất [now-20, now+10] vào playlist.
					// Đảm bảo EXT-X-MEDIA-SEQUENCE liên tục trượt và tăng tiến.
					if seg.StartTime >= (now-20) && seg.StartTime <= (now+10) {
						m3u8Segments = append(m3u8Segments, seg)
					}
				}
			}

			e.syncLocalCacheDir(channelDir, activeMap)

			var wg sync.WaitGroup
			for _, seg := range activeSegments {
				fileName := fmt.Sprintf("seg_%d.ts", seg.Sequence)
				localFilePath := filepath.Join(channelDir, fileName)

				if _, err := os.Stat(localFilePath); err == nil {
					continue
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

			e.generateHLSPlaylist(ctx, channelID, m3u8Segments)
			e.rdb.ZRemRangeByScore(ctx, redisKey, "-inf", fmt.Sprintf("%d", now-120))
		}
	}
}

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

	channelDir := filepath.Join(e.tmpDir, channelID)
	playlistPath := filepath.Join(channelDir, "playlist.m3u8")
	tmpPlaylistPath := playlistPath + ".tmp"

	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	} else {
		log.Printf("[%s ERROR] Failed writing playlist.m3u8: %v", channelID, err)
	}

	redisHLSKey := fmt.Sprintf("simlive:hls:%s", channelID)
	e.rdb.Set(ctx, redisHLSKey, playlist, 10*time.Second)
}
