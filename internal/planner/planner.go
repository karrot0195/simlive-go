package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

const (
	WindowItemSize      = 50
	AdInsertionInterval = 30 // seconds
	RedisExpiryHours    = 26
	WindowBufferSeconds = 120
	RETENTION_SECONDS   = 7200
	LOOKAHEAD_SECONDS   = 7200

	// §7 retry/backoff config. Overridable via MAX_PREPARE_RETRIES,
	// PREPARE_RETRY_BASE_MS, PREPARE_RETRY_MAX_MS.
	defaultMaxPrepareRetries = 3
	defaultPrepareRetryBase  = 500  // ms
	defaultPrepareRetryMax   = 8000 // ms

	// §7 failure tracking. After FAILURE_INACTIVATE_THRESHOLD consecutive
	// per-date ProcessEPGToTimeline failures, mark the channel inactive.
	// The counter resets to 0 on the first success.
	failureInactivateThreshold = 5

	// Per-channel failure counter Redis key prefix; the value is the
	// number of consecutive failed (channel, date) attempts since the last
	// success. Cleared by a successful prepareDate().
	failureCounterKey = "planner:failures:%s"
)

func maxPrepareRetries() int {
	return utils.GetEnvInt("MAX_PREPARE_RETRIES", defaultMaxPrepareRetries)
}
func prepareRetryBaseMs() int {
	return utils.GetEnvInt("PREPARE_RETRY_BASE_MS", defaultPrepareRetryBase)
}
func prepareRetryMaxMs() int {
	return utils.GetEnvInt("PREPARE_RETRY_MAX_MS", defaultPrepareRetryMax)
}

// Possible renditions for VOD and ads. Priority from lowest to highest
// (prefer pairs with equal resolution, as high as possible).
var (
	vodRenditionCandidates = []string{"L_0", "L_1", "L_2"}
	adsRenditionCandidates = []string{"L0", "L1", "L2"}
)

var ErrEndOfTimeline = errors.New("end of timeline reached - processing complete")

type renditionPair struct {
	vod string
	ads string
}

// qualityTier: quality level per SIMLIVE_QUALITY.
type qualityTier struct {
	label string
	w, h  int // target resolution (width x height)
}

// qualityTierMap: SIMLIVE_QUALITY -> target resolution.
var qualityTierMap = map[string]qualityTier{
	"360p":  {label: "360p", w: 640, h: 360},
	"720p":  {label: "720p", w: 1280, h: 720},
	"1080p": {label: "1080p", w: 1920, h: 1080},
}

// fallbackRenditionForTier: verified default VOD/ads pair per tier used when
// no rendition can be probed (360p->L_0/L2, 720p->L_1/L1, 1080p->L_2/L0).
var fallbackRenditionForTier = map[string]renditionPair{
	"360p":  {vod: "L_0", ads: "L2"},
	"720p":  {vod: "L_1", ads: "L1"},
	"1080p": {vod: "L_2", ads: "L0"},
}

type Planner struct {
	db         *gorm.DB
	rdb        *redis.Client
	espx       *api.ESPXClient
	rclone     *storage.RcloneService
	bucketName string
	adsBucket  string

	globalRunningTime  float64
	totalSegmentsAdded int64
	channelID          string

	// horizon is the wall-clock end of the date we are currently processing
	// (EndOfDay of processDate in the channel's content timezone). A single
	// ProcessEPGToTimeline call walks the timeline up to this point and no
	// further, regardless of when the planner was started. Zero means
	// "no horizon set" -- isEndOfTimeline is a no-op in that case.
	horizon     time.Time
	processDate time.Time

	// Rendition matching: ensures VOD and ads share the same resolution for seamless streaming
	rendMu     sync.Mutex
	rendCache  map[string]renditionPair
	adsRend    string // resolved once (ads are shared across all streams)
	adsRendSet bool

	// NOTE: ProcessEPGToTimeline is NOT safe to call concurrently on the same
	// Planner instance because it mutates shared state (globalRunningTime,
	// totalSegmentsAdded, processDate, horizon, channelID) without locking.
	// The cron loop in cmd/planner/main.go runs one channel per goroutine
	// under PLANNER_WORKERS, so any value > 1 risks inter-channel races on
	// this shared state. Keep PLANNER_WORKERS at 1 until per-call state is
	// moved onto SegmentProcessor (see docs/290826-planner-24h-next-day-rollover.md).
}

func NewPlanner(db *gorm.DB, rdb *redis.Client, espx *api.ESPXClient, rclone *storage.RcloneService) *Planner {
	return &Planner{
		db:         db,
		rdb:        rdb,
		espx:       espx,
		rclone:     rclone,
		bucketName: "espx-video-lib:espx-video-lib",
		adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
		rendCache:  make(map[string]renditionPair),
	}
}

func (p *Planner) ProcessEPGToTimeline(ctx context.Context, channel models.PlayoutChannel, dateStr string) error {
	p.channelID = channel.ID
	callStartedAt := time.Now()

	// Set the per-call horizon from the requested date so a single call can
	// walk today AND tomorrow without being stopped at today's 23:59:59.
	// If dateStr is empty or unparseable, fall back to today in the channel's
	// content timezone (defaulting to UTC if the timezone is unknown).
	loc := utils.GetLocFor(channel.ContentTimezone)
	if dateStr == "" {
		p.processDate = utils.StartOfDay(callStartedAt).In(loc)
	} else if parsed, err := time.ParseInLocation("2006-01-02", dateStr, loc); err == nil {
		p.processDate = utils.StartOfDay(parsed)
	} else {
		p.processDate = utils.StartOfDay(callStartedAt).In(loc)
	}
	p.horizon = utils.EndOfDay(p.processDate)
	// Always reset the loop's running time so each call starts clean.
	p.globalRunningTime = 0

	schedules, err := p.espx.GetSchedules(channel.ChannelEpgID, dateStr, configs.TZ_NUM)
	if err != nil || len(schedules.Data) == 0 {
		return err
	}

	adSegments, _ := p.loadAdSegments()
	lastSeq := p.getLastSequenceNumber(ctx)

	processor := &SegmentProcessor{
		planner:         p,
		pipe:            p.rdb.Pipeline(),
		ctx:             ctx,
		nowUnix:         float64(time.Now().Unix()),
		sequenceCounter: lastSeq,
		adSegments:      adSegments,
	}

	nowLocal := callStartedAt
	for _, item := range schedules.Data {
		if p.isEndOfTimeline(p.globalRunningTime) {
			break
		}
		if err := processor.ProcessScheduleItem(item, channel, nowLocal); err == ErrEndOfTimeline {
			break
		}
	}

	return p.flushRedisPipeline(processor.pipe)
}

// resolveQualityTier reads SIMLIVE_QUALITY: auto (default) | 360p | 720p | 1080p.
// Returns the selected tier and isAuto. Invalid values -> WARN + auto.
func (p *Planner) resolveQualityTier() (tier qualityTier, isAuto bool) {
	q := utils.GetEnv("SIMLIVE_QUALITY", "auto")
	if q == "" || q == "auto" {
		return qualityTier{}, true
	}
	if t, ok := qualityTierMap[q]; ok {
		return t, false
	}
	log.Printf("[Planner] WARN: SIMLIVE_QUALITY=%q invalid (auto|360p|720p|1080p), using auto", q)
	return qualityTier{}, true
}

// --- Rendition resolution -------------------------------------------------

// probeResolution downloads the first segment of a rendition temporarily and reads
// the video resolution (width x height) with ffprobe. Returns an error if the
// rendition does not exist / cannot be read.
func (p *Planner) probeResolution(remotePath string) (w, h int, err error) {
	tmp, err := os.CreateTemp("", "simlive-probe-*.ts")
	if err != nil {
		log.Printf("[Planner] probe %s: CreateTemp failed: %v", remotePath, err)
		return 0, 0, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := p.rclone.CopyTo(remotePath, tmpPath); err != nil {
		log.Printf("[Planner] probe %s: rclone copy failed: %v", remotePath, err)
		return 0, 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0", tmpPath)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[Planner] probe %s: ffprobe failed: %v", remotePath, err)
		return 0, 0, fmt.Errorf("ffprobe: %w", err)
	}
	// ffprobe may print the resolution twice (mpegts multi-program), take the first line only
	firstLine := strings.TrimSpace(strings.Split(strings.TrimSpace(string(out)), "\n")[0])
	parts := strings.Split(firstLine, ",")
	if len(parts) != 2 {
		log.Printf("[Planner] probe %s: unexpected ffprobe output: %q", remotePath, out)
		return 0, 0, fmt.Errorf("unexpected ffprobe output: %q", out)
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("bad resolution %q", out)
	}
	return w, h, nil
}

// probeRenditions measures the resolution of each rendition in the list.
func (p *Planner) probeRenditions(basePath, firstSeg string, candidates []string) map[string][2]int {
	res := make(map[string][2]int)
	for _, dir := range candidates {
		remote := fmt.Sprintf("%s/%s/%s", basePath, dir, firstSeg)
		w, h, err := p.probeResolution(remote)
		if err == nil {
			res[dir] = [2]int{w, h}
		}
	}
	return res
}

// resolveAdsRendition picks the ads rendition by priority: env
// SIMLIVE_ADS_RENDITION (hard override) > SIMLIVE_QUALITY tier (rendition closest
// to the target resolution) > auto (largest measured rendition). The result is
// cached once because ads are shared across all streams.
func (p *Planner) resolveAdsRendition() string {
	if p.adsRendSet {
		return p.adsRend
	}
	defer func() { p.adsRendSet = true }()

	if ex := utils.GetEnv("SIMLIVE_ADS_RENDITION", ""); ex != "" {
		p.adsRend = ex
		return p.adsRend
	}

	probes := p.probeRenditions(p.adsBucket, "vod_000.ts", adsRenditionCandidates)
	tier, isAuto := p.resolveQualityTier()
	if !isAuto {
		if best, ok := pickClosestRendition(probes, tier.w, tier.h); ok {
			p.adsRend = best
			return p.adsRend
		}
		if fb, ok := fallbackRenditionForTier[tier.label]; ok {
			log.Printf("[Planner] WARN: could not probe ads resolution for tier %s, fallback %s", tier.label, fb.ads)
			p.adsRend = fb.ads
			return p.adsRend
		}
	}

	best := ""
	bestArea := -1
	for dir, wh := range probes {
		if area := wh[0] * wh[1]; area > bestArea {
			best, bestArea = dir, area
		}
	}
	if best == "" {
		log.Printf("[Planner] WARN: could not probe ads resolution, fallback L0")
		best = "L0"
	}
	p.adsRend = best
	return best
}

// resolveRenditions picks the (VOD, ads) rendition pair for streamID per the
// SIMLIVE_QUALITY tier: prefer a VOD==ads pair at exactly the tier resolution; if
// none, pick the VOD==ads pair closest to the tier (VOD==ads matters most for
// seamless playback). Auto: highest equal-resolution pair. Can be forced
// via env SIMLIVE_VOD_RENDITION / SIMLIVE_ADS_RENDITION.
func (p *Planner) resolveRenditions(streamID string) renditionPair {
	p.rendMu.Lock()
	defer p.rendMu.Unlock()
	if pair, ok := p.rendCache[streamID]; ok {
		return pair
	}

	pair := renditionPair{vod: "L_0", ads: "L0"} // legacy fallback if everything fails
	vodExplicit := utils.GetEnv("SIMLIVE_VOD_RENDITION", "")
	adsExplicit := utils.GetEnv("SIMLIVE_ADS_RENDITION", "")

	if vodExplicit != "" && adsExplicit != "" {
		pair = renditionPair{vod: vodExplicit, ads: adsExplicit}
		p.rendCache[streamID] = pair
		return pair
	}

	vodBase := fmt.Sprintf("%s/%s", p.bucketName, streamID)
	vodProbes := p.probeRenditions(vodBase, "vod_000.ts", vodRenditionCandidates)
	adsProbes := p.probeRenditions(p.adsBucket, "vod_000.ts", adsRenditionCandidates)
	tier, isAuto := p.resolveQualityTier()

	if len(vodProbes) == 0 || len(adsProbes) == 0 {
		log.Printf("[Planner] WARN: could not probe renditions for stream %s (vod=%d ads=%d)",
			streamID, len(vodProbes), len(adsProbes))
		if !isAuto {
			if fb, ok := fallbackRenditionForTier[tier.label]; ok {
				pair = fb
				log.Printf("[Planner] WARN: stream %s fallback by tier %s: VOD %s + ads %s",
					streamID, tier.label, pair.vod, pair.ads)
				p.rendCache[streamID] = pair
				return pair
			}
		}
		p.rendCache[streamID] = pair
		return pair
	}

	if !isAuto {
		if tierPair, exact, ok := pickPairForTier(vodProbes, adsProbes, tier.w, tier.h); ok {
			pair = tierPair
			if !exact {
				log.Printf("[Planner] WARN: stream %s has no VOD/ads pair matching tier %s (%dx%d), picking closest VOD %s (%dx%d) + ads %s (%dx%d)",
					streamID, tier.label, tier.w, tier.h,
					pair.vod, vodProbes[pair.vod][0], vodProbes[pair.vod][1],
					pair.ads, adsProbes[pair.ads][0], adsProbes[pair.ads][1])
			}
			// The actual ads rendition is chosen globally (resolveAdsRendition) -
			// log and warn if its resolution differs from the chosen VOD.
			adsRend := p.resolveAdsRendition()
			if aWh, ok := adsProbes[adsRend]; ok {
				vWh := vodProbes[pair.vod]
				if vWh[0] != aWh[0] || vWh[1] != aWh[1] {
					log.Printf("[Planner] WARN: stream %s VOD %s (%dx%d) resolution differs from ads %s (%dx%d); consider forcing SIMLIVE_VOD_RENDITION/SIMLIVE_ADS_RENDITION",
						streamID, pair.vod, vWh[0], vWh[1], adsRend, aWh[0], aWh[1])
				}
			}
			log.Printf("[Planner] quality tier=%s, stream %s -> renditions: VOD %s + ads %s", tier.label, streamID, pair.vod, adsRend)
			p.rendCache[streamID] = pair
			return pair
		}
		log.Printf("[Planner] WARN: stream %s has no rendition probes for tier %s", streamID, tier.label)
	}

	// 1) Auto (or tier mismatch): prefer the highest equal-resolution pair
	bestPair := renditionPair{}
	bestArea := -1
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if vWh[0] == aWh[0] && vWh[1] == aWh[1] {
				if area := vWh[0] * vWh[1]; area > bestArea {
					bestArea = area
					bestPair = renditionPair{vod: vDir, ads: aDir}
				}
			}
		}
	}
	if bestArea >= 0 {
		pair = bestPair
	} else {
		// 2) No equal pair: pick the pair with the smallest area difference
		diff := int(^uint(0) >> 1) // MaxInt
		for vDir, vWh := range vodProbes {
			for aDir, aWh := range adsProbes {
				if d := abs(vWh[0]*vWh[1] - aWh[0]*aWh[1]); d < diff {
					diff = d
					pair = renditionPair{vod: vDir, ads: aDir}
				}
			}
		}
		log.Printf("[Planner] WARN: stream %s has no equal-resolution pair, choosing VOD %s + ads %s (smallest difference)",
			streamID, pair.vod, pair.ads)
	}

	log.Printf("[Planner] stream %s -> renditions: VOD %s + ads %s", streamID, pair.vod, pair.ads)
	p.rendCache[streamID] = pair
	return pair
}

// pickClosestRendition picks the rendition whose resolution is closest to the target (by area).
func pickClosestRendition(probes map[string][2]int, tw, th int) (string, bool) {
	best := ""
	bestDiff := int(^uint(0) >> 1) // MaxInt
	for dir, wh := range probes {
		if d := abs(wh[0]*wh[1] - tw*th); d < bestDiff {
			bestDiff = d
			best = dir
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

// pickPairForTier picks the (VOD, ads) pair for the target tier:
//  1. A VOD==ads pair at exactly the target resolution -> exact=true.
//  2. Otherwise: VOD==ads pair closest to the tier by area -> exact=false.
//  3. No VOD==ads pair at all: smallest area-difference pair -> exact=false.
//
// ok=false only when there are no probes at all.
func pickPairForTier(vodProbes, adsProbes map[string][2]int, tw, th int) (pair renditionPair, exact bool, ok bool) {
	// 1) Exact tier
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if vWh[0] == tw && vWh[1] == th && aWh[0] == tw && aWh[1] == th {
				return renditionPair{vod: vDir, ads: aDir}, true, true
			}
		}
	}

	// 2) VOD==ads, closest to tier
	bestPair := renditionPair{}
	bestDiff := int(^uint(0) >> 1) // MaxInt
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if vWh[0] != aWh[0] || vWh[1] != aWh[1] {
				continue
			}
			if d := abs(vWh[0]*vWh[1] - tw*th); d < bestDiff {
				bestDiff = d
				bestPair = renditionPair{vod: vDir, ads: aDir}
			}
		}
	}
	if bestDiff != int(^uint(0)>>1) {
		return bestPair, false, true
	}

	// 3) No VOD==ads pair: smallest area difference
	diff := int(^uint(0) >> 1) // MaxInt
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if d := abs(vWh[0]*vWh[1] - aWh[0]*aWh[1]); d < diff {
				diff = d
				bestPair = renditionPair{vod: vDir, ads: aDir}
			}
		}
	}
	if diff == int(^uint(0)>>1) {
		return renditionPair{}, false, false
	}
	return bestPair, false, true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// loadAdSegments parses PTS for ads when available (or falls back to the standard duration)
func (p *Planner) loadAdSegments() ([]models.SegmentItem, error) {
	adsRend := p.resolveAdsRendition()
	adsM3u8Path := fmt.Sprintf("%s/%s/vod.m3u8", p.adsBucket, adsRend)
	m3u8Content, err := p.rclone.Cat(adsM3u8Path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ads m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var adSegments []models.SegmentItem
	var currentDuration float64
	var ptsStart, ptsEnd int64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			if dur, err := strconv.ParseFloat(durStr, 64); err == nil {
				currentDuration = dur
			}
		case strings.HasPrefix(line, "#EXT-X-PTS-RANGE:"):
			ptsRange := strings.TrimPrefix(line, "#EXT-X-PTS-RANGE:")
			parts := strings.Split(ptsRange, "-")
			if len(parts) == 2 {
				ptsStart, _ = strconv.ParseInt(parts[0], 10, 64)
				ptsEnd, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		case strings.HasSuffix(line, ".ts"):
			mStart := float64(ptsStart) / 90000.0
			mEnd := float64(ptsEnd) / 90000.0
			if currentDuration == 0 {
				currentDuration = mEnd - mStart
			}
			adSegments = append(adSegments, models.SegmentItem{
				Duration:    currentDuration,
				StoragePath: fmt.Sprintf("%s/%s/%s", p.adsBucket, adsRend, line),
				PtsStart:    ptsStart,
				PtsEnd:      ptsEnd,
				MediaStart:  mStart,
				MediaEnd:    mEnd,
			})
			currentDuration = 0
		}
	}
	if len(adSegments) > 5 {
		return adSegments[:5], nil
	}
	return adSegments, nil
}

func (p *Planner) getRedisKey() string {
	return fmt.Sprintf("simlive:timeline:%s", p.channelID)
}

func (p *Planner) getLastSequenceNumber(ctx context.Context) int64 {
	items, err := p.rdb.ZRevRange(ctx, p.getRedisKey(), 0, 0).Result()
	if err != nil || len(items) == 0 {
		return 0
	}
	var seg models.SegmentItem
	if err := json.Unmarshal([]byte(items[0]), &seg); err == nil {
		return seg.Sequence
	}
	return 0
}

func (p *Planner) flushRedisPipeline(pipe redis.Pipeliner) error {
	pipe.Expire(context.Background(), p.getRedisKey(), RedisExpiryHours*time.Hour)
	_, err := pipe.Exec(context.Background())
	return err
}

func (p *Planner) addSegmentToRedis(pipe redis.Pipeliner, ctx context.Context, runningTime float64, data models.SegmentItem) error {
	if p.isEndOfTimeline(runningTime) {
		return ErrEndOfTimeline
	}

	data.TimeText = utils.TimeNumToText(data.StartTime)
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	pipe.ZAdd(ctx, p.getRedisKey(), redis.Z{
		Score:  runningTime,
		Member: bytes,
	})

	if runningTime <= (float64(time.Now().Unix()) + 120) {
		_ = p.flushBatch(pipe, ctx)
	} else if p.totalSegmentsAdded > 0 && p.totalSegmentsAdded%WindowItemSize == 0 {
		_ = p.flushBatch(pipe, ctx)
	}
	return nil
}

// isEndOfTimeline reports whether runningTime (Unix seconds) has crossed the
// per-call horizon. If no horizon is set the function is a defensive no-op
// (returns false) so legacy callers that never set horizon keep walking.
func (p *Planner) isEndOfTimeline(runningTime float64) bool {
	if p.horizon.IsZero() {
		return false
	}
	return p.horizon.Before(time.Unix(int64(runningTime), 0))
}

// HasDateInTimeline returns true when the ZSET simlive:timeline:{channelID}
// already has at least one segment whose MediaStart score falls inside the
// local date [dayStart, dayEnd]. Used by the cron loop to short-circuit
// re-processing of a date that has already been prepared.
func (p *Planner) HasDateInTimeline(ctx context.Context, channelID string, dayStart, dayEnd time.Time) (bool, error) {
	if p.rdb == nil {
		return false, nil
	}
	key := fmt.Sprintf("simlive:timeline:%s", channelID)
	n, err := p.rdb.ZCount(ctx, key,
		fmt.Sprintf("%d", dayStart.Unix()),
		fmt.Sprintf("%d", dayEnd.Unix())).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (p *Planner) flushBatch(pipe redis.Pipeliner, ctx context.Context) error {
	_, err := pipe.Exec(ctx)
	return err
}

func (p *Planner) getCursor(ctx context.Context, channelID string) int64 {
	val, err := p.rdb.Get(ctx, fmt.Sprintf("planner:cursor:%s", channelID)).Int64()
	if err != nil {
		return int64(time.Now().Unix())
	}
	return val
}

func (p *Planner) setCursor(ctx context.Context, channelID string, ts int64) {
	p.rdb.Set(ctx, fmt.Sprintf("planner:cursor:%s", channelID), ts, 0)
}

// ---------------------------------------------------------------------------
// Failure tracking (§7)
// ---------------------------------------------------------------------------
// Each channel keeps a counter "planner:failures:{channelID}" of the number
// of consecutive failed (channel, date) prepares since the last success.
// When the counter crosses failureInactivateThreshold the channel is flipped
// to is_active = false so it stops burning CPU/EPG bandwidth on a broken
// upstream. A successful prepareDate resets the counter to 0.
//
// The counter is keyed per-channel (not per-date) so one broken date does not
// by itself trip the breaker — a channel that fails today AND tomorrow's
// prepares repeatedly will be deactivated.

func (p *Planner) incrFailureCounter(ctx context.Context, channelID string) int64 {
	key := fmt.Sprintf(failureCounterKey, channelID)
	n, err := p.rdb.Incr(ctx, key).Result()
	if err != nil {
		log.Printf("[Planner] INCR(%s) error: %v", key, err)
		return 0
	}
	// 24h TTL so a forgotten counter eventually self-cleans.
	p.rdb.Expire(ctx, key, 24*time.Hour)
	return n
}

func (p *Planner) resetFailureCounter(ctx context.Context, channelID string) {
	key := fmt.Sprintf(failureCounterKey, channelID)
	if err := p.rdb.Del(ctx, key).Err(); err != nil {
		log.Printf("[Planner] DEL(%s) error: %v", key, err)
	}
}

func (p *Planner) getFailureCounter(ctx context.Context, channelID string) int64 {
	key := fmt.Sprintf(failureCounterKey, channelID)
	v, err := p.rdb.Get(ctx, key).Int64()
	if err != nil {
		if err != redis.Nil {
			log.Printf("[Planner] GET(%s) error: %v", key, err)
		}
		return 0
	}
	return v
}

// markChannelInactive flips is_active=false on the given channel so the cron
// loop skips it on subsequent ticks. Returns the new row count (0 means the
// channel no longer exists / nothing was updated).
func (p *Planner) markChannelInactive(channelID string) error {
	if p.db == nil {
		return nil
	}
	res := p.db.Model(&models.PlayoutChannel{}).
		Where("id = ?", channelID).
		Update("is_active", false)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("[Planner] channel=%s marked INACTIVE after %d consecutive failures",
			channelID, failureInactivateThreshold)
	}
	return nil
}

// handlePrepareResult records success/failure for a single (channel, date)
// prepare attempt and trips the inactivity breaker once the failure threshold
// is crossed. Idempotent: a successful run resets the counter to 0.
func (p *Planner) handlePrepareResult(ctx context.Context, channelID string, err error) {
	if err == nil {
		p.resetFailureCounter(ctx, channelID)
		return
	}
	count := p.incrFailureCounter(ctx, channelID)
	if count >= int64(failureInactivateThreshold) {
		if dbErr := p.markChannelInactive(channelID); dbErr != nil {
			log.Printf("[Planner] markChannelInactive(%s) error: %v", channelID, dbErr)
		}
	}
}

// HandlePrepareResultForChannel is a thin alias used by the cron entrypoint
// to keep call-sites short and avoid duplicating the "ctx, channelID, err"
// argument list. Equivalent to (*Planner).handlePrepareResult. Exported so
// cmd/planner can call it without an in-package wrapper.
func (p *Planner) HandlePrepareResultForChannel(ctx context.Context, channelID string, err error) {
	p.handlePrepareResult(ctx, channelID, err)
}

// ResetFailureCounterForChannel clears the per-channel consecutive-failure
// counter. Exported so cmd/planner can call it from a "date already prepared"
// short-circuit without an in-package wrapper.
func (p *Planner) ResetFailureCounterForChannel(ctx context.Context, channelID string) {
	p.resetFailureCounter(ctx, channelID)
}

func (p *Planner) ProcessWindow(ctx context.Context, ch models.PlayoutChannel) error {
	cursor := p.getCursor(ctx, ch.ID)
	nowUnix := int64(time.Now().Unix())
	if cursor < nowUnix {
		cursor = nowUnix
	}
	lookahead := nowUnix + int64(LOOKAHEAD_SECONDS)
	p.setCursor(ctx, ch.ID, lookahead)
	return nil
}

func (p *Planner) CleanupOldSegments(ctx context.Context, channelID string) {
	cutoff := time.Now().Unix() - RETENTION_SECONDS
	// Bug fix: was "timeline:%s" which never matched the real key the engine
	// reads, so the bounded retention model was a silent no-op. Now hits the
	// real simlive:timeline:{channelID} ZSET used by the engine + dedup.
	key := fmt.Sprintf("simlive:timeline:%s", channelID)
	if _, err := p.rdb.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", cutoff)).Result(); err != nil {
		log.Printf("[Planner] ZREMRANGEBYSCORE(%s, -inf, %d) error: %v", channelID, cutoff, err)
	}
}

// retryBackoffDelay returns the wait duration for the i-th retry attempt
// (1-indexed: i=1 is the first retry, i.e. the second try overall).
// Schedule: base * 2^(i-1), capped at maxDelay. Pure function — exposed
// (lowercase) so tests can verify the schedule without spinning real sleeps.
func retryBackoffDelay(i int, base, maxDelay time.Duration) time.Duration {
	if i < 1 {
		return 0
	}
	delay := base * (1 << (i - 1))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// ProcessEPGToTimelineWithRetry wraps ProcessEPGToTimeline with bounded
// exponential backoff. Returns the last error if all attempts fail.
// Backoff is "delay = base * 2^(attempt-1)" capped at maxDelay; bounded by
// maxAttempts (default from MAX_PREPARE_RETRIES).
func (p *Planner) ProcessEPGToTimelineWithRetry(ctx context.Context, ch models.PlayoutChannel, dateStr string) error {
	attempts := maxPrepareRetries()
	if attempts < 1 {
		attempts = 1
	}
	base := time.Duration(prepareRetryBaseMs()) * time.Millisecond
	maxDelay := time.Duration(prepareRetryMaxMs()) * time.Millisecond

	var lastErr error
	for i := 1; i <= attempts; i++ {
		lastErr = p.ProcessEPGToTimeline(ctx, ch, dateStr)
		if lastErr == nil {
			return nil
		}
		if i == attempts {
			break
		}
		delay := retryBackoffDelay(i, base, maxDelay)
		log.Printf("[Planner] channel=%s date=%s attempt=%d/%d failed: %v (sleeping %s)",
			ch.ID, dateStr, i, attempts, lastErr, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

// TimelineSize returns the current ZSET cardinality for a channel. Used by
// the metrics reporter to surface Redis pressure to logs/operators.
// Returns 0 on error so a transient Redis blip doesn't crash the cron loop.
func (p *Planner) TimelineSize(ctx context.Context, channelID string) int64 {
	if p.rdb == nil {
		return 0
	}
	key := fmt.Sprintf("simlive:timeline:%s", channelID)
	n, err := p.rdb.ZCard(ctx, key).Result()
	if err != nil {
		log.Printf("[Planner] ZCARD(%s) error: %v", channelID, err)
		return 0
	}
	return n
}

// LastProcessedCursor returns the unix timestamp the planner last wrote for
// the channel (cursor), or 0 if none is set / Redis errors out.
func (p *Planner) LastProcessedCursor(ctx context.Context, channelID string) int64 {
	return p.getCursor(ctx, channelID)
}
