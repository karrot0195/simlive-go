package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

const (
	// DefaultKeepBehind is the number of segments retained *before* the
	// playlist window so backwards seek/continuity has buffer.
	DefaultKeepBehind = 5
	// DefaultDownloadAhead is the number of segments pre-fetched *after* the
	// playlist window so the next playhead step never stalls on a download.
	DefaultDownloadAhead = 5
	// PlaylistWindowSize is the number of segments advertised to the player.
	PlaylistWindowSize = 5
)

type channelState struct {
	mu          sync.Mutex
	firstSeq    int64
	lastSeq     int64
	lastEndTime int64
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

	// Active on-disk window controls (SIMLIVE_KEEP_BEHIND, SIMLIVE_DOWNLOAD_AHEAD).
	// Total on-disk files per channel <= keepBehind + PlaylistWindowSize + downloadAhead.
	keepBehind    int
	downloadAhead int

	// Prevents duplicate download/rewrite of the same file across ticks
	inFlight   map[string]bool
	inFlightMu sync.Mutex
}

func NewEngine(rdb *redis.Client, rclone *storage.RcloneService, maxWorkers int) *Engine {
	return &Engine{
		rdb:           rdb,
		rclone:        rclone,
		tmpDir:        utils.GetEnv("TMP_DIR", os.TempDir()),
		workerPool:    make(chan struct{}, maxWorkers),
		enableCleanup: utils.GetEnv("ENABLE_CLEAN_UP", "false") == "true",
		states:        make(map[string]*channelState),
		inFlight:      make(map[string]bool),
		keepBehind:    utils.GetEnvInt("SIMLIVE_KEEP_BEHIND", DefaultKeepBehind),
		downloadAhead: utils.GetEnvInt("SIMLIVE_DOWNLOAD_AHEAD", DefaultDownloadAhead),
	}
}

func (e *Engine) StartPlayoutLoop(ctx context.Context, channelID string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	channelDir := filepath.Join(e.tmpDir, channelID)
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		return
	}

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

			// Query the lookahead window from Redis (score = MediaStart)
			min := fmt.Sprintf("%d", nowUnix-300)
			maxLookahead := fmt.Sprintf("%d", nowUnix+120)

			items, err := e.rdb.ZRangeByScore(ctx, redisKey, &redis.ZRangeBy{
				Min: min,
				Max: maxLookahead,
			}).Result()
			if err != nil || len(items) == 0 {
				continue
			}

			var allSegs []models.SegmentItem
			for _, itemStr := range items {
				var seg models.SegmentItem
				if err := json.Unmarshal([]byte(itemStr), &seg); err == nil {
					allSegs = append(allSegs, seg)
				}
			}

			sort.Slice(allSegs, func(i, j int) bool {
				return allSegs[i].Sequence < allSegs[j].Sequence
			})

			playlistWindow := PlaylistWindowSize
			var candidateSegs []models.SegmentItem
			currentIndex := -1

			// 1. Find the segment containing the current time
			for idx, seg := range allSegs {
				if seg.MediaStart <= nowFloat && nowFloat < seg.MediaEnd {
					currentIndex = idx
					break
				}
			}

			// 2. Auto-Catchup if nowFloat drifts away from the Redis timeline
			if currentIndex == -1 && len(allSegs) > 0 {
				if nowFloat >= allSegs[len(allSegs)-1].MediaEnd {
					// If real time passed the last segment -> play the final window
					if len(allSegs) >= playlistWindow {
						currentIndex = len(allSegs) - playlistWindow
					} else {
						currentIndex = 0
					}
				} else {
					// If the timeline is in the future or out of sync -> pick the segment whose MediaStart is closest to now
					for idx, seg := range allSegs {
						if seg.MediaStart >= nowFloat {
							currentIndex = idx
							break
						}
					}
					if currentIndex == -1 {
						currentIndex = 0 // Fall back to the first segment if nothing matches
					}
				}
			}

			// Get the segment window
			if currentIndex != -1 {
				endIndex := currentIndex + playlistWindow
				if endIndex > len(allSegs) {
					endIndex = len(allSegs)
				}
				candidateSegs = allSegs[currentIndex:endIndex]
			}

			// Safe fallback when the list is too short
			if len(candidateSegs) < playlistWindow {
				if len(allSegs) <= playlistWindow {
					candidateSegs = allSegs
				} else {
					candidateSegs = allSegs[len(allSegs)-playlistWindow:]
				}
			}

			if len(candidateSegs) == 0 {
				continue
			}

			// State maintenance
			state.mu.Lock()
			candidateFirst := candidateSegs[0].Sequence
			candidateLast := candidateSegs[len(candidateSegs)-1].Sequence
			lastSeg := candidateSegs[len(candidateSegs)-1]
			lastEndTimeInt := int64(lastSeg.MediaEnd)

			if !state.initialized {
				state.firstSeq = candidateFirst
				state.lastSeq = candidateLast
				state.lastEndTime = lastEndTimeInt
				state.initialized = true
				log.Printf("[%s] Initialized PTS window: firstSeq=%d, lastSeq=%d", channelID, state.firstSeq, state.lastSeq)
			} else if candidateFirst != state.firstSeq || candidateLast != state.lastSeq {
				state.firstSeq = candidateFirst
				state.lastSeq = candidateLast
				state.lastEndTime = lastEndTimeInt
			}
			state.mu.Unlock()

			// Compute the bounded active on-disk window:
			// [keepBehind before currentIndex] + [playlistWindow] + [downloadAhead after]
			// Total <= keepBehind + PlaylistWindowSize + downloadAhead (default 15).
			// currentIndex may be -1 here in edge cases (empty allSegs) -- guarded by the
			// `len(candidateSegs) == 0` continue above, but be defensive anyway.
			downloadSegs := allSegs
			if currentIndex >= 0 {
				startIdx := currentIndex - e.keepBehind
				if startIdx < 0 {
					startIdx = 0
				}
				endIdx := currentIndex + playlistWindow + e.downloadAhead - 1
				if endIdx >= len(allSegs) {
					endIdx = len(allSegs) - 1
				}
				if endIdx >= startIdx {
					downloadSegs = allSegs[startIdx : endIdx+1]
				}
			}

			// Sync the local cache dir and download missing .ts files (no wg.Wait() so the main tick never blocks playlist writes)
			activeMap := make(map[string]bool)
			for _, seg := range downloadSegs {
				activeMap[fmt.Sprintf("seg_%d.ts", seg.Sequence)] = true
			}
			e.syncLocalCacheDir(channelDir, activeMap)

			// Download + rewrite PTS/DTS in the background so playlist writes are never blocked
			// (goroutines keep the 1s tick from stalling on downloads -> the window
			// slides exactly +1 segment, avoiding 2-3 index jumps in the playlist)
			go e.downloadAndPrepareSegments(channelDir, channelID, downloadSegs)

			// Write the playlist immediately, never blocked by downloads
			e.generateHLSPlaylist(ctx, channelID, candidateSegs)
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
		// Remove both the .ts file and the .pts marker of segments no longer in the window
		base := name
		if strings.HasSuffix(name, ".pts") {
			base = strings.TrimSuffix(name, ".pts")
		}
		if filepath.Ext(base) == ".ts" && !activeFiles[base] {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// downloadAndPrepareSegments downloads missing segments and rewrites PTS/DTS
// (ffmpeg -copyts -output_ts_offset) so DTS stays continuous across VOD + ads.
// Each file is processed only once (in-flight dedupe + .pts marker).
func (e *Engine) downloadAndPrepareSegments(channelDir, channelID string, segs []models.SegmentItem) {
	var wg sync.WaitGroup
	for _, seg := range segs {
		dest := filepath.Join(channelDir, fmt.Sprintf("seg_%d.ts", seg.Sequence))

		e.inFlightMu.Lock()
		if e.inFlight[dest] {
			e.inFlightMu.Unlock()
			continue
		}
		e.inFlight[dest] = true
		e.inFlightMu.Unlock()

		wg.Add(1)
		go func(s models.SegmentItem, d string) {
			defer wg.Done()
			defer func() {
				e.inFlightMu.Lock()
				delete(e.inFlight, d)
				e.inFlightMu.Unlock()
			}()
			e.workerPool <- struct{}{}
			defer func() { <-e.workerPool }()
			if err := e.prepareSegment(channelDir, s); err != nil {
				log.Printf("[%s] prepare segment %d failed: %v", channelID, s.Sequence, err)
			}
		}(seg, dest)
	}
	wg.Wait()
}

// prepareSegment ensures the segment is downloaded and has its PTS/DTS rewritten
// (marked by the .pts marker). Idempotent: skips when the marker already exists.
func (e *Engine) prepareSegment(channelDir string, seg models.SegmentItem) error {
	dest := filepath.Join(channelDir, fmt.Sprintf("seg_%d.ts", seg.Sequence))
	marker := dest + ".pts"

	if _, err := os.Stat(marker); err == nil {
		return nil // already prepared
	}

	if _, err := os.Stat(dest); err != nil {
		if err := e.rclone.CopyTo(seg.StoragePath, dest); err != nil {
			return fmt.Errorf("download %s: %w", seg.StoragePath, err)
		}
	}

	if err := e.rewritePtsOffset(dest, marker, seg); err != nil {
		// Keep the raw file (legacy behavior) but still mark it so the window never stalls
		log.Printf("[%s] PTS rewrite failed for %s: %v", filepath.Base(channelDir), filepath.Base(dest), err)
		return e.markPrepared(marker)
	}
	return nil
}

func (e *Engine) markPrepared(marker string) error {
	f, err := os.Create(marker)
	if err != nil {
		return err
	}
	return f.Close()
}

// rewritePtsOffset rewrites all timestamps (PCR/PTS/DTS) of the .ts file via an
// ffmpeg remux (-c copy, no re-encode) so the first video packet has DTS equal
// to PtsOffset (the channel's local timeline, in seconds). This keeps DTS
// continuous across adjacent VOD/ads segments -> Chrome no longer throws
// "Parsed buffers not in DTS sequence" when switching normal -> ads.
func (e *Engine) rewritePtsOffset(dest, marker string, seg models.SegmentItem) error {
	// Target for the first video packet: prefer the local timeline (PtsOffset);
	// fall back to MediaStart (epoch) for legacy data that lacks this field.
	target := seg.MediaStart
	if seg.PtsOffset != nil {
		target = *seg.PtsOffset
	}

	firstDTS, err := probeFirstVideoDTS(dest)
	var offset float64
	if err != nil {
		// Fallback: anchor on PtsStart (first video PTS from the #EXT-X-PTS-RANGE tag).
		// Output DTS = target - delta (delta is constant within one encode) so it stays continuous.
		if seg.PtsStart <= 0 {
			return fmt.Errorf("probe first video DTS: %w", err)
		}
		offset = target - float64(seg.PtsStart)/90000.0
	} else {
		offset = target - firstDTS
	}

	tmp := dest + ".rew.tmp"

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-v", "error",
		"-i", dest,
		"-c", "copy",
		"-copyts",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-output_ts_offset", strconv.FormatFloat(offset, 'f', 6, 64),
		"-f", "mpegts",
		tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(out)))
	}

	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return e.markPrepared(marker)
}

// probeFirstVideoDTS reads the DTS (seconds) of the first video packet in the .ts file.
func probeFirstVideoDTS(path string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "packet=dts_time",
		"-of", "csv=p=0",
		"-read_intervals", "%+#1",
		path)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	// The csv may append a trailing comma ("373.772011,\n") -> strip it
	v, err := strconv.ParseFloat(strings.TrimRight(strings.TrimSpace(string(out)), ","), 64)
	if err != nil {
		return 0, fmt.Errorf("parse dts %q: %w", out, err)
	}
	return v, nil
}

func (e *Engine) generateHLSPlaylist(ctx context.Context, channelID string, segments []models.SegmentItem) {
	if len(segments) == 0 {
		return
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Sequence < segments[j].Sequence
	})

	// Only advertise prepared segments (downloaded + PTS-rewritten, with the .pts marker)
	// so players never request files still being written / missing -> avoids 404 buffering.
	channelDir := filepath.Join(e.tmpDir, channelID)
	var available []models.SegmentItem
	for _, seg := range segments {
		marker := filepath.Join(channelDir, fmt.Sprintf("seg_%d.ts.pts", seg.Sequence))
		if _, err := os.Stat(marker); err == nil {
			available = append(available, seg)
		}
	}
	if len(available) == 0 {
		return // keep the previous playlist until the window is ready
	}
	segments = available

	maxDuration := 10.0
	for _, seg := range segments {
		if seg.Duration > maxDuration {
			maxDuration = seg.Duration + 1
		}
	}

	playlist := "#EXTM3U\n"
	playlist += "#EXT-X-VERSION:3\n"
	playlist += fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(maxDuration))
	playlist += "#EXT-X-START:TIME-OFFSET=0\n"
	playlist += fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n\n", segments[0].Sequence)

	for _, seg := range segments {
		playlist += fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration)
		playlist += fmt.Sprintf("seg_%d.ts\n", seg.Sequence)
	}

	playlistPath := filepath.Join(channelDir, "playlist.m3u8")
	tmpPlaylistPath := playlistPath + ".tmp"

	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	}

	_ = e.rdb.Set(ctx, fmt.Sprintf("simlive:hls:%s", channelID), playlist, 24*time.Hour).Err()
}
