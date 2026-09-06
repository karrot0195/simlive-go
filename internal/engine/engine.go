package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

var RENDITIONMAP = []string{
	"360p",
	"720p",
	"1080p",
}

// downloadJob is one rendition-specific download/rewrite task. A single
// SegmentItem with Renditions=[360p,720p] fans out into two jobs (one
// per rung) so each rung gets its own source path and its own on-disk
// seg_{seq}_{rend}.ts file.
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
	states        map[string]*channelState
	statesMu      sync.Mutex

	// Active on-disk window controls (SIMLIVE_KEEP_BEHIND, SIMLIVE_DOWNLOAD_AHEAD).
	// Total on-disk files per channel <= keepBehind + PlaylistWindowSize + downloadAhead.
	keepBehind    int
	downloadAhead int

	// Prevents duplicate download/rewrite of the same file across ticks
	inFlight   map[string]bool
	inFlightMu sync.Mutex

	// M1: cache of per-channel rung metadata (simlive:renditions:{ch}) loaded
	// from Redis. Populated lazily by loadRenditionMeta and used by
	// generateMasterPlaylist to render STREAM-INF lines without re-reading
	// Redis on every tick. ProcessEPGToTimeline also re-reads it when the
	// cache is cold, so a brief Redis blip still yields a usable master.
	rendMetaCache map[string][]RenditionMeta
	rendMetaMu    sync.Mutex
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
		rendMetaCache: make(map[string][]RenditionMeta),
		keepBehind:    utils.GetEnvInt("SIMLIVE_KEEP_BEHIND", DefaultKeepBehind),
		downloadAhead: utils.GetEnvInt("SIMLIVE_DOWNLOAD_AHEAD", DefaultDownloadAhead),
	}
}

func (e *Engine) StartPlayoutLoop(ctx context.Context, channelID string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	channelDir := filepath.Join(e.tmpDir, channelID)
	logger := utils.GetLogger(fmt.Sprintf("PLANNER:%s", channelID))
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		return
	}

	for _, ren := range RENDITIONMAP {
		renDIR := filepath.Join(e.tmpDir, channelID, ren)

		if err := os.MkdirAll(renDIR, 0755); err != nil {
			log.Printf("[%s] mkdir rung dir %s error: %v", channelID, renDIR, err)
			return
		}
	}

	// M1: per-rung channelState entries (keyed "{channelID}:{rend}") are
	// created lazily inside the tick loop. The legacy channel-only state is
	// no longer touched (concurrent rungs would race on it).

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nowFloat := float64(time.Now().UnixNano()) / 1e9
			nowUnix := time.Now().Unix()

			// M3: read a single shared timeline ZSET (simlive:timeline:{ch}).
			// Each SegmentItem carries its own Renditions list; the engine
			// pulls one shared .ts per sequence and renders one media
			// playlist per rendition that appears in the window.
			allSegs := e.fetchTimeline(ctx, channelID, nowUnix)
			if len(allSegs) == 0 {
				// Still emit a master so player reloads never 404.
				logger.Warn("allSegs are empty")
				e.generateMasterPlaylist(ctx, channelID, e.activeRungMeta(ctx, channelID))
				continue
			}

			candidateSegs := e.selectCandidateWindow(allSegs, nowFloat)
			if len(candidateSegs) == 0 {
				logger.Warn("candidateSegs are empty")
				e.generateMasterPlaylist(ctx, channelID, e.activeRungMeta(ctx, channelID))
				continue
			}

			downloadSegs := e.computeDownloadWindow(allSegs, candidateSegs)
			meta := e.activeRungMeta(ctx, channelID)

			rendBuckets := downloadSegs[0].Renditions
			// rendBuckets := groupSegsByRendition(candidateSegs)
			e.syncAndPreparePerRendition(ctx, channelID, downloadSegs)
			for i, rend := range rendBuckets {
				if rend != "" {
					e.generateRenditionPlaylist(ctx, channelID, RENDITIONMAP[i], downloadSegs)
				}
			}

			// Master uses the union of renditions present + the static ladder
			// so hls.js always sees a valid variant list.
			e.generateMasterPlaylist(ctx, channelID, meta)
		}
	}
}

// fetchTimeline reads the single shared ZSET (simlive:timeline:{ch}) and
// returns the segments sorted by sequence. M3: replaces per-rung ZSETs.
func (e *Engine) fetchTimeline(ctx context.Context, channelID string, nowUnix int64) []models.SegmentItem {
	redisKey := fmt.Sprintf("simlive:timeline:%s", channelID)
	min := fmt.Sprintf("%d", nowUnix-300)
	max := fmt.Sprintf("%d", nowUnix+120)
	items, err := e.rdb.ZRangeByScore(ctx, redisKey, &redis.ZRangeBy{
		Min: min,
		Max: max,
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

// selectCandidateWindow picks the playlist window (PlaylistWindowSize
// segments) centered on `now`. Auto-catchup if `now` drifted past the end.
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

// computeDownloadWindow returns the bounded on-disk set: [current-keepBehind,
// current+window+downloadAhead]. Falls back to allSegs if no current index.
func (e *Engine) computeDownloadWindow(allSegs, candidate []models.SegmentItem) []models.SegmentItem {
	if len(allSegs) == 0 {
		return nil
	}
	// Find current index from the candidate window anchor.
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

func (e *Engine) syncAndPreparePerRendition(ctx context.Context, channelID string, downloadSegs []models.SegmentItem) {
	channelDir := filepath.Join(e.tmpDir, channelID)

	// Track active files per rendition directory for clean up
	activePerRendition := make(map[string]map[string]bool)
	for _, ren := range RENDITIONMAP {
		activePerRendition[ren] = make(map[string]bool)
	}

	var jobs []downloadJob

	// 1. Collect all jobs and mark active files across all segments and renditions
	for _, seg := range downloadSegs {
		rends := seg.Renditions
		if len(rends) == 0 {
			rends = []string{DefaultRendition}
		}

		for i, rend := range rends {
			// Map index safely to RENDITIONMAP or fallback
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

	// 2. Perform cache cleanup in parallel for all rendition directories
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

	// 3. Batch process all rendition download/rewrite jobs concurrently across worker pool
	if len(jobs) > 0 {
		e.downloadAndPrepareJobs(ctx, channelDir, channelID, jobs)
	}
}

// syncLocalCacheDir removes any .ts/.pts file whose base name is not in the
// active set (the bounded 15-segment window computed in step 4). Runs every
// tick, so leftover files from a previous run are evicted on the first tick
// after startup; no separate startup cleanup is required.
//
// Free function (not a method) so tests can call it without constructing an
// Engine -- the GC logic only depends on the dir + the active set, not on
// any Engine state.
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

// activeRungMeta returns the ladder to advertise for the channel. Tries
// the planner-written per-channel metadata first; on any failure (cold
// start, transient Redis blip) falls back to the hard-coded ActiveLadder
// so the master still lists 3 rungs and the player gets a valid response.
func (e *Engine) activeRungMeta(ctx context.Context, channelID string) []RenditionMeta {
	if meta, err := e.loadRenditionMeta(ctx, channelID); err == nil && len(meta) > 0 {
		return sortRungsByLadder(meta)
	}
	return ActiveLadder
}

// downloadAndPrepareJobs downloads each (rendition, source, dest) tuple and
// rewrites PTS/DTS (ffmpeg -copyts -output_ts_offset) so DTS stays
// continuous across VOD + ads. M3.1: a single SegmentItem can fan out to
// several jobs (one per rendition in its Renditions list); each job has
// its own source path (StoragePath with {rendition} substituted) and
// its own dest file (seg_{seq}_{rend}.ts). Dedupe is keyed on the
// dest path so the same job never runs twice in the same tick.
func (e *Engine) downloadAndPrepareJobs(ctx context.Context, dir, channelID string, jobs []downloadJob) {
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
			if err := e.prepareSegmentFromSource(dir, s, d, sr); err != nil {
				log.Printf("[%s] prepare segment %d (rend=%s) failed: %v", channelID, s.Sequence, j.rend, err)
			}
		}(seg, dest, src)
	}
	wg.Wait()
}

// prepareSegmentFromSource is like prepareSegment but takes an explicit
// source path (StoragePath with {rendition} already substituted) and an
// explicit dest file (seg_{seq}_{rend}.ts).
func (e *Engine) prepareSegmentFromSource(dir string, seg models.SegmentItem, dest, src string) error {
	marker := dest + ".pts"

	if _, err := os.Stat(marker); err == nil {
		return nil // already prepared
	}

	needDownload := false
	if _, err := os.Stat(dest); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[%s] Stat dest error: %v (will re-download)", filepath.Base(dir), err)
		}
		needDownload = true
	}
	if needDownload {
		if err := e.rclone.CopyTo(src, dest); err != nil {
			return fmt.Errorf("download %s: %w", src, err)
		}
	}

	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("verify dest after download: %w", err)
	}

	if err := e.rewritePtsOffset(dest, marker, seg); err != nil {
		log.Printf("[%s] PTS rewrite failed for %s: %v", filepath.Base(dir), filepath.Base(dest), err)
		return e.markPrepared(marker)
	}

	if seg.IsAd || len(seg.SliceInserts) > 0 {
		e.applySliceEvents(seg, dest)
	}
	return nil
}

func (e *Engine) applySliceEvents(seg models.SegmentItem, dest string) {
	// Convert SpliceInsert events to SpliceEvent format for TSDuck injection
	events := seg.SliceInserts
	currentFile := dest

	if len(events) == 0 {

		// inject programme information for normal ads ts
		if seg.IsAd {
			tempFile := fmt.Sprintf("%s.splice.tmp", currentFile)
			success := utils.ConfigurePmtForScte35(currentFile, tempFile)
			if !success {
				log.Printf("Splice injection failed for %s", dest)
				if _, err := os.Stat(tempFile); err == nil {
					os.Remove(tempFile)
				}
				return
			}

			// Replace current file with injected version
			err := os.Rename(tempFile, currentFile)
			if err != nil {
				log.Printf("Failed to move %s to %s: %v", tempFile, currentFile, err)
				return
			}
			log.Printf("[%d] Injected pmt for %s", seg.Sequence, currentFile)
		}

		return
	}

	for i := range events {
		se := events[i]
		tempFile := fmt.Sprintf("%s.splice%d.tmp", currentFile, i)
		xml := utils.GenerateSpliceInsertXml(se)

		success := utils.InjectSpliceEvent(currentFile, tempFile, xml)
		if !success {
			log.Printf("Splice injection failed for %s", dest)
			if _, err := os.Stat(tempFile); err == nil {
				os.Remove(tempFile)
			}
			return
		}

		// Replace current file with injected version
		err := os.Rename(tempFile, currentFile)
		if err != nil {
			log.Printf("Failed to move %s to %s: %v", tempFile, currentFile, err)
			return
		}
		log.Printf("Injected %s splice event into %s", se.Type, currentFile)
	}
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

// generateRenditionPlaylist writes one media playlist per rendition
// into the rung's own subdir. M3.2: layout is
// `{TMP_DIR}/{ch}/{rend}/playlist.m3u8` referencing the flat
// `seg_{seq}.ts` files in the SAME subdir (each rendition downloads
// from its own tier bucket). Only segments whose per-rung .pts marker
// exists are advertised, so players never 404 on a half-prepared file.
func (e *Engine) generateRenditionPlaylist(ctx context.Context, channelID, rendition string, segments []models.SegmentItem) {
	if len(segments) == 0 {
		return
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Sequence < segments[j].Sequence
	})

	// Only advertise prepared segments (downloaded + PTS-rewritten, with the
	// .pts marker) so players never request files still being written /
	// missing -> avoids 404 buffering. M3.2: marker is in the rung subdir.
	rDir := rungDir(e.tmpDir, channelID, rendition)
	var available []models.SegmentItem
	for _, seg := range segments {
		marker := filepath.Join(rDir, fmt.Sprintf("seg_%d.ts.pts", seg.Sequence))
		if _, err := os.Stat(marker); err == nil {
			available = append(available, seg)
		}
	}
	if len(available) == 0 {
		return // keep the previous playlist until the window is ready
	}
	segments = available

	// EXT-X-TARGETDURATION must be >= every segment duration. We track the
	// max observed segment duration and round UP to the next integer so
	// 3.999 -> 4 and 5.111 -> 6. math.Ceil guarantees spec compliance even
	// when a segment is fractionally over an integer boundary.
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
		log.Printf("[Engine] channel=%s rung=%s buildMediaPlaylistBody: %v", channelID, rendition, err)
		return
	}

	playlistPath := filepath.Join(rDir, "playlist.m3u8")
	tmpPlaylistPath := playlistPath + ".tmp"

	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	}

	_ = e.rdb.Set(ctx, renditionMediaRedisKey(channelID, rendition), playlist, 24*time.Hour).Err()
}

// generateMasterPlaylist renders the master playlist from the supplied
// meta list. The caller resolves the ladder (planner metadata or the
// hard-coded ActiveLadder fallback) so this function is pure: same input
// -> same output, easy to unit-test. Always writes -- even on ticks with
// no recent candidates -- so a player reload never gets a 404 against
// the master URI. M3: each STREAM-INF points at the flat
// `playlist_{rend}.m3u8` in the channel root (not a rung subdir).
func (e *Engine) generateMasterPlaylist(ctx context.Context, channelID string, meta []RenditionMeta) {
	if len(meta) == 0 {
		// Defensive: caller should always pass at least one rung (the
		// hard-coded ActiveLadder is the cold-start fallback). Synth a
		// single-rung default so the master still serves. The default meta
		// carries no bandwidth / no resolution so it's skipped from the
		// STREAM-INF lines, but the file is written with the header + an
		// empty body so curl returns 200 (better than 404 for player reload).
		meta = []RenditionMeta{{Rendition: DefaultRendition}}
	}

	sorted := sortRungsByLadder(meta)
	uris := pickRenditionURIs(sorted)
	playlist := buildMasterPlaylist(sorted, uris)

	chRoot := channelDir(e.tmpDir, channelID)
	if err := os.MkdirAll(chRoot, 0755); err != nil {
		log.Printf("[%s] mkdir channel dir error: %v", channelID, err)
		return
	}
	playlistPath := filepath.Join(chRoot, "playlist.m3u8")
	tmpPlaylistPath := playlistPath + ".tmp"
	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	}
	_ = e.rdb.Set(ctx, masterRedisKey(channelID), playlist, 24*time.Hour).Err()
}

// loadRenditionMeta reads simlive:renditions:{ch} from Redis and decodes it
// into []RenditionMeta. Returns (nil, nil) for a missing key (cold start),
// (nil, err) for an unparseable payload. Cached in e.rendMetaCache so a
// brief metadata blip still yields a master.
func (e *Engine) loadRenditionMeta(ctx context.Context, channelID string) ([]RenditionMeta, error) {
	e.rendMetaMu.Lock()
	if cached, ok := e.rendMetaCache[channelID]; ok {
		e.rendMetaMu.Unlock()
		return cached, nil
	}
	e.rendMetaMu.Unlock()

	raw, err := e.rdb.Get(ctx, renditionMetadataRedisKey(channelID)).Result()
	if err != nil {
		// redis.Nil is the expected cold-start case -- not an error worth logging.
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
