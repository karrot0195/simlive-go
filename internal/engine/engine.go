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
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		return
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

			// M1.5: SHARED segment cache. All rungs reference the same
			// seg_N.ts files on disk, so window selection / downloads / GC
			// happen ONCE per tick on the flat (channel-root) view of
			// allSegs. The per-rung group is only used to render N media
			// playlists (one per active rung), all referencing the same
			// shared seg_N.ts files. The active rung's VOD dir is the
			// single source the engine downloads from; the other rungs
			// re-use the same files.
			//
			// Legacy untagged members (Rendition == "") are rescued to
			// DefaultRendition ("360p") by groupSegsByRendition.
			allSegsForWindow := allSegs

			// Pick a single set of candidate segments + on-disk window
			// from the full timeline (no per-rung split -- they all share).
			var candidateSegs []models.SegmentItem
			currentIndex := -1

			// 1. Find the segment containing the current time
			for idx, seg := range allSegsForWindow {
				if seg.MediaStart <= nowFloat && nowFloat < seg.MediaEnd {
					currentIndex = idx
					break
				}
			}

			// 2. Auto-Catchup if nowFloat drifts away from the Redis timeline
			if currentIndex == -1 && len(allSegsForWindow) > 0 {
				if nowFloat >= allSegsForWindow[len(allSegsForWindow)-1].MediaEnd {
					if len(allSegsForWindow) >= playlistWindow {
						currentIndex = len(allSegsForWindow) - playlistWindow
					} else {
						currentIndex = 0
					}
				} else {
					for idx, seg := range allSegsForWindow {
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

			if currentIndex != -1 {
				endIndex := currentIndex + playlistWindow
				if endIndex > len(allSegsForWindow) {
					endIndex = len(allSegsForWindow)
				}
				candidateSegs = allSegsForWindow[currentIndex:endIndex]
			}

			if len(candidateSegs) < playlistWindow {
				if len(allSegsForWindow) <= playlistWindow {
					candidateSegs = allSegsForWindow
				} else {
					candidateSegs = allSegsForWindow[len(allSegsForWindow)-playlistWindow:]
				}
			}

			if len(candidateSegs) == 0 {
				continue
			}

			// Single channel-level state (one set of files = one window).
			e.statesMu.Lock()
			state, ok := e.states[channelID]
			if !ok {
				state = &channelState{}
				e.states[channelID] = state
			}
			e.statesMu.Unlock()

			candidateFirst := candidateSegs[0].Sequence
			candidateLast := candidateSegs[len(candidateSegs)-1].Sequence
			lastSeg := candidateSegs[len(candidateSegs)-1]
			lastEndTimeInt := int64(lastSeg.MediaEnd)

			state.mu.Lock()
			if !state.initialized {
				state.firstSeq = candidateFirst
				state.lastSeq = candidateLast
				state.lastEndTime = lastEndTimeInt
				state.initialized = true
				log.Printf("[%s] Initialized PTS window: firstSeq=%d, lastSeq=%d (shared seg cache)",
					channelID, state.firstSeq, state.lastSeq)
			} else if candidateFirst != state.firstSeq || candidateLast != state.lastSeq {
				state.firstSeq = candidateFirst
				state.lastSeq = candidateLast
				state.lastEndTime = lastEndTimeInt
			}
			state.mu.Unlock()

			// Compute the bounded active on-disk window from the shared
			// timeline (one set of seg_N.ts files for all rungs).
			downloadSegs := allSegsForWindow
			if currentIndex >= 0 {
				startIdx := currentIndex - e.keepBehind
				if startIdx < 0 {
					startIdx = 0
				}
				endIdx := currentIndex + playlistWindow + e.downloadAhead - 1
				if endIdx >= len(allSegsForWindow) {
					endIdx = len(allSegsForWindow) - 1
				}
				if endIdx >= startIdx {
					downloadSegs = allSegsForWindow[startIdx : endIdx+1]
				}
			}

			// Single sync + single download (shared cache).
			activeMap := make(map[string]bool)
			for _, seg := range downloadSegs {
				activeMap[fmt.Sprintf("seg_%d.ts", seg.Sequence)] = true
			}
			syncLocalCacheDir(channelDir, activeMap)
			go e.downloadAndPrepareSegments(channelDir, channelID, downloadSegs)

			// Determine the active rungs to advertise. Prefer the
			// planner-written per-channel metadata; fall back to the
			// hard-coded ActiveLadder so the master always lists 3 rungs
			// even before the planner has run.
			meta := e.activeRungMeta(ctx, channelID)
			for _, m := range meta {
				e.generateHLSPlaylist(ctx, channelID, m.Rendition, candidateSegs)
			}
			e.generateMasterPlaylist(ctx, channelID, meta)
		}
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

	// Stat the .ts file with an explicit "not exists" check (defensive against
	// a Stat returning a transient error masking a missing file, which would
	// otherwise skip the download and fail inside rewritePtsOffset with
	// "No such file or directory").
	needDownload := false
	if _, err := os.Stat(dest); err != nil {
		if !os.IsNotExist(err) {
			// Some other stat error (perm denied, IO blip) -- treat as
			// missing and let the download path retry.
			log.Printf("[%s] Stat dest error: %v (will re-download)", filepath.Base(channelDir), err)
		}
		needDownload = true
	}
	if needDownload {
		if err := e.rclone.CopyTo(seg.StoragePath, dest); err != nil {
			return fmt.Errorf("download %s: %w", seg.StoragePath, err)
		}
	}

	// Belt + braces: verify the file is actually on disk before handing it
	// to ffmpeg. A racing syncLocalCacheDir + recreate race could otherwise
	// pass a missing path to rewritePtsOffset.
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("verify dest after download: %w", err)
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

// generateHLSPlaylist writes a per-rung media playlist into the channel
// root (M1.5 shared-cache model). The .pts marker is checked in the same
// channel root (NOT a per-rung subdir) because the seg_N.ts files are
// shared across all rungs. Each rung's playlist references the SAME
// seg_N.ts files -- only the playlist label differs.
func (e *Engine) generateHLSPlaylist(ctx context.Context, channelID, rendition string, segments []models.SegmentItem) {
	if len(segments) == 0 {
		return
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Sequence < segments[j].Sequence
	})

	// Only advertise prepared segments (downloaded + PTS-rewritten, with the
	// .pts marker) so players never request files still being written /
	// missing -> avoids 404 buffering. M1.5: markers live in the channel
	// root, not a per-rung subdir (shared cache).
	chRoot := channelDir(e.tmpDir, channelID)
	var available []models.SegmentItem
	for _, seg := range segments {
		marker := filepath.Join(chRoot, fmt.Sprintf("seg_%d.ts.pts", seg.Sequence))
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

	playlist := "#EXTM3U\n"
	playlist += "#EXT-X-VERSION:3\n"
	playlist += fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDuration)
	playlist += "#EXT-X-START:TIME-OFFSET=0\n"
	playlist += fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n\n", segments[0].Sequence)

	for _, seg := range segments {
		playlist += fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration)
		playlist += fmt.Sprintf("seg_%d.ts\n", seg.Sequence)
	}

	playlistPath := filepath.Join(chRoot, rungPlaylistFilename(rendition))
	tmpPlaylistPath := playlistPath + ".tmp"

	if err := os.WriteFile(tmpPlaylistPath, []byte(playlist), 0644); err == nil {
		_ = os.Rename(tmpPlaylistPath, playlistPath)
	}

	_ = e.rdb.Set(ctx, rungMediaRedisKey(channelID, rendition), playlist, 24*time.Hour).Err()
}

// generateMasterPlaylist renders the master playlist from the supplied
// meta list. The caller resolves the ladder (planner metadata or the
// hard-coded ActiveLadder fallback) so this function is pure: same input
// -> same output, easy to unit-test. Always writes -- even on ticks with
// no recent candidates -- so a player reload never gets a 404 against
// the master URI.
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
