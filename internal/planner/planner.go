package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

// ErrNoScheduleData means the provider returned no usable schedule and the
// channel has no retained schedule to fall back to. The caller must treat the
// day as NOT prepared (failure counter / channel status), never as success.
var ErrNoScheduleData = errors.New("no schedule data from provider or retained schedule")

type LastSegmentState struct {
	Sequence    int64
	RunningTime float64
}

type Planner struct {
	db         *gorm.DB
	rdb        *redis.Client
	espx       *api.ESPXClient
	rclone     *storage.RcloneService
	bucketName string
	adsBucket  string
	cacheDir   string

	logger *utils.Logger
}

func NewPlanner(db *gorm.DB, rdb *redis.Client, espx *api.ESPXClient, rclone *storage.RcloneService) *Planner {
	return &Planner{
		db:         db,
		rdb:        rdb,
		espx:       espx,
		rclone:     rclone,
		bucketName: utils.GetSettings().VideoBucket,
		adsBucket:  fmt.Sprintf("%s/%s", utils.GetSettings().VideoBucket, utils.GetSettings().AdsStreamId),
		cacheDir:   utils.GetSettings().EpgCacheDir,
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
		p.logger.Error("[Planner] CleanChannelData error", "channel", channelID, "error", err)
	} else {
		p.logger.Info("[Planner] Cleaned up all Redis data", "channel", channelID)
	}
}

func (p *Planner) ProcessEPGToTimeline(ctx context.Context, channel models.PlayoutChannel, dateStr string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	callStartedAt := time.Now()

	loc := utils.LocalLocation()
	nowLocal := callStartedAt.In(loc)
	processDate := utils.StartOfDay(nowLocal)
	if parsed, err := time.ParseInLocation(configs.DATE_FORMAT_YYYY_MM_DD, dateStr, loc); err == nil {
		processDate = utils.StartOfDay(parsed)
	}
	horizon := utils.EndOfDay(processDate)
	targetDate := processDate.Format(configs.DATE_FORMAT_YYYY_MM_DD)

	logger := utils.GetLogger(fmt.Sprintf("%s:%s", "PLANNER", channel.ID))

	// Convert local timezone to +HH:MM for ESPX
	tzNum := utils.LocalTZOffset()
	schedules, fetchErr := p.espx.GetEPGScheduleItems(channel.ChannelEpgID, dateStr, tzNum)

	var (
		items        []models.ESPXScheduleItem
		fallbackFrom *RetainedSchedule
	)

	if fetchErr == nil && schedules != nil && len(schedules.Data) > 0 {
		items = schedules.Data
		p.logger.Info("pulled schedule data", "processDate", processDate, "time", time.Since(callStartedAt), "total items", schedules.TotalItems)

		// Retain the raw provider payload before any gap-fill rewriting.
		p.saveProviderSchedule(channel, dateStr, items)

		// Temporary gap-fill: duplicate tail/head schedule items so the timeline
		// covers from now until the end of the day. Time fields are reset so the
		// existing processor can handle them. Remove with schedule_pad.go once the
		// real schedule-gap strategy is ready.
		items = padScheduleToCoverDay(items, callStartedAt, horizon, channel.ContentTimezone)
	} else {
		if fetchErr != nil {
			logger.VWarning("provider schedule error for channel=%s date=%s, trying retained schedule: %v", channel.ID, dateStr, fetchErr)
		} else {
			logger.VWarning("provider returned no schedule items for channel=%s date=%s, trying retained schedule", channel.ID, dateStr)
		}

		// FR-007: fallback must never duplicate or rewind existing coverage.
		hasCoverage, err := p.HasDateInTimeline(ctx, channel.ID, processDate, horizon)
		if err != nil {
			return fmt.Errorf("check fallback coverage for %s: %w", channel.ID, err)
		}

		cached, hasRetained := p.loadRetainedSchedule(channel)

		switch decideFallback(hasCoverage, hasRetained) {
		case fallbackSkipCovered:
			logger.Info("target day already covered, skipping fallback generation", "channel", channel.ID, "date", targetDate)
			return nil
		case fallbackUnavailable:
			logger.Error("no schedule data (provider empty, cache empty)", "channel", channel.ID, "date", dateStr)
			return ErrNoScheduleData
		}

		items = buildFallbackSchedule(cached.Items, processDate, horizon, channel.ContentTimezone)
		if len(items) == 0 {
			logger.Error("no schedule data (provider empty, cache usable items empty)", "channel", channel.ID, "date", dateStr, "source_date", cached.SourceDate)
			return ErrNoScheduleData
		}
		fallbackFrom = cached
	}

	if err := p.planScheduleItems(ctx, channel, processDate, horizon, items, fallbackFrom != nil); err != nil {
		return err
	}

	if fallbackFrom != nil {
		// The label must reflect committed coverage: a fallback that produced
		// nothing is a failure, not a prepared day.
		covered, err := p.HasDateInTimeline(ctx, channel.ID, processDate, horizon)
		if err != nil {
			logger.Error("failed to verify fallback coverage", "channel", channel.ID, "date", targetDate, "error", err)
			return err
		}
		if !covered {
			logger.Error("no schedule data (fallback produced no coverage)", "channel", channel.ID, "date", dateStr, "source_date", fallbackFrom.SourceDate)
			return ErrNoScheduleData
		}

		state := FallbackState{
			TargetDate:  targetDate,
			SourceDate:  fallbackFrom.SourceDate,
			SourceEpgID: fallbackFrom.ChannelEpgID,
			DayStart:    processDate.Unix(),
			DayEnd:      horizon.Unix(),
			CreatedAt:   time.Now().Unix(),
		}
		if err := p.MarkFallbackDay(ctx, channel.ID, state); err != nil {
			logger.Error("failed to record fallback state", "channel", channel.ID, "date", targetDate, "error", err)
		}
		logger.Info("fallback used", "channel", channel.ID, "source_date", fallbackFrom.SourceDate, "target_date", targetDate, "items", len(items))
	}

	p.logger.Info(fmt.Sprintf("finished plan for %s", processDate))
	return nil
}

// planScheduleItems walks items through the existing segment processor,
// seeding sequence and running time from the current timeline tail so
// coverage stays continuous. useFallback only affects logging.
func (p *Planner) planScheduleItems(ctx context.Context, channel models.PlayoutChannel, processDate, horizon time.Time, items []models.ESPXScheduleItem, useFallback bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	nowLocal := time.Now().In(utils.LocalLocation())

	var adSegments []models.SegmentItem

	if !channel.UseAdEpgAds {
		adSegments, _ = p.loadAdSegments()
	}

	lastState := p.getLastSegmentState(ctx, channel.ID)

	processor := &SegmentProcessor{
		planner:         p,
		pipe:            p.rdb.Pipeline(),
		ctx:             ctx,
		nowUnix:         float64(nowLocal.Unix()),
		sequenceCounter: lastState.Sequence,
		runningTime:     lastState.RunningTime,
		adSegments:      adSegments,
		channelID:       channel.ID,
		horizon:         horizon,
		logger:          utils.GetLogger(fmt.Sprintf("%s:%s", "PLANNER", channel.ID)),
		useEpgAds:       channel.UseAdEpgAds,
	}

	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if isEndOfTimeline(processor.horizon, processor.runningTime) {
			break
		}

		if err := processor.ProcessScheduleItem(item, channel, nowLocal); err != nil {
			if errors.Is(err, ErrEndOfTimeline) {
				break
			}
			return err
		}
	}

	// Sync the last segment into Redis.
	processor.synchSegmentIntoRedis(ctx, true)

	if useFallback {
		p.logger.Info("planned fallback coverage", "channel", channel.ID, "date", processDate.Format(configs.DATE_FORMAT_YYYY_MM_DD), "next_sequence", processor.sequenceCounter)
	}
	return processor.flush()
}

// RecoverFallbackDay switches a fallback-covered day back to real provider
// data. It reports false when the day is not a fallback day or the provider
// still has nothing for it. Real coverage replaces only the not-yet-played
// part of the timeline, and the marker is cleared only after the replan
// succeeds.
func (p *Planner) RecoverFallbackDay(ctx context.Context, channel models.PlayoutChannel, day time.Time) (bool, error) {
	dayStart := utils.StartOfDay(day.In(utils.LocalLocation()))

	state, ok := p.HasFallbackDay(ctx, channel.ID, dayStart)
	if !ok {
		return false, nil
	}

	dateStr := dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD)
	schedules, err := p.espx.GetEPGScheduleItems(channel.ChannelEpgID, dateStr, utils.LocalTZOffset())
	if err != nil {
		return false, err
	}
	if schedules == nil || len(schedules.Data) == 0 {
		return false, nil
	}

	p.saveProviderSchedule(channel, dateStr, schedules.Data)

	cutoff := recoveryCutoff(time.Now(), dayStart, utils.GetPlannerSettings().WindowBufferSeconds)
	if _, err := p.rdb.ZRemRangeByScore(ctx, timelineKeyFor(channel.ID), fmt.Sprintf("%d", cutoff), "+inf").Result(); err != nil {
		return false, err
	}

	horizon := utils.EndOfDay(dayStart)
	items := padScheduleToCoverDay(schedules.Data, time.Now(), horizon, channel.ContentTimezone)

	if err := p.planScheduleItems(ctx, channel, dayStart, horizon, items, false); err != nil {
		return false, err
	}

	if err := p.ClearFallbackDay(ctx, channel.ID, dayStart); err != nil {
		p.logger.Error("[Planner] failed to clear fallback state", "channel", channel.ID, "date", dateStr, "error", err)
	}
	p.logger.Info("fallback recovered with real data",
		"channel", channel.ID, "date", dateStr, "source_date", state.SourceDate, "cutoff", cutoff, "items", len(schedules.Data))
	return true, nil
}

func (p *Planner) loadAdSegments() ([]models.SegmentItem, error) {
	adsMasterM3u8Path := fmt.Sprintf("%s/vod.m3u8", p.adsBucket)

	renditions := utils.GetRenditionsByMaster(adsMasterM3u8Path, p.rclone)
	if len(renditions) == 0 {
		return nil, fmt.Errorf("no renditions found for ads master: %s", adsMasterM3u8Path)
	}

	adsM3u8Path := fmt.Sprintf("%s/%s/vod.m3u8", p.adsBucket, renditions[0])
	m3u8Content, err := p.rclone.Cat(adsM3u8Path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ads m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	adSegments := make([]models.SegmentItem, 0)
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
				StoragePath: fmt.Sprintf("%s/%s/%s", p.adsBucket, "{rendition}", line),
				PtsStart:    ptsStart,
				PtsEnd:      ptsEnd,
				MediaStart:  mStart,
				MediaEnd:    mEnd,
				Renditions:  renditions,
			})
			currentDuration = 0
		}
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

func (p *Planner) getLastSequenceNumber(ctx context.Context, channelID string) int64 {
	return p.getLastSegmentState(ctx, channelID).Sequence
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
		p.logger.Info("[Planner] INCR(%s) error: %v", key, err)
		return 0
	}
	p.rdb.Expire(ctx, key, 24*time.Hour)
	return n
}

func (p *Planner) resetFailureCounter(ctx context.Context, channelID string) {
	key := fmt.Sprintf(utils.GetPlannerSettings().FailureCounterKey, channelID)
	if err := p.rdb.Del(ctx, key).Err(); err != nil {
		p.logger.Info("[Planner] DEL(%s) error: %v", key, err)
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
		p.logger.Info("[Planner] channel=%s marked INACTIVE after %d consecutive failures",
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
		p.logger.Info("channel marked LIVE", "channel", channelID)
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
			p.logger.Info("[Planner] markChannelLive(%s) error: %v", channelID, dbErr)
		}
		return
	}
	count := p.incrFailureCounter(ctx, channelID)
	if count >= utils.GetPlannerSettings().FailureInactivateThreshold {
		if dbErr := p.markChannelInactive(channelID); dbErr != nil {
			p.logger.Info("[Planner] markChannelInactive(%s) error: %v", channelID, dbErr)
		}
	}
}

func (p *Planner) ResetFailureCounterForChannel(ctx context.Context, channelID string) {
	p.resetFailureCounter(ctx, channelID)
}

func (p *Planner) MarkChannelLiveForChannel(ctx context.Context, channelID string) {
	if err := p.markChannelLive(channelID); err != nil {
		p.logger.Info("[Planner] MarkChannelLiveForChannel(%s) error: %v", channelID, err)
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
		p.logger.Error("ZREMRANGEBYSCORE failed", "key", key, "cutoff", cutoff, "error", err)
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
		p.logger.Info("plan attempt failed", "channel", ch.ID, "date", dateStr, "attempt", i, "of", attempts, "error", lastErr, "sleep", delay)
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
