package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
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

	defaultMaxPrepareRetries = 3
	defaultPrepareRetryBase  = 500  // ms
	defaultPrepareRetryMax   = 8000 // ms

	failureInactivateThreshold = 5
	failureCounterKey          = "planner:failures:%s"
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

var ErrEndOfTimeline = errors.New("end of timeline reached - processing complete")

type renditionPair struct {
}

type Planner struct {
	db         *gorm.DB
	rdb        *redis.Client
	espx       *api.ESPXClient
	rclone     *storage.RcloneService
	bucketName string
	adsBucket  string

	rendCache   map[string]renditionPair
	ladderCache map[string]map[string]renditionPair

	isBlockingProcessing bool
	processingSegments   []models.SegmentItem
	adsId                uint64
	logger               utils.Logger
}

func NewPlanner(db *gorm.DB, rdb *redis.Client, espx *api.ESPXClient, rclone *storage.RcloneService) *Planner {
	return &Planner{
		db:          db,
		rdb:         rdb,
		espx:        espx,
		rclone:      rclone,
		bucketName:  "espx-video-lib:espx-video-lib",
		adsBucket:   "espx-video-lib:espx-video-lib/ads-v3",
		rendCache:   make(map[string]renditionPair),
		ladderCache: make(map[string]map[string]renditionPair),
		logger:      utils.GetLogger("PLANNER"),
	}
}

func (p *Planner) ProcessEPGToTimeline(ctx context.Context, channel models.PlayoutChannel, dateStr string) error {
	callStartedAt := time.Now()

	loc := utils.GetLocFor(channel.ContentTimezone)
	processDate := utils.StartOfDay(callStartedAt).In(loc)
	if parsed, err := time.ParseInLocation("2006-01-02", dateStr, loc); err == nil {
		processDate = utils.StartOfDay(parsed)
	}
	horizon := utils.EndOfDay(processDate)

	schedules, err := p.espx.GetSchedules(channel.ChannelEpgID, dateStr, configs.TZ_NUM)
	if err != nil || len(schedules.Data) == 0 {
		return err
	}

	p.logger.Info(fmt.Sprintf("pulled schedule data %s (%s)", processDate, time.Duration(time.Since(callStartedAt)).String()))

	logger := utils.GetLogger(fmt.Sprintf("%s:%s", "PLANNER", channel.ID))

	adSegments, _ := p.loadAdSegments()
	lastSeq := p.getLastSequenceNumber(ctx, channel.ID)

	processor := &SegmentProcessor{
		planner:         p,
		pipe:            p.rdb.Pipeline(),
		ctx:             ctx,
		nowUnix:         float64(callStartedAt.Unix()),
		sequenceCounter: lastSeq,
		adSegments:      adSegments,
		channelID:       channel.ID,
		horizon:         horizon,
		logger:          logger,
	}

	for _, item := range schedules.Data {
		if isEndOfTimeline(processor.horizon, processor.runningTime) {
			break
		}
		if err := processor.ProcessScheduleItem(item, channel, callStartedAt); err == ErrEndOfTimeline {
			break
		}
	}

	p.logger.Info(fmt.Sprintf("finished plan for %s", processDate))
	if err := processor.flush(); err != nil {
		return err
	}
	return nil
}

func (p *Planner) loadAdSegments() ([]models.SegmentItem, error) {
	adsMasterM3u8Path := fmt.Sprintf("%s/vod.m3u8", p.adsBucket)

	renditions := utils.GetRenditionsByMaster(adsMasterM3u8Path, p.rclone)
	adsM3u8Path := fmt.Sprintf("%s/%s/vod.m3u8", p.adsBucket, renditions[0])
	m3u8Content, err := p.rclone.Cat(adsM3u8Path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ads m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	adSegments := make([]models.SegmentItem, 0, 5)
	var currentDuration float64
	var ptsStart, ptsEnd int64

	// Convert []*string to []string for Renditions field

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
				StoragePath: fmt.Sprintf("%s/%s/%s", p.adsBucket, "{rendition}", line),
				PtsStart:    ptsStart,
				PtsEnd:      ptsEnd,
				MediaStart:  mStart,
				MediaEnd:    mEnd,
				Renditions:  renditions,
			})
			currentDuration = 0
			if len(adSegments) == 5 {
				return adSegments, nil
			}
		}
	}
	return adSegments, nil
}

func (p *Planner) getLastSequenceNumber(ctx context.Context, channelID string) int64 {
	items, err := p.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:   timelineKeyFor(channelID),
		Start: 0,
		Stop:  0,
		Rev:   true,
	}).Result()
	if err != nil || len(items) == 0 {
		return 0
	}
	var seg models.SegmentItem
	if err := json.Unmarshal([]byte(items[0]), &seg); err == nil {
		return seg.Sequence
	}
	return 0
}

func isEndOfTimeline(horizon time.Time, runningTime float64) bool {
	if horizon.IsZero() {
		return false
	}
	return horizon.Before(time.Unix(int64(runningTime), 0))
}

func timelineKeyFor(channelID string) string {
	return fmt.Sprintf("simlive:timeline:%s", channelID)
}

func (p *Planner) HasDateInTimeline(ctx context.Context, channelID string, dayStart, dayEnd time.Time) (bool, error) {
	if p.rdb == nil {
		return false, nil
	}
	startStr := fmt.Sprintf("%d", dayStart.Unix())
	endStr := fmt.Sprintf("%d", dayEnd.Unix())

	key := timelineKeyFor(channelID)
	n, err := p.rdb.ZCount(ctx, key, startStr, endStr).Result()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	return false, nil
}

func (p *Planner) HasLegacyTimeline(ctx context.Context, channelID string) (bool, error) {
	return p.hasLegacyTimeline(ctx, channelID)
}

func (p *Planner) DeleteTimeline(ctx context.Context, channelID string) error {
	if p.rdb == nil {
		return nil
	}
	key := timelineKeyFor(channelID)
	return p.rdb.Del(ctx, key).Err()
}

func (p *Planner) getCursor(ctx context.Context, channelID string) int64 {
	val, err := p.rdb.Get(ctx, fmt.Sprintf("planner:cursor:%s", channelID)).Int64()
	if err != nil {
		return time.Now().Unix()
	}
	return val
}

func (p *Planner) setCursor(ctx context.Context, channelID string, ts int64) {
	p.rdb.Set(ctx, fmt.Sprintf("planner:cursor:%s", channelID), ts, 0)
}

func (p *Planner) incrFailureCounter(ctx context.Context, channelID string) int64 {
	key := fmt.Sprintf(failureCounterKey, channelID)
	n, err := p.rdb.Incr(ctx, key).Result()
	if err != nil {
		log.Printf("[Planner] INCR(%s) error: %v", key, err)
		return 0
	}
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

func (p *Planner) markChannelLive(channelID string) error {
	if p.db == nil {
		return nil
	}
	res := p.db.Model(&models.PlayoutChannel{}).
		Where("id = ?", channelID).
		Update("status", "LIVE")
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("[Planner] channel=%s marked LIVE (timeline data in Redis)", channelID)
	}
	return nil
}

func (p *Planner) handlePrepareResult(ctx context.Context, channelID string, err error) {
	if err == nil {
		p.resetFailureCounter(ctx, channelID)
		if dbErr := p.markChannelLive(channelID); dbErr != nil {
			log.Printf("[Planner] markChannelLive(%s) error: %v", channelID, dbErr)
		}
		return
	}
	count := p.incrFailureCounter(ctx, channelID)
	if count >= int64(failureInactivateThreshold) {
		if dbErr := p.markChannelInactive(channelID); dbErr != nil {
			log.Printf("[Planner] markChannelInactive(%s) error: %v", channelID, dbErr)
		}
	}
}

func (p *Planner) HandlePrepareResultForChannel(ctx context.Context, channelID string, err error) {
	p.handlePrepareResult(ctx, channelID, err)
}

func (p *Planner) ResetFailureCounterForChannel(ctx context.Context, channelID string) {
	p.resetFailureCounter(ctx, channelID)
}

func (p *Planner) MarkChannelLiveForChannel(ctx context.Context, channelID string) {
	if err := p.markChannelLive(channelID); err != nil {
		log.Printf("[Planner] MarkChannelLiveForChannel(%s) error: %v", channelID, err)
	}
}

func (p *Planner) ProcessWindow(ctx context.Context, ch models.PlayoutChannel) error {
	nowUnix := time.Now().Unix()
	lookahead := nowUnix + LOOKAHEAD_SECONDS
	p.setCursor(ctx, ch.ID, lookahead)
	return nil
}

func (p *Planner) CleanupOldSegments(ctx context.Context, channelID string) {
	cutoff := fmt.Sprintf("%d", time.Now().Unix()-RETENTION_SECONDS)
	key := timelineKeyFor(channelID)
	if _, err := p.rdb.ZRemRangeByScore(ctx, key, "-inf", cutoff).Result(); err != nil {
		log.Printf("[Planner] ZREMRANGEBYSCORE(%s, -inf, %s) error: %v", key, cutoff, err)
	}
}

func retryBackoffDelay(i int, base, maxDelay time.Duration) time.Duration {
	if i < 1 {
		return 0
	}
	delay := base * (1 << (i - 1))
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func (p *Planner) ProcessEPGToTimelineWithRetry(ctx context.Context, ch models.PlayoutChannel, dateStr string) error {
	attempts := maxPrepareRetries()
	if attempts < 1 {
		attempts = 1
	}
	base := time.Duration(prepareRetryBaseMs()) * time.Millisecond
	maxDelay := time.Duration(prepareRetryMaxMs()) * time.Millisecond

	var lastErr error
	for i := 1; i <= attempts; i++ {
		if lastErr = p.ProcessEPGToTimeline(ctx, ch, dateStr); lastErr == nil {
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

func (p *Planner) TimelineSize(ctx context.Context, channelID string) int64 {
	if p.rdb == nil {
		return 0
	}

	return 1
}

func (p *Planner) LastProcessedCursor(ctx context.Context, channelID string) int64 {
	return p.getCursor(ctx, channelID)
}

// hasLegacyTimeline reports whether the ZSET simlive:timeline:{ch} contains
// at least one member whose JSON has no "rendition" field. Used on the first
// planner tick after M1 ships to detect pre-upgrade timelines and DEL the
// key so ProcessEPGToTimeline repopulates with tags. Best-effort: an empty
// ZSET (no members at all) returns false so cold starts aren't flushed.
func (p *Planner) hasLegacyTimeline(ctx context.Context, channelID string) (bool, error) {
	if p.rdb == nil {
		return false, nil
	}
	// M2: check every active rung's ZSET. A legacy (pre-M1) timeline lives
	// under the flat simlive:timeline:{ch} key; the per-rung keys are new.
	// If ANY rung key holds a member without a rendition tag, treat the
	// channel as legacy so the caller flushes + repopulates.
	key := timelineKeyFor(channelID)
	items, err := p.rdb.ZRange(ctx, key, 0, 0).Result()
	if err != nil {
		return false, err
	}
	// Probe the first member for the rendition field. Use a minimal struct
	// instead of models.SegmentItem so we don't drag the full model import
	// (also makes the field-check explicit).
	var probe struct {
		Rendition string `json:"rendition"`
	}
	if err := json.Unmarshal([]byte(items[0]), &probe); err != nil {
		// Unparseable -- treat as legacy so we don't serve garbage.
		return true, nil
	}
	if probe.Rendition == "" {
		return true, nil
	}
	return false, nil
}
