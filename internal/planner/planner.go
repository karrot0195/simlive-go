package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/configs"
	customerrors "simlive/internal/custom-errors"
	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

var ErrEndOfTimeline = errors.New("end of timeline reached - processing complete")

type LastSegmentState struct {
	Sequence    int64
	RunningTime float64
}

type Planner struct {
	ctx        context.Context
	db         *gorm.DB
	rdb        *redis.Client
	espx       *api.ESClient
	rclone     *storage.RcloneService
	bucketName string

	isBlockingProcessing bool
	processingSegments   []models.SegmentItem
	adsId                uint32
	logger               *utils.Logger
}

func NewPlanner(ctx context.Context, db *gorm.DB, rdb *redis.Client, espx *api.ESClient, rclone *storage.RcloneService) *Planner {
	return &Planner{
		ctx:        ctx,
		db:         db,
		rdb:        rdb,
		espx:       espx,
		rclone:     rclone,
		bucketName: utils.GetSettings().VideoBucket,
		logger:     utils.GetLogger("PLANNER"),
	}
}

// CleanChannelData deletes all Redis keys associated with a channel
func (p *Planner) CleanChannelData(ctx context.Context, channelID string) {
	if p.rdb == nil {
		return
	}
	timelineKey := timelineKeyFor(channelID)
	cursorKey := fmt.Sprintf("planner:cursor:%s", channelID)
	failKey := fmt.Sprintf(utils.GetPlannerSettings().FailureCounterKey, channelID)

	if err := p.rdb.Del(ctx, timelineKey, cursorKey, failKey).Err(); err != nil {
		log.Printf("[Planner] CleanChannelData(%s) error: %v", channelID, err)
	} else {
		log.Printf("[Planner] Cleaned up all Redis data for channel=%s", channelID)
	}
}

func (p *Planner) ProcessEPGToTimeline(ctx context.Context, channel models.PlayoutChannel, dateStr string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	callStartedAt := time.Now()

	loc := utils.GetTimeLocation(channel.ContentTimezone)
	processDate := utils.StartOfDay(callStartedAt).In(loc)
	if parsed, err := time.ParseInLocation(configs.DATE_FORMAT_YYYY_MM_DD, dateStr, loc); err == nil {
		processDate = utils.StartOfDay(parsed)
	}
	horizon := utils.EndOfDay(processDate)

	schedules, err := p.espx.GetEPGScheduleItems(channel.ChannelEpgID, dateStr, configs.TZ_NUM)
	if err != nil || len(schedules.Data) == 0 {
		return err
	}

	p.logger.Info("pulled schedule data", "processDate", processDate, "time", time.Since(callStartedAt), "total items", schedules.TotalItems)

	logger := utils.GetLogger(fmt.Sprintf("%s:%s", "PLANNER", channel.ID))

	var adSegments []models.SegmentItem

	if !channel.UseAdEpgAds {
		adSegments, _ = p.loadAdSegments([]string{utils.GetSettings().AdsStreamId})
	}

	// Fetch last sequence and running time from Redis to guarantee sequence & time continuity across days
	lastState := p.getLastSegmentState(ctx, channel.ID)

	processor := &SegmentProcessor{
		planner:         p,
		pipe:            p.rdb.Pipeline(),
		ctx:             ctx,
		nowUnix:         float64(callStartedAt.Unix()),
		sequenceCounter: lastState.Sequence,
		runningTime:     lastState.RunningTime,
		adSegments:      adSegments,
		channelID:       channel.ID,
		horizon:         horizon,
		logger:          logger,
		useEpgAds:       channel.UseAdEpgAds,
		mux:             sync.Mutex{},
	}

	var adsStreamIds []string
	var needLoadAds bool
	for _, item := range schedules.Data {
		if err := ctx.Err(); err != nil {
			return err
		}
		if isEndOfTimeline(processor.horizon, processor.runningTime) {
			break
		}

		if endTime, err := utils.ParseESPXTime(item.LocalDateEnd, channel.ContentTimezone); err == nil && endTime.Before(callStartedAt) {
			continue
		}

		if item.IsAdsScheduleItem() {
			s := item.GetStream()

			if s != nil {
				adsStreamIds = append(adsStreamIds, s.StreamID)
			}
			needLoadAds = true
			continue
		}

		if needLoadAds {
			adSegments, _ = p.loadAdSegments(adsStreamIds)
			processor.adSegments = adSegments
		}

		if err := processor.ProcessScheduleItem(item, channel, callStartedAt, needLoadAds); err != nil {
			if errors.Is(err, ErrEndOfTimeline) {
				break
			}

			return err
		}

		if needLoadAds {
			processor.adSegments = nil
			needLoadAds = false
			adsStreamIds = []string{}
		}
	}

	// Sync the last segment into Redis.
	processor.synchSegmentIntoRedis(ctx, true)

	p.logger.Info(fmt.Sprintf("finished plan for %s", processDate))
	return processor.flush()
}

func (p *Planner) loadAdSegments(streamIds []string) ([]models.SegmentItem, error) {
	adSegments := make([]models.SegmentItem, 0)

	for _, adsStreamId := range streamIds {
		// Try to get cached segments from Redis
		cacheKey := fmt.Sprintf("ad_segments:%s", adsStreamId)
		cached, err := p.rdb.Get(p.ctx, cacheKey).Result()
		if err == nil && cached != "" {
			var cachedSegments []models.SegmentItem
			if err := json.Unmarshal([]byte(cached), &cachedSegments); err == nil {
				adSegments = append(adSegments, cachedSegments...)
				continue
			}
			// If unmarshal fails, fall through to recompute
		} else if err != nil && err != redis.Nil {
			// Log unexpected Redis errors but continue to compute
			// log.Printf("redis error for %s: %v", adsStreamId, err)
		}

		// Cache miss: compute segments
		var adsStreamTs []models.SegmentItem

		adsMasterM3u8Path := fmt.Sprintf("%s/%s/vod.m3u8", p.bucketName, adsStreamId)

		renditions := utils.GetRenditionsByMaster(adsMasterM3u8Path, p.rclone)
		if len(renditions) == 0 {
			return nil, fmt.Errorf("no renditions found for ads master: %s", adsMasterM3u8Path)
		}

		adsM3u8Path := fmt.Sprintf("%s/%s/%s/vod.m3u8", p.bucketName, adsStreamId, renditions[0])
		m3u8Content, err := p.rclone.Cat(adsM3u8Path)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch ads m3u8: %w", err)
		}

		lines := strings.Split(m3u8Content, "\n")
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
				adsStreamTs = append(adsStreamTs, models.SegmentItem{
					Duration:    currentDuration,
					StoragePath: fmt.Sprintf("%s/%s/%s/%s", p.bucketName, adsStreamId, "{rendition}", line),
					PtsStart:    ptsStart,
					PtsEnd:      ptsEnd,
					MediaStart:  mStart,
					MediaEnd:    mEnd,
					Renditions:  renditions,
				})
				currentDuration = 0
			}
		}

		// Store computed segments in Redis cache
		if data, err := json.Marshal(adsStreamTs); err == nil {
			// Set with expiration (e.g., 1 hour). Adjust TTL as needed.
			p.rdb.Set(p.ctx, cacheKey, data, time.Hour)
		}

		adSegments = append(adSegments, adsStreamTs...)
	}

	return adSegments, nil
}

func (p *Planner) getLastSegmentState(ctx context.Context, channelID string) LastSegmentState {
	if p.rdb == nil {
		return LastSegmentState{Sequence: 0, RunningTime: 0}
	}
	items, err := p.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:   timelineKeyFor(channelID),
		Start: 0,
		Stop:  0,
		Rev:   true,
	}).Result()
	if err != nil || len(items) == 0 {
		return LastSegmentState{Sequence: 0, RunningTime: 0}
	}
	var seg models.SegmentItem
	if err := json.Unmarshal([]byte(items[0]), &seg); err == nil {
		return LastSegmentState{
			Sequence:    seg.Sequence,
			RunningTime: seg.MediaEnd,
		}
	}
	return LastSegmentState{Sequence: 0, RunningTime: 0}
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
	return n > 0, nil
}

func (p *Planner) HasLegacyTimeline(ctx context.Context, channelID string) (bool, error) {
	if p.rdb == nil {
		return false, nil
	}
	key := timelineKeyFor(channelID)
	items, err := p.rdb.ZRange(ctx, key, 0, 0).Result()
	if err != nil || len(items) == 0 {
		return false, err
	}
	var probe struct {
		Rendition string `json:"rendition"`
	}
	if err := json.Unmarshal([]byte(items[0]), &probe); err != nil {
		return true, nil
	}
	return probe.Rendition == "", nil
}

func (p *Planner) DeleteTimeline(ctx context.Context, channelID string) error {
	if p.rdb == nil {
		return nil
	}
	return p.rdb.Del(ctx, timelineKeyFor(channelID)).Err()
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
	key := fmt.Sprintf(utils.GetPlannerSettings().FailureCounterKey, channelID)
	n, err := p.rdb.Incr(ctx, key).Result()
	if err != nil {
		log.Printf("[Planner] INCR(%s) error: %v", key, err)
		return 0
	}
	p.rdb.Expire(ctx, key, 24*time.Hour)
	return n
}

func (p *Planner) resetFailureCounter(ctx context.Context, channelID string) {
	key := fmt.Sprintf(utils.GetPlannerSettings().FailureCounterKey, channelID)
	if err := p.rdb.Del(ctx, key).Err(); err != nil {
		log.Printf("[Planner] DEL(%s) error: %v", key, err)
	}
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
			channelID, utils.GetPlannerSettings().FailureInactivateThreshold)
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

func (p *Planner) MarkChannelFailed(channelID string, errorLabel string) {
	if p.db == nil {
		return
	}
	p.db.Model(&models.PlayoutChannel{}).
		Where("id = ?", channelID).
		Update("status", models.PlayoutFailureStatus).Update("label", errorLabel)
}

func (p *Planner) HandlePrepareResultForChannel(ctx context.Context, channelID string, err error) {
	if err == nil {
		p.resetFailureCounter(ctx, channelID)
		if dbErr := p.markChannelLive(channelID); dbErr != nil {
			log.Printf("[Planner] markChannelLive(%s) error: %v", channelID, dbErr)
		}
		return
	}
	count := p.incrFailureCounter(ctx, channelID)
	if count >= utils.GetPlannerSettings().FailureInactivateThreshold {
		if dbErr := p.markChannelInactive(channelID); dbErr != nil {
			log.Printf("[Planner] markChannelInactive(%s) error: %v", channelID, dbErr)
		}
	}
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
	lookahead := nowUnix + utils.GetPlannerSettings().LookaheadSeconds
	p.setCursor(ctx, ch.ID, lookahead)
	return nil
}

func (p *Planner) CleanupOldSegments(ctx context.Context, channelID string) {
	cutoff := fmt.Sprintf("%d", time.Now().Unix()-utils.GetPlannerSettings().RetentionSeconds)
	key := timelineKeyFor(channelID)
	if _, err := p.rdb.ZRemRangeByScore(ctx, key, "-inf", cutoff).Result(); err != nil {
		log.Printf("[Planner] ZREMRANGEBYSCORE(%s, -inf, %s) error: %v", key, cutoff, err)
	}
}

func (p *Planner) ProcessEPGToTimelineWithRetry(ctx context.Context, ch models.PlayoutChannel, dateStr string) error {
	settings := utils.GetPlannerSettings()
	attempts := settings.MaxPrepareRetries
	if attempts < 1 {
		attempts = 1
	}
	base := time.Duration(settings.PrepareRetryBaseMs) * time.Millisecond
	maxDelay := time.Duration(settings.PrepareRetryMaxMs) * time.Millisecond

	var lastErr error
	for i := 1; i <= attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if lastErr = p.ProcessEPGToTimeline(ctx, ch, dateStr); lastErr == nil {
			return nil
		}

		if errors.Is(lastErr, customerrors.UnspportStreamSchema) {
			break
		}

		if i > attempts {
			break
		}
		delay := base * (1 << (i - 1))
		if delay > maxDelay {
			delay = maxDelay
		}
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
	n, _ := p.rdb.ZCard(ctx, timelineKeyFor(channelID)).Result()
	return n
}

func (p *Planner) LastProcessedCursor(ctx context.Context, channelID string) int64 {
	return p.getCursor(ctx, channelID)
}
