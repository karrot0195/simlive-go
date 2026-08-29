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
)

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
	processStartTime   time.Time

	// Rendition matching: ensures VOD and ads share the same resolution for seamless streaming
	rendMu     sync.Mutex
	rendCache  map[string]renditionPair
	adsRend    string // resolved once (ads are shared across all streams)
	adsRendSet bool
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
	p.processStartTime = time.Now()

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

	nowLocal := p.processStartTime
	for _, item := range schedules.Data {
		if p.isEndOfTimeline(p.processStartTime, p.globalRunningTime) {
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
	if p.isEndOfTimeline(p.processStartTime, runningTime) {
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

func (p *Planner) isEndOfTimeline(processTime time.Time, runningTime float64) bool {
	endOfDay := utils.EndOfDay(processTime)
	return endOfDay.Before(time.Unix(int64(runningTime), 0))
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
	key := fmt.Sprintf("timeline:%s", channelID)
	p.rdb.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", cutoff))
}
