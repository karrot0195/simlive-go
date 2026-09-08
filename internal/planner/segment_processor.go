package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"simlive/internal/models"
	"simlive/internal/utils"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	SegTypePlacementOpportunityStart = 0x30
	SegTypeAdvertisementStart        = 0x34
	SegTypeAdvertisementEnd          = 0x35
	PTS_DURATION                     = (1001.0 / 24000.0) * 90000.0
)

var PreRollDelaysSec = []float64{8, 4}

type SegmentProcessor struct {
	planner         *Planner
	pipe            redis.Pipeliner
	ctx             context.Context
	sequenceCounter int64
	lastAdTime      float64
	totalAdsAdded   int
	nowUnix         float64
	adSegments      []models.SegmentItem

	runningTime        float64
	totalSegmentsAdded int64
	channelID          string
	horizon            time.Time

	currentPtsOffset float64
	timelineBase     float64

	logger utils.Logger
}

func (sp *SegmentProcessor) ProcessScheduleItem(item models.ESPXScheduleItem, channel models.PlayoutChannel, nowLocal time.Time) error {
	if err := sp.ctx.Err(); err != nil {
		return err
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
		return err
	}
	if endTime, err := utils.ParseESPXTime(item.LocalDateEnd, channel.ContentTimezone); err == nil && endTime.Before(nowLocal) {
		return nil
	}

	epgStartUnix := float64(startTime.Unix())
	if epgStartUnix > sp.runningTime {
		sp.runningTime = epgStartUnix
	}

	if sp.lastAdTime == 0 {
		sp.lastAdTime = sp.runningTime
	}

	return sp.processStreamSegments(streamID)
}

func (sp *SegmentProcessor) processStreamSegments(streamID string) error {
	masterPath := fmt.Sprintf("%s/%s/vod.m3u8", sp.planner.bucketName, streamID)
	upstreamRends := utils.GetRenditionsByMaster(masterPath, sp.planner.rclone)

	primaryRung := "L_0"
	if len(upstreamRends) > 0 {
		primaryRung = upstreamRends[0]
	}

	m3u8Path := fmt.Sprintf("%s/%s/%s/vod.m3u8", sp.planner.bucketName, streamID, primaryRung)
	m3u8Content, err := sp.planner.rclone.Cat(m3u8Path)
	if err != nil {
		return fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var currentDuration float64
	var ptsStart, ptsEnd int64
	firstSegmentInStream := true

	for _, line := range lines {
		if err := sp.ctx.Err(); err != nil {
			return err
		}

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
			if isEndOfTimeline(sp.horizon, sp.runningTime) {
				return ErrEndOfTimeline
			}

			sp.processingVerifyNearingAds(sp.runningTime)

			if err := sp.insertAdsIfNeeded(sp.runningTime); err != nil {
				return err
			}

			if firstSegmentInStream {
				targetPtsStart := sp.runningTime * 90000.0
				sp.currentPtsOffset = targetPtsStart - float64(ptsStart)
				firstSegmentInStream = false
			}

			adjustedPtsStart := float64(ptsStart) + sp.currentPtsOffset
			adjustedPtsEnd := float64(ptsEnd) + sp.currentPtsOffset

			mediaStart := adjustedPtsStart / 90000.0
			mediaEnd := adjustedPtsEnd / 90000.0
			calculatedDuration := mediaEnd - mediaStart
			if calculatedDuration <= 0 {
				calculatedDuration = currentDuration
			}

			if sp.runningTime > mediaStart {
				mediaStart = sp.runningTime
				mediaEnd = mediaStart + calculatedDuration
			}

			if err := sp.processSegment(streamID, upstreamRends, line, calculatedDuration, ptsStart, ptsEnd, mediaStart, mediaEnd); err != nil {
				return err
			}

			sp.runningTime = mediaEnd
		}
	}
	return nil
}

func (sp *SegmentProcessor) processSegment(streamID string, renditions []string, line string, duration float64, ptsStart, ptsEnd int64, mediaStart, mediaEnd float64) error {
	sp.sequenceCounter++
	segmentPath := fmt.Sprintf("%s/%s/%s/%s", sp.planner.bucketName, streamID, "{rendition}", line)
	ptsOffset := sp.setTimelineBase(mediaStart)

	segment := models.SegmentItem{
		Sequence:    sp.sequenceCounter,
		StoragePath: segmentPath,
		Duration:    duration,
		StartTime:   int64(mediaStart),
		PtsStart:    ptsStart,
		PtsEnd:      ptsEnd,
		MediaStart:  mediaStart,
		MediaEnd:    mediaEnd,
		PtsOffset:   &ptsOffset,
		Renditions:  renditions,
	}

	if mediaStart >= (sp.nowUnix - float64(utils.GetPlannerSettings().WindowBufferSeconds)) {
		if err := sp.addSegment(sp.ctx, mediaStart, segment); err != nil {
			return err
		}
		sp.totalSegmentsAdded++
	}

	return nil
}

func (sp *SegmentProcessor) setTimelineBase(mediaStart float64) float64 {
	if sp.timelineBase == 0 {
		sp.timelineBase = mediaStart
	}
	return mediaStart - sp.timelineBase
}

func (sp *SegmentProcessor) processingVerifyNearingAds(actualStartTime float64) {
	interval := utils.GetPlannerSettings().AdInsertionInterval
	if (actualStartTime - sp.lastAdTime) > float64(interval-12) {
		sp.planner.isBlockingProcessing = true
	} else if sp.planner.isBlockingProcessing && (actualStartTime-sp.lastAdTime) > 8 {
		sp.planner.isBlockingProcessing = false
	}
}

func (sp *SegmentProcessor) insertAdsIfNeeded(actualStartTime float64) error {
	interval := utils.GetPlannerSettings().AdInsertionInterval
	if len(sp.adSegments) == 0 || actualStartTime < sp.nowUnix || (actualStartTime-sp.lastAdTime) < float64(interval) {
		return nil
	}

	breakStart := sp.runningTime
	breakDuration := totalBreakDuration(sp.adSegments)
	breakID := fmt.Sprintf("adbreak-%d", int64(breakStart))

	for _, delay := range PreRollDelaysSec {
		preRollPts := breakStart - delay
		if preRollPts < 0 {
			continue
		}
		idx, ok := findSegmentContainingPts(sp.planner.processingSegments, preRollPts)
		if !ok {
			continue
		}
		ev := models.SpliceInsert{
			Type:               "out",
			EventID:            int64(sp.planner.adsId),
			SpliceTimePTS:      int64(preRollPts * 90000.0),
			SpliceTimeSeconds:  preRollPts,
			Duration:           breakDuration,
			UniqueProgramID:    int64(sp.planner.adsId),
			SegmentationTypeID: SegTypePlacementOpportunityStart,
		}
		appendSpliceInsertUnique(&sp.planner.processingSegments[idx], ev)
	}

	breakEnd := breakStart + breakDuration

	for i, adSeg := range sp.adSegments {
		if err := sp.ctx.Err(); err != nil {
			return err
		}
		if isEndOfTimeline(sp.horizon, sp.runningTime) {
			return ErrEndOfTimeline
		}

		sp.sequenceCounter++
		adItem := adSeg
		adItem.Sequence = sp.sequenceCounter

		adItem.MediaStart = sp.runningTime
		adItem.MediaEnd = sp.runningTime + adItem.Duration
		adItem.StartTime = int64(adItem.MediaStart)
		ptsOffset := sp.setTimelineBase(adItem.MediaStart)
		adItem.PtsOffset = &ptsOffset

		adItem.IsAd = true
		adItem.AdBreakID = breakID
		adItem.AdBreakStart = breakStart
		adItem.AdBreakDuration = breakDuration

		if i == 0 {
			appendSpliceInsertUnique(&adItem, models.SpliceInsert{
				Type:               "out",
				EventID:            int64(sp.planner.adsId),
				SpliceTimePTS:      int64(breakStart * 90000.0),
				SpliceTimeSeconds:  breakStart,
				Duration:           breakDuration,
				UniqueProgramID:    int64(sp.planner.adsId),
				SegmentationTypeID: SegTypeAdvertisementStart,
			})
		}

		if i == len(sp.adSegments)-1 {
			appendSpliceInsertUnique(&adItem, models.SpliceInsert{
				Type:               "in",
				EventID:            int64(sp.planner.adsId),
				SpliceTimePTS:      int64(breakEnd * 90000.0),
				SpliceTimeSeconds:  breakEnd,
				UniqueProgramID:    int64(sp.planner.adsId),
				SegmentationTypeID: SegTypeAdvertisementEnd,
				Duration:           breakDuration,
			})
		}

		if err := sp.addSegment(sp.ctx, adItem.MediaStart, adItem); err != nil {
			return err
		}

		sp.runningTime = adItem.MediaEnd
		sp.totalSegmentsAdded++
		sp.totalAdsAdded++
	}

	sp.lastAdTime = sp.runningTime
	sp.planner.adsId++
	return nil
}

func (sp *SegmentProcessor) timelineKey() string {
	return timelineKeyFor(sp.channelID)
}

func (sp *SegmentProcessor) flush() error {
	if err := sp.ctx.Err(); err != nil {
		return err
	}
	sp.pipe.Expire(sp.ctx, sp.timelineKey(), time.Duration(utils.GetPlannerSettings().RedisExpiryHours)*time.Hour)
	_, err := sp.pipe.Exec(sp.ctx)
	return err
}

func (sp *SegmentProcessor) addSegment(ctx context.Context, runningTime float64, data models.SegmentItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if isEndOfTimeline(sp.horizon, runningTime) {
		return ErrEndOfTimeline
	}

	data.TimeText = utils.TimeNumToText(data.StartTime)
	data.RunningTime = runningTime
	sp.planner.processingSegments = append(sp.planner.processingSegments, data)

	if sp.planner.isBlockingProcessing {
		return nil
	}

	if runningTime <= (float64(time.Now().Unix())+float64(utils.GetPlannerSettings().WindowBufferSeconds)) || (sp.totalSegmentsAdded > 0 && sp.totalSegmentsAdded%int64(utils.GetPlannerSettings().WindowItemSize) == 0) {
		sp.synchSegmentIntoRedis(ctx)
	}
	return nil
}

func (sp *SegmentProcessor) synchSegmentIntoRedis(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	for _, segment := range sp.planner.processingSegments {
		member, err := json.Marshal(segment)
		if err != nil {
			log.Printf("[Planner] json.Marshal(SegmentItem) failed: %v", err)
			continue
		}
		sp.pipe.ZAdd(ctx, sp.timelineKey(), redis.Z{
			Score:  float64(segment.RunningTime),
			Member: string(member),
		})
	}
	if _, err := sp.pipe.Exec(ctx); err != nil {
		log.Printf("[Planner] synchSegmentIntoRedis Exec error: %v", err)
	}
	sp.planner.processingSegments = nil
}

func totalBreakDuration(ads []models.SegmentItem) float64 {
	total := 0.0
	for _, a := range ads {
		total += a.Duration
	}
	return total
}

func findSegmentContainingPts(segs []models.SegmentItem, ptsSec float64) (int, bool) {
	for i, s := range segs {
		if ptsSec >= s.MediaStart && (ptsSec < s.MediaEnd || (i+1 < len(segs) && ptsSec < segs[i+1].MediaStart)) {
			return i, true
		}
	}
	return -1, false
}

func appendSpliceInsertUnique(target *models.SegmentItem, ev models.SpliceInsert) {
	if target == nil {
		return
	}
	for _, existing := range target.SliceInserts {
		if existing.Type == ev.Type && existing.SpliceTimePTS == ev.SpliceTimePTS {
			return
		}
	}
	target.SliceInserts = append(target.SliceInserts, ev)
}
