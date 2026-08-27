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
	AdInsertionInterval = 120 // seconds
	RedisExpiryHours    = 26
	WindowBufferSeconds = 120 // seconds before current time to include segments
)

// ErrEndOfTimeline indicates that the end of the timeline has been reached
var ErrEndOfTimeline = errors.New("end of timeline reached - processing complete")

// Planner handles the orchestration of EPG to timeline processing
type Planner struct {
	db         *gorm.DB
	rdb        *redis.Client
	espx       *api.ESPXClient
	rclone     *storage.RcloneService
	bucketName string
	adsBucket  string

	// processing state: Uses float64 to prevent fractional duration truncation (e.g. 3.999s)
	globalRunningTime  float64
	totalSegmentsAdded int64
	redisSyncOffset    int64
	channelID          string
	processStartTime   time.Time
}

// SegmentProcessor handles segment processing logic
type SegmentProcessor struct {
	planner *Planner
	pipe    redis.Pipeliner
	ctx     context.Context

	// processing state
	sequenceCounter int64
	adOffset        float64
	lastAdTime      float64
	totalAdsAdded   int
	nowUnix         float64
	adSegments      []models.SegmentItem
}

// NewPlanner creates a new Planner instance
func NewPlanner(
	db *gorm.DB,
	rdb *redis.Client,
	espx *api.ESPXClient,
	rclone *storage.RcloneService,
) *Planner {
	return &Planner{
		db:         db,
		rdb:        rdb,
		espx:       espx,
		rclone:     rclone,
		bucketName: "espx-video-lib:espx-video-lib",
		adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
	}
}

func (p *Planner) loadAdSegments() ([]models.SegmentItem, error) {
	adsM3u8Path := fmt.Sprintf("%s/L0/vod.m3u8", p.adsBucket)
	m3u8Content, err := p.rclone.Cat(adsM3u8Path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ads m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var adSegments []models.SegmentItem
	var currentDuration float64

	for _, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			if dur, err := strconv.ParseFloat(durStr, 64); err == nil {
				currentDuration = dur
			}
		case strings.HasSuffix(line, ".ts"):
			adSegments = append(adSegments, models.SegmentItem{
				Duration:    currentDuration,
				StoragePath: fmt.Sprintf("%s/L0/%s", p.adsBucket, line),
			})
		}
	}

	if len(adSegments) == 0 {
		return nil, fmt.Errorf("no ad segments found in %s", adsM3u8Path)
	}

	return adSegments, nil
}

func (p *Planner) getRedisKey() string {
	return fmt.Sprintf("simlive:timeline:%s", p.channelID)
}

// getLastSequenceNumber queries existing items in Redis to maintain monotonic sequence IDs
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

func (p *Planner) ProcessEPGToTimeline(
	ctx context.Context,
	channel models.PlayoutChannel,
	dateStr string,
) error {
	p.channelID = channel.ID
	p.processStartTime = time.Now()

	log.Printf("[Planner] Starting processing for channel %s (%s) date: %s",
		channel.Name, channel.ID, dateStr)

	// Fetch schedules
	schedules, err := p.espx.GetSchedules(channel.ChannelEpgID, dateStr, configs.TZ_NUM)
	if err != nil {
		return fmt.Errorf("failed to fetch schedules: %w", err)
	}

	if len(schedules.Data) == 0 {
		log.Printf("[Planner] No schedule items for channel %s", channel.ID)
		return nil
	}

	adSegments, err := p.loadAdSegments()
	if err != nil {
		log.Printf("[Planner] Warning: Could not load ads: %v", err)
		adSegments = []models.SegmentItem{}
	}

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
			log.Printf("[Planner] End of timeline reached, stopping processing")
			break
		}

		if err := processor.processScheduleItem(item, channel, nowLocal); err != nil {
			if err == ErrEndOfTimeline {
				log.Printf("[Planner] End of timeline reached during processing, stopping")
				break
			}
			log.Printf("[Planner] Warning: Failed to process schedule item: %v", err)
			continue
		}
	}

	// Finalize remaining items in Redis pipeline
	if err := p.flushRedisPipeline(processor.pipe); err != nil {
		return fmt.Errorf("failed to finalize Redis operations: %w", err)
	}

	log.Printf("[Planner] Successfully processed %d segments (%d ads) for channel %s",
		p.totalSegmentsAdded, processor.totalAdsAdded, channel.ID)

	return nil
}

func (p *Planner) flushRedisPipeline(pipe redis.Pipeliner) error {
	pipe.Expire(context.Background(), p.getRedisKey(), RedisExpiryHours*time.Hour)

	if _, err := pipe.Exec(context.Background()); err != nil {
		return fmt.Errorf("pipeline execution failed: %w", err)
	}

	return nil
}

func (sp *SegmentProcessor) processScheduleItem(
	item models.ESPXScheduleItem,
	channel models.PlayoutChannel,
	nowLocal time.Time,
) error {
	if sp.planner.isEndOfTimeline(sp.planner.processStartTime, sp.planner.globalRunningTime) {
		return ErrEndOfTimeline
	}

	if len(item.Programme.ProgrammeStreams) == 0 {
		return nil
	}

	streamID := item.Programme.ProgrammeStreams[0].StreamID
	if streamID == "" {
		return nil
	}

	startTime, err := utils.ParseESPXTime(item.LocalDateStart, channel.ContentTimezone)
	if err != nil {
		return fmt.Errorf("failed to parse start time: %w", err)
	}

	endTime, err := utils.ParseESPXTime(item.LocalDateEnd, channel.ContentTimezone)
	if err != nil {
		return fmt.Errorf("failed to parse end time: %w", err)
	}

	if endTime.Before(nowLocal) {
		return nil
	}

	epgStartUnix := float64(startTime.Unix())
	epgWithOffset := epgStartUnix + sp.adOffset
	if epgWithOffset > sp.planner.globalRunningTime {
		sp.planner.globalRunningTime = epgWithOffset
	}

	if sp.lastAdTime == 0 {
		sp.lastAdTime = sp.planner.globalRunningTime
	}

	return sp.processStreamSegments(streamID)
}

func (sp *SegmentProcessor) processStreamSegments(streamID string) error {
	m3u8Path := fmt.Sprintf("%s/%s/L_0/vod.m3u8", sp.planner.bucketName, streamID)
	m3u8Content, err := sp.planner.rclone.Cat(m3u8Path)
	if err != nil {
		return fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var currentDuration float64

	for _, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			if dur, err := strconv.ParseFloat(durStr, 64); err == nil {
				currentDuration = dur
			}
		case strings.HasSuffix(line, ".ts"):
			if sp.planner.isEndOfTimeline(sp.planner.processStartTime, sp.planner.globalRunningTime) {
				return ErrEndOfTimeline
			}

			if err := sp.processSegment(streamID, line, currentDuration); err != nil {
				return err
			}
		}
	}

	return nil
}

func (sp *SegmentProcessor) processSegment(streamID, line string, duration float64) error {
	sp.sequenceCounter++

	segmentPath := fmt.Sprintf("%s/%s/L_0/%s", sp.planner.bucketName, streamID, line)
	actualStartTime := sp.planner.globalRunningTime

	segment := models.SegmentItem{
		Sequence:    sp.sequenceCounter,
		StoragePath: segmentPath,
		Duration:    duration,
		StartTime:   int64(actualStartTime),
	}

	if actualStartTime >= (sp.nowUnix - WindowBufferSeconds) {
		if err := sp.planner.addSegmentToRedis(sp.pipe, sp.ctx, actualStartTime, segment); err != nil {
			return err
		}
		sp.planner.totalSegmentsAdded++
	}

	// Adds exact float duration (e.g. 3.999s) to prevent timeline drift
	sp.planner.globalRunningTime += duration

	if err := sp.insertAdsIfNeeded(actualStartTime); err != nil {
		return err
	}

	return nil
}

func (sp *SegmentProcessor) insertAdsIfNeeded(actualStartTime float64) error {
	if len(sp.adSegments) == 0 {
		return nil
	}

	if actualStartTime < sp.nowUnix || (actualStartTime-sp.lastAdTime) < float64(AdInsertionInterval) {
		return nil
	}

	for _, adSeg := range sp.adSegments {
		if sp.planner.isEndOfTimeline(sp.planner.processStartTime, sp.planner.globalRunningTime) {
			return ErrEndOfTimeline
		}

		sp.sequenceCounter++

		adItem := adSeg
		adItem.Sequence = sp.sequenceCounter
		adItem.StartTime = int64(sp.planner.globalRunningTime)

		if err := sp.planner.addSegmentToRedis(sp.pipe, sp.ctx, sp.planner.globalRunningTime, adItem); err != nil {
			return err
		}

		sp.planner.globalRunningTime += adItem.Duration
		sp.adOffset += adItem.Duration
		sp.planner.totalSegmentsAdded++
		sp.totalAdsAdded++
	}

	sp.lastAdTime = sp.planner.globalRunningTime
	return nil
}

func (p *Planner) addSegmentToRedis(
	pipe redis.Pipeliner,
	ctx context.Context,
	runningTime float64,
	data models.SegmentItem,
) error {
	if p.isEndOfTimeline(p.processStartTime, runningTime) {
		return ErrEndOfTimeline
	}

	data.TimeText = utils.TimeNumToText(data.StartTime)
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal segment: %w", err)
	}

	redisKey := p.getRedisKey()
	pipe.ZAdd(ctx, redisKey, redis.Z{
		Score:  runningTime,
		Member: bytes,
	})

	// Immediate pipeline flush if segment falls into current playout window
	if runningTime <= (float64(time.Now().Unix()) + 120) {
		_ = p.flushBatch(pipe, ctx)
	} else if p.totalSegmentsAdded > 0 && p.totalSegmentsAdded%WindowItemSize == 0 {
		if err := p.flushBatch(pipe, ctx); err != nil {
			return err
		}
	}

	return nil
}

func (p *Planner) isEndOfTimeline(processTime time.Time, runningTime float64) bool {
	endOfDay := utils.EndOfDay(processTime)
	currentRunningTime := time.Unix(int64(runningTime), 0)
	return endOfDay.Before(currentRunningTime)
}

func (p *Planner) flushBatch(pipe redis.Pipeliner, ctx context.Context) error {
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[Planner] Batch flush failed: %v", err)
		return err
	}

	log.Printf("[Planner] Batch flushed: %d items (total=%d, running_time=%s)",
		WindowItemSize, p.totalSegmentsAdded, utils.TimeNumToText(int64(p.globalRunningTime)))

	return nil
}
