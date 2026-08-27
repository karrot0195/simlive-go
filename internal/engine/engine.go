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

	// Chống download/rewrite trùng lặp cho cùng một file giữa các tick
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

			// Query lookahead window từ Redis (score là MediaStart)
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

			windowSize := 5
			var candidateSegs []models.SegmentItem
			currentIndex := -1

			// 1. Tìm segment chứa thời gian hiện tại
			for idx, seg := range allSegs {
				if seg.MediaStart <= nowFloat && nowFloat < seg.MediaEnd {
					currentIndex = idx
					break
				}
			}

			// 2. Cơ chế tự động bắt nhịp (Auto-Catchup) nếu nowFloat bị lệch so với Redis
			if currentIndex == -1 && len(allSegs) > 0 {
				if nowFloat >= allSegs[len(allSegs)-1].MediaEnd {
					// Nếu thời gian thực đã vượt quá segment cuối cùng -> lấy nhóm segment cuối để chạy
					if len(allSegs) >= windowSize {
						currentIndex = len(allSegs) - windowSize
					} else {
						currentIndex = 0
					}
				} else {
					// Nếu timeline ở tương lai hoặc chưa khớp -> chọn segment có MediaStart gần nhất với hiện tại
					for idx, seg := range allSegs {
						if seg.MediaStart >= nowFloat {
							currentIndex = idx
							break
						}
					}
					if currentIndex == -1 {
						currentIndex = 0 // Fallback về segment đầu tiên nếu không khớp
					}
				}
			}

			// Lấy cửa sổ segment
			if currentIndex != -1 {
				endIndex := currentIndex + windowSize
				if endIndex > len(allSegs) {
					endIndex = len(allSegs)
				}
				candidateSegs = allSegs[currentIndex:endIndex]
			}

			// Fallback an toàn nếu danh sách quá ít
			if len(candidateSegs) < windowSize {
				if len(allSegs) <= windowSize {
					candidateSegs = allSegs
				} else {
					candidateSegs = allSegs[len(allSegs)-windowSize:]
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

			// Đồng bộ cache thư mục cục bộ và tải các file .ts còn thiếu (Không dùng wg.Wait() chặn luồng chính ghi m3u8)
			activeMap := make(map[string]bool)
			for _, seg := range allSegs {
				activeMap[fmt.Sprintf("seg_%d.ts", seg.Sequence)] = true
			}
			e.syncLocalCacheDir(channelDir, activeMap)

			// Download + rewrite PTS/DTS nền, không chặn luồng ghi playlist
			// (chạy goroutine để tick 1s không bị nghẽn bởi download -> window
			// trượt đều +1 segment, tránh playlist nhảy 2-3 index một lúc)
			go e.downloadAndPrepareSegments(channelDir, channelID, allSegs)

			// Ghi playlist ngay lập tức không bị nghẽn bởi download
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
		// Xóa cả file .ts và marker .pts của segment không còn trong window
		base := name
		if strings.HasSuffix(name, ".pts") {
			base = strings.TrimSuffix(name, ".pts")
		}
		if filepath.Ext(base) == ".ts" && !activeFiles[base] {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// downloadAndPrepareSegments tải các segment còn thiếu và viết lại PTS/DTS
// (ffmpeg -copyts -output_ts_offset) để DTS liên tục xuyên suốt VOD + ads.
// Mỗi file chỉ được xử lý một lần (in-flight dedupe + marker .pts).
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

// prepareSegment đảm bảo segment đã được tải về và đã được viết lại PTS/DTS
// (có marker .pts). Idempotent: bỏ qua nếu marker đã tồn tại.
func (e *Engine) prepareSegment(channelDir string, seg models.SegmentItem) error {
	dest := filepath.Join(channelDir, fmt.Sprintf("seg_%d.ts", seg.Sequence))
	marker := dest + ".pts"

	if _, err := os.Stat(marker); err == nil {
		return nil // đã sẵn sàng
	}

	if _, err := os.Stat(dest); err != nil {
		if err := e.rclone.CopyTo(seg.StoragePath, dest); err != nil {
			return fmt.Errorf("download %s: %w", seg.StoragePath, err)
		}
	}

	if err := e.rewritePtsOffset(dest, marker, seg); err != nil {
		// Giữ file gốc (hành vi cũ) nhưng vẫn đánh dấu để không kẹt window
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

// rewritePtsOffset viết lại toàn bộ timestamp (PCR/PTS/DTS) của file .ts bằng
// ffmpeg remux (-c copy, không re-encode) sao cho video packet đầu tiên có DTS
// đúng bằng PtsOffset (timeline cục bộ của kênh, giây). Nhờ vậy DTS liên tục
// giữa các segment VOD/ads kề nhau -> Chrome không còn lỗi "Parsed buffers not
// in DTS sequence" khi chuyển normal -> ads.
func (e *Engine) rewritePtsOffset(dest, marker string, seg models.SegmentItem) error {
	// Đích cho video packet đầu tiên: ưu tiên timeline cục bộ (PtsOffset);
	// fallback sang MediaStart (epoch) cho dữ liệu cũ chưa có field này.
	target := seg.MediaStart
	if seg.PtsOffset != nil {
		target = *seg.PtsOffset
	}

	firstDTS, err := probeFirstVideoDTS(dest)
	var offset float64
	if err != nil {
		// Fallback: neo theo PtsStart (video PTS đầu tiên từ tag #EXT-X-PTS-RANGE).
		// DTS đầu ra = target - delta (delta không đổi trong một encode) nên vẫn liên tục.
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

// probeFirstVideoDTS đọc DTS (giây) của video packet đầu tiên trong file .ts.
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
	// csv có thể in thêm dấu phẩy cuối dòng ("373.772011,\n") -> cắt bỏ
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

	// Chỉ quảng bá các segment đã sẵn sàng (đã tải + đã rewrite PTS, có marker .pts)
	// để player không request phải file đang tải dở / chưa có -> tránh 404 gây buffer.
	channelDir := filepath.Join(e.tmpDir, channelID)
	var available []models.SegmentItem
	for _, seg := range segments {
		marker := filepath.Join(channelDir, fmt.Sprintf("seg_%d.ts.pts", seg.Sequence))
		if _, err := os.Stat(marker); err == nil {
			available = append(available, seg)
		}
	}
	if len(available) == 0 {
		return // giữ playlist cũ cho tới khi window sẵn sàng
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
