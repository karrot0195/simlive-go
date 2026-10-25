package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	DefaultKeepBehind            = 5
	DefaultDownloadAhead         = 5
	PlaylistWindowSize           = 5
	DefaultTimelineRetentionSecs = 600 // Keep 10 minutes of past timeline segments in Redis
)

var RENDITIONMAP = []string{
	"360p",
	"720p",
	"1080p",
}

type downloadJob struct {
	rend string
	seq  int64
	src  string
	dest string
	seg  models.SegmentItem
}

type Engine struct {
	rdb           *redis.Client
	rclone        *storage.RcloneService
	tmpDir        string
	workerPool    chan struct{}
	enableCleanup bool

	keepBehind    int
	downloadAhead int

	inFlight   map[string]bool
	inFlightMu sync.Mutex

	rendMetaCache map[string][]RenditionMeta
	rendMetaMu    sync.Mutex

	// Tracks active background syncAndPreparePerRendition runs per channel
	downloadingChannels   map[string]bool
	downloadingChannelsMu sync.Mutex
}

func NewEngine(rdb *redis.Client, rclone *storage.RcloneService, maxWorkers int) *Engine {
	if maxWorkers <= 0 {
		maxWorkers = 4
	}
	return &Engine{
		rdb:                 rdb,
		rclone:              rclone,
		tmpDir:              utils.GetSettings().TMPDir,
		workerPool:          make(chan struct{}, maxWorkers),
		enableCleanup:       utils.GetEnv("ENABLE_CLEAN_UP", "false") == "true",
		inFlight:            make(map[string]bool),
		rendMetaCache:       make(map[string][]RenditionMeta),
		downloadingChannels: make(map[string]bool),
		keepBehind:          utils.GetEnvInt("SIMLIVE_KEEP_BEHIND", DefaultKeepBehind),
		downloadAhead:       utils.GetEnvInt("SIMLIVE_DOWNLOAD_AHEAD", DefaultDownloadAhead),
	}
}

func (e *Engine) ClearChannelCache(channelID string) {
	e.rendMetaMu.Lock()
	delete(e.rendMetaCache, channelID)
	e.rendMetaMu.Unlock()
}

func (e *Engine) StartPlayoutLoop(ctx context.Context, channelID string) {
	// 500ms ticker for fast sliding window updates
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	channelDir := filepath.Join(e.tmpDir, channelID)
	logger := utils.GetLogger(fmt.Sprintf("ENGINE:%s", channelID))
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		return
	}

	for _, ren := range RENDITIONMAP {
		renDIR := filepath.Join(e.tmpDir, channelID, ren)
		if err := os.MkdirAll(renDIR, 0755); err != nil {
			logger.Error("mkdir rung dir error", "channel", channelID, "dir", renDIR, "error", err)
			return
		}
	}

	var currSeg models.SegmentItem

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nowFloat := float64(time.Now().UnixNano()) / 1e9
			nowUnix := time.Now().Unix()

			// 1. Clean up stale Redis timeline items
			e.pruneTimelineRedis(ctx, channelID, nowUnix)

			// 2. Fetch timeline
			allSegs := e.fetchTimeline(ctx, channelID, nowUnix)
			if len(allSegs) == 0 {
				logger.Warn("allSegs are empty")
				e.generateMasterPlaylist(ctx, logger, channelID, e.activeRungMeta(ctx, channelID))
				continue
			}

			candidateSegs := e.selectCandidateWindow(allSegs, nowFloat)
			if len(candidateSegs) == 0 {
				logger.Warn("candidateSegs are empty")
				e.generateMasterPlaylist(ctx, logger, channelID, e.activeRungMeta(ctx, channelID))
				continue
			}

			// skip if the same segment is already being processed
			if currSeg.Sequence == candidateSegs[0].Sequence {
				logger.Warn("same segment is already being processed", "sequence", currSeg.Sequence, "time", currSeg.TimeText)
				continue
			}

			currSeg = candidateSegs[0]

			downloadSegs := e.computeDownloadWindow(allSegs, candidateSegs)
			meta := e.activeRungMeta(ctx, channelID)

			// Execute download/prepare in background without blocking playlist generation.
			// If a previous download cycle is still in progress, skip this execution.
			e.downloadingChannelsMu.Lock()
			isBusy := e.downloadingChannels[channelID]
			if !isBusy {
				e.downloadingChannels[channelID] = true
				e.downloadingChannelsMu.Unlock()

				go func(segs []models.SegmentItem) {
					defer func() {
						e.downloadingChannelsMu.Lock()
						delete(e.downloadingChannels, channelID)
						e.downloadingChannelsMu.Unlock()
					}()
					e.syncAndPreparePerRendition(ctx, logger, channelID, segs)
				}(downloadSegs)
			} else {
				e.downloadingChannelsMu.Unlock()
			}

			// Generate playlists immediately on every tick using available .pts segments
			rendBuckets := candidateSegs[0].Renditions
			// behind := lookBehindSegment(allSegs, candidateSegs[0])
			for i, rend := range rendBuckets {
				if rend != "" {
					e.generateRenditionPlaylist(ctx, logger, channelID, RENDITIONMAP[i], candidateSegs)
				}
			}

			e.generateMasterPlaylist(ctx, logger, channelID, meta)

			liveSeg := int64(-1)
			if len(candidateSegs) > 0 {
				liveSeg = candidateSegs[0].Sequence
			}
			logger.Info("channel playout",
				"channel", channelID,
				"live_segment", liveSeg,
				"download_ahead", e.downloadAhead,
				"active_rungs", len(meta),
				"segments_in_window", len(candidateSegs))
		}
	}
}

// pruneTimelineRedis removes expired segment metadata entries from the sorted set in Redis
func (e *Engine) pruneTimelineRedis(ctx context.Context, channelID string, nowUnix int64) {
	redisKey := fmt.Sprintf("simlive:timeline:%s", channelID)
	cutoff := nowUnix - DefaultTimelineRetentionSecs
	_ = e.rdb.ZRemRangeByScore(ctx, redisKey, "-inf", fmt.Sprintf("%d", cutoff)).Err()
}

func (e *Engine) fetchTimeline(ctx context.Context, channelID string, nowUnix int64) []models.SegmentItem {
	redisKey := fmt.Sprintf("simlive:timeline:%s", channelID)
	min := fmt.Sprintf("%d", nowUnix-300)
	max := fmt.Sprintf("%d", nowUnix+120)
	items, err := e.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     redisKey,
		Start:   min,
		Stop:    max,
		ByScore: true,
	}).Result()
	if err != nil || len(items) == 0 {
		return nil
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
	return allSegs
}

func (e *Engine) selectCandidateWindow(allSegs []models.SegmentItem, nowFloat float64) []models.SegmentItem {
	playlistWindow := PlaylistWindowSize
	var currentIndex = -1
	for idx, seg := range allSegs {
		if seg.MediaStart <= nowFloat && nowFloat < seg.MediaEnd {
			currentIndex = idx
			break
		}
	}
	if currentIndex == -1 && len(allSegs) > 0 {
		if nowFloat >= allSegs[len(allSegs)-1].MediaEnd {
			if len(allSegs) >= playlistWindow {
				currentIndex = len(allSegs) - playlistWindow
			} else {
				currentIndex = 0
			}
		} else {
			for idx, seg := range allSegs {
				if seg.MediaStart >= nowFloat {
					currentIndex = idx
					break
				}
			}
			if currentIndex == -1 {
				currentIndex = 0
			}
		}
	}
	if currentIndex == -1 {
		return allSegs
	}
	end := currentIndex + playlistWindow
	if end > len(allSegs) {
		end = len(allSegs)
	}
	candidate := allSegs[currentIndex:end]
	if len(candidate) < playlistWindow {
		if len(allSegs) <= playlistWindow {
			return allSegs
		}
		return allSegs[len(allSegs)-playlistWindow:]
	}
	return candidate
}

func (e *Engine) computeDownloadWindow(allSegs, candidate []models.SegmentItem) []models.SegmentItem {
	if len(allSegs) == 0 {
		return nil
	}
	anchor := candidate[0]
	for idx, seg := range allSegs {
		if seg.Sequence == anchor.Sequence {
			start := idx - e.keepBehind
			if start < 0 {
				start = 0
			}
			end := idx + PlaylistWindowSize + e.downloadAhead - 1
			if end >= len(allSegs) {
				end = len(allSegs) - 1
			}
			if end >= start {
				return allSegs[start : end+1]
			}
			break
		}
	}
	return allSegs
}

// func lookBehindSegment(allSegs []models.SegmentItem, anchor models.SegmentItem) *models.SegmentItem {
// 	for i, seg := range allSegs {
// 		if seg.Sequence == anchor.Sequence {
// 			if i-1 >= 0 {
// 				prev := allSegs[i-1]
// 				return &prev
// 			}
// 			break
// 		}
// 	}
// 	return nil
// }

func (e *Engine) syncAndPreparePerRendition(ctx context.Context, logger *utils.Logger, channelID string, downloadSegs []models.SegmentItem) {
	channelDir := filepath.Join(e.tmpDir, channelID)

	activePerRendition := make(map[string]map[string]bool)
	for _, ren := range RENDITIONMAP {
		activePerRendition[ren] = make(map[string]bool)
	}

	var jobs []downloadJob

	for _, seg := range downloadSegs {
		rends := seg.Renditions
		if len(rends) == 0 {
			rends = []string{DefaultRendition}
		}

		for i, rend := range rends {
			localRenName := DefaultRendition
			if i < len(RENDITIONMAP) {
				localRenName = RENDITIONMAP[i]
			}

			rDir := filepath.Join(channelDir, localRenName)
			src := strings.ReplaceAll(seg.StoragePath, "{rendition}", rend)
			dest := filepath.Join(rDir, fmt.Sprintf("seg_%d.ts", seg.Sequence))

			segFileName := fmt.Sprintf("seg_%d.ts", seg.Sequence)
			activePerRendition[localRenName][segFileName] = true

			jobs = append(jobs, downloadJob{
				rend: rend,
				seq:  seg.Sequence,
				src:  src,
				dest: dest,
				seg:  seg,
			})
		}
	}

	var syncWg sync.WaitGroup
	for _, ren := range RENDITIONMAP {
		renDir := filepath.Join(channelDir, ren)
		activeSet := activePerRendition[ren]
		syncWg.Add(1)
		go func(dir string, active map[string]bool) {
			defer syncWg.Done()
			syncLocalCacheDir(dir, active)
		}(renDir, activeSet)
	}
	syncWg.Wait()

	if len(jobs) > 0 {
		e.downloadAndPrepareJobs(ctx, logger, channelDir, channelID, jobs)
	}
}

// syncLocalCacheDir cleans up unneeded .ts files, .pts markers, and dangling temporary files
func syncLocalCacheDir(dir string, activeFiles map[string]bool) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		filePath := filepath.Join(dir, name)

		base := name
		if strings.HasSuffix(name, ".pts") {
			base = strings.TrimSuffix(name, ".pts")
		}

		if strings.HasSuffix(base, ".ts") {
			key := base
			if !activeFiles[key] {
				_ = os.Remove(filePath)
			}
		} else if strings.Contains(name, ".partial") || strings.Contains(name, ".rew.tmp") || strings.Contains(name, ".splice") {
			_ = os.Remove(filePath)
		}
	}
}

func (e *Engine) activeRungMeta(ctx context.Context, channelID string) []RenditionMeta {
	if meta, err := e.loadRenditionMeta(ctx, channelID); err == nil && len(meta) > 0 {
		return sortRungsByLadder(meta)
	}
	allowed := map[string]bool{}
	for _, r := range allowedRenditionLabels() {
		allowed[r] = true
	}
	out := make([]RenditionMeta, 0, len(ActiveLadder))
	for _, m := range ActiveLadder {
		if allowed[m.Rendition] {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		out = ActiveLadder
	}
	return out
}

func (e *Engine) downloadAndPrepareJobs(parentCtx context.Context, logger *utils.Logger, dir, channelID string, jobs []downloadJob) {
	var wg sync.WaitGroup
	for _, j := range jobs {
		dest := j.dest
		src := j.src
		seg := j.seg

		e.inFlightMu.Lock()
		if e.inFlight[dest] {
			e.inFlightMu.Unlock()
			continue
		}
		e.inFlight[dest] = true
		e.inFlightMu.Unlock()

		wg.Add(1)
		go func(s models.SegmentItem, d, sr string) {
			defer wg.Done()
			defer func() {
				e.inFlightMu.Lock()
				delete(e.inFlight, d)
				e.inFlightMu.Unlock()
			}()

			e.workerPool <- struct{}{}
			defer func() { <-e.workerPool }()

			logger.VInfo("downloading %s", dest)
			if err := e.prepareSegmentFromSource(parentCtx, logger, channelID, dir, s, d, sr); err != nil {
				logger.VError("download error %s - %v", dest, err)
			} else {
				logger.VInfo("downloaded %s", dest)
			}

		}(seg, dest, src)
	}
	wg.Wait()
}

func (e *Engine) prepareSegmentFromSource(parentCtx context.Context, logger *utils.Logger, channelID string, dir string, seg models.SegmentItem, dest, src string) error {

	marker := dest + ".pts"

	if _, err := os.Stat(marker); err == nil {
		return nil
	}

	needDownload := false
	if _, err := os.Stat(dest); err != nil {
		if !os.IsNotExist(err) {
			logger.VError("[%s] Stat dest error: %v (will re-download)", filepath.Base(dir), err)
		}
		needDownload = true
	}
	if needDownload {
		if err := e.rclone.CopyTo(parentCtx, src, dest); err != nil {
			return utils.Error("download %s: %v", src, err)
		}
	}

	if _, err := os.Stat(dest); err != nil {
		return utils.Error("verify dest after download: %v", err)
	}

	if err := e.rewritePtsOffset(parentCtx, dest, marker, seg); err != nil {
		return e.markPrepared(marker)
	}

	err := e.applySliceEvents(parentCtx, seg, dest, channelID)
	if err != nil {
		return err
	}
	return nil
}

func (e *Engine) applySliceEvents(parentCtx context.Context, seg models.SegmentItem, dest, channelID string) error {
	events := seg.SliceInserts
	currentFile := dest

	logger := utils.GetLogger(fmt.Sprintf("ENGINE:%s", channelID))

	if len(events) == 0 {

		if seg.IsAd {
			tempFile := fmt.Sprintf("%s.splice.tmp", currentFile)
			err := utils.ConfigurePmtForAdsTs(parentCtx, seg, currentFile, tempFile)
			if err != nil {
				// retry once (FR-006)
				err = utils.ConfigurePmtForAdsTs(parentCtx, seg, currentFile, tempFile)
			}
			if err != nil {
				if _, statErr := os.Stat(tempFile); statErr == nil {
					_ = os.Remove(tempFile)
				}
				logger.Error("[SCTE35] event=pmt_config_retry_exhausted", "channel", channelID, "seq", seg.Sequence, "dest", dest, "error", err)
				return utils.Error("PMT configuration failed for %s - %v", dest, err)
			}

			err = os.Rename(tempFile, currentFile)
			if err != nil {
				if _, statErr := os.Stat(tempFile); statErr == nil {
					_ = os.Remove(tempFile)
				}
				return utils.Error("[SCTE35] event=rename_failed channel=%s temp=%s dest=%s error=%v", channelID, tempFile, currentFile, err)
			}
			return nil
		}

		// VOD segment: declare SCTE-35 (0x86) in PMT for consistency (FR-001/FR-002).
		tempFile := fmt.Sprintf("%s.pmt.tmp", currentFile)
		err := utils.ConfigurePmtForVodTs(parentCtx, seg, currentFile, tempFile)
		if err != nil {
			// retry once (FR-006)
			err = utils.ConfigurePmtForVodTs(parentCtx, seg, currentFile, tempFile)
		}
		if err != nil {
			if _, statErr := os.Stat(tempFile); statErr == nil {
				_ = os.Remove(tempFile)
			}
			logger.Error("[SCTE35] event=pmt_config_retry_exhausted", "channel", channelID, "seq", seg.Sequence, "dest", dest, "error", err)
			return utils.Error("PMT configuration failed for %s - %v", dest, err)
		}
		if err := os.Rename(tempFile, currentFile); err != nil {
			if _, statErr := os.Stat(tempFile); statErr == nil {
				_ = os.Remove(tempFile)
			}
			return utils.Error("[SCTE35] event=rename_failed channel=%s temp=%s dest=%s error=%v", channelID, tempFile, currentFile, err)
		}
		return nil
	}

	for i := range events {
		se := events[i]
		tempFile := fmt.Sprintf("%s.splice%d.tmp", currentFile, i)

		errr := utils.InjectSpliceEvent(parentCtx, currentFile, tempFile, se)
		if errr != nil {
			// retry once (FR-006)
			if _, err := os.Stat(tempFile); err == nil {
				_ = os.Remove(tempFile)
			}
			errr = utils.InjectSpliceEvent(parentCtx, currentFile, tempFile, se)
		}
		if errr != nil {
			if _, err := os.Stat(tempFile); err == nil {
				_ = os.Remove(tempFile)
			}
			logger.Error("[SCTE35] event=splice_injection_failed", "channel", channelID, "seq", seg.Sequence, "dest", dest, "error", errr)
			return utils.Error("[SCTE35] event=splice_injection_failed file=%s - %s", dest, errr)
		}

		err := os.Rename(tempFile, currentFile)
		if err != nil {
			return utils.Error("Failed to move %s to %s: %v", tempFile, currentFile, err)
		}
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

func (e *Engine) rewritePtsOffset(parentCtx context.Context, dest, marker string, seg models.SegmentItem) error {
	offset := seg.PtsOffset
	tmp := dest + ".rew.tmp"

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	tsOffset := strconv.FormatFloat(offset, 'f', 6, 64)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-v", "error",
		"-i", dest,
		"-c", "copy",
		// "-copyts",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-output_ts_offset", tsOffset,
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

func (e *Engine) generateRenditionPlaylist(ctx context.Context, logger *utils.Logger, channelID, rendition string, segments []models.SegmentItem) {
	if len(segments) == 0 {
		return
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Sequence < segments[j].Sequence
	})

	rDir := rungDir(e.tmpDir, channelID, rendition)
	var available []models.SegmentItem
	for _, seg := range segments {
		marker := filepath.Join(rDir, fmt.Sprintf("seg_%d.ts.pts", seg.Sequence))
		if _, err := os.Stat(marker); err == nil {
			available = append(available, seg)
		}
	}
	if len(available) == 0 {
		return
	}
	segments = available

	maxDuration := 0.0
	for _, seg := range segments {
		if seg.Duration > maxDuration {
			maxDuration = seg.Duration
		}
	}
	if maxDuration < 1 {
		maxDuration = 1
	}
	targetDuration := int(math.Ceil(maxDuration))

	playlist, err := buildMediaPlaylistBody(segments, targetDuration)
	if err != nil {
		logger.Error("buildMediaPlaylistBody failed", "channel", channelID, "rung", rendition, "error", err)
		return
	}

	playlistPath := filepath.Join(rDir, "playlist.m3u8")
	tmpPlaylistPath := playlistPath + ".tmp"

	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	}

	_ = e.rdb.Set(ctx, renditionMediaRedisKey(channelID, rendition), playlist, 24*time.Hour).Err()
}

func (e *Engine) generateMasterPlaylist(ctx context.Context, logger *utils.Logger, channelID string, meta []RenditionMeta) {
	if len(meta) == 0 {
		meta = []RenditionMeta{{Rendition: DefaultRendition}}
	}

	sorted := sortRungsByLadder(meta)
	uris := pickRenditionURIs(sorted)
	playlist := buildMasterPlaylist(sorted, uris)

	chRoot := channelDir(e.tmpDir, channelID)
	if err := os.MkdirAll(chRoot, 0755); err != nil {
		logger.Error("mkdir channel dir error", "channel", channelID, "error", err)
		return
	}
	playlistPath := filepath.Join(chRoot, "playlist.m3u8")
	tmpPlaylistPath := playlistPath + ".tmp"
	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	}
	_ = e.rdb.Set(ctx, masterRedisKey(channelID), playlist, 24*time.Hour).Err()
}

func (e *Engine) loadRenditionMeta(ctx context.Context, channelID string) ([]RenditionMeta, error) {
	e.rendMetaMu.Lock()
	if cached, ok := e.rendMetaCache[channelID]; ok {
		e.rendMetaMu.Unlock()
		return cached, nil
	}
	e.rendMetaMu.Unlock()

	raw, err := e.rdb.Get(ctx, renditionMetadataRedisKey(channelID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var meta []RenditionMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, err
	}

	e.rendMetaMu.Lock()
	e.rendMetaCache[channelID] = meta
	e.rendMetaMu.Unlock()
	return meta, nil
}
