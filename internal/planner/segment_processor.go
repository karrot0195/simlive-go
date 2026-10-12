package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"simlive/internal/configs"
	customerrors "simlive/internal/custom-errors"
	"simlive/internal/models"
	"simlive/internal/utils"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var PreRollDelaysSec = []float64{8, 4}
var PreRollCUEINDelaysSec = []float64{4}

const DefaulStreamName = "Default Stream"

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
	// timelineBase     float64

	logger *utils.Logger

	useEpgAds bool
}

func (sp *SegmentProcessor) ProcessScheduleItem(item models.ESPXScheduleItem, channel models.PlayoutChannel, nowLocal time.Time, needLoadAds bool) error {
	if err := sp.ctx.Err(); err != nil {
		return err
	}

	if len(item.Programme.ProgrammeStreams) == 0 {
		return nil
	}
	// get main stream with IsVod=true and IsTrailer=false
	var stream *models.ESPXProgrammeStream // replace ProgrammeStream with your actual type

	for i := range item.Programme.ProgrammeStreams {
		s := &item.Programme.ProgrammeStreams[i]
		if s.IsVod && !s.IsTrailer || s.Name == DefaulStreamName {
			stream = s
			break
		}
	}
	if stream == nil || stream.StreamID == "" {
		return nil
	}

	if stream.SchemaVersion < configs.STREAM_SCHEMA_VERSION {
		sp.logger.Warn("unsport schema ", "StreamID", stream.StreamID)
		return customerrors.UnspportStreamSchema
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

	return sp.processStreamSegments(*stream, needLoadAds)
}

func (sp *SegmentProcessor) processStreamSegments(stream models.ESPXProgrammeStream, needLoadAds bool) error {

	streamRelativePath := stream.StreamPath
	streamAbsolutePath := path.Join(sp.planner.bucketName, streamRelativePath, stream.StreamID)
	masterPath := path.Join(streamAbsolutePath, "vod.m3u8")
	StreamID := stream.StreamID

	if streamRelativePath != "" {
		StreamID = path.Join(streamRelativePath, StreamID)
	}

	upstreamRends := utils.GetRenditionsByMaster(masterPath, sp.planner.rclone)

	primaryRung := "L_0"
	if len(upstreamRends) > 0 {
		primaryRung = upstreamRends[0]
	}

	m3u8Path := fmt.Sprintf("%s/%s.m3u8", streamAbsolutePath, primaryRung)

	if stream.SchemaVersion == 3 {
		m3u8Path = fmt.Sprintf("%s/playout.m3u8", streamAbsolutePath)
	}

	m3u8Content, err := sp.planner.rclone.Cat(m3u8Path)
	if err != nil {
		return fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var ptsStart, ptsEnd int64

	for _, line := range lines {
		if err := sp.ctx.Err(); err != nil {
			return err
		}

		line = strings.TrimSpace(line)
		switch {
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

			var spliceEvent *models.SpliceInsert

			if !sp.useEpgAds {
				sp.processingVerifyNearingAds(sp.runningTime)
				spliceEvent, err = sp.insertAdsIfNeeded(sp.runningTime)
				if err != nil {
					return err
				}
			} else if needLoadAds {
				spliceEvent, err = sp.insertAdsIfNeeded(sp.runningTime)
				if err != nil {
					return err
				}
			}

			segmentPtsOffset := sp.currentPtsOffset

			adjustedPtsStart := float64(ptsStart)
			adjustedPtsEnd := float64(ptsEnd)
			calculatedDuration := (adjustedPtsEnd - adjustedPtsStart) / configs.TICKS

			mediaStart := sp.runningTime
			mediaEnd := sp.runningTime + calculatedDuration

			if err := sp.processSegment(StreamID, upstreamRends, line, segmentPtsOffset, calculatedDuration, ptsStart, ptsEnd, mediaStart, mediaEnd, spliceEvent); err != nil {
				return err
			}

			sp.runningTime = mediaEnd
			sp.currentPtsOffset = sp.currentPtsOffset + calculatedDuration
		}
	}
	return nil
}

func (sp *SegmentProcessor) processSegment(streamID string, renditions []string, line string, ptsOffset float64, duration float64, ptsStart, ptsEnd int64, mediaStart, mediaEnd float64, spliceEvent *models.SpliceInsert) error {
	sp.sequenceCounter++
	segmentPath := fmt.Sprintf("%s/%s/%s/%s", sp.planner.bucketName, streamID, "{rendition}", line)
	segment := models.SegmentItem{
		Sequence:    sp.sequenceCounter,
		StoragePath: segmentPath,
		Duration:    duration,
		StartTime:   int64(mediaStart),
		PtsStart:    ptsStart,
		PtsEnd:      ptsEnd,
		MediaStart:  mediaStart,
		MediaEnd:    mediaEnd,
		PtsOffset:   ptsOffset,
		Renditions:  renditions,
	}

	if spliceEvent != nil {
		segment.AdType = "in"
		segment.AdBreakID = spliceEvent.EventID
		appendSpliceInsertUnique(&segment, *spliceEvent)
	}

	if mediaStart >= (sp.nowUnix - float64(utils.GetPlannerSettings().WindowBufferSeconds)) {
		if err := sp.addSegment(sp.ctx, mediaStart, segment); err != nil {
			return err
		}
		sp.totalSegmentsAdded++
	}

	return nil
}

func (sp *SegmentProcessor) processingVerifyNearingAds(actualStartTime float64) {
	interval := utils.GetPlannerSettings().AdInsertionInterval
	if !sp.planner.isBlockingProcessing && (actualStartTime-sp.lastAdTime) > float64(interval-12) {
		sp.planner.isBlockingProcessing = true
	} else if sp.planner.isBlockingProcessing && (actualStartTime-sp.lastAdTime) > 8 {
		sp.planner.isBlockingProcessing = false
	}
}

func (sp *SegmentProcessor) insertAdsIfNeeded(actualStartTime float64) (*models.SpliceInsert, error) {
	interval := utils.GetPlannerSettings().AdInsertionInterval

	if !sp.useEpgAds && len(sp.adSegments) == 0 || actualStartTime < sp.nowUnix || (actualStartTime-sp.lastAdTime) < float64(interval) {
		return nil, nil
	}

	breakStart := sp.runningTime
	currentPtsOffset := sp.currentPtsOffset
	breakDuration := totalBreakDuration(sp.adSegments)

	for _, delay := range PreRollDelaysSec {
		preRollPts := breakStart - delay
		preRollPtsOffset := currentPtsOffset - delay
		if preRollPts < 0 {
			continue
		}
		idx, ok := findSegmentContainingPts(sp.planner.processingSegments, preRollPts)
		if !ok {
			continue
		}
		ev := models.SpliceInsert{
			Type:               "out",
			EventID:            (sp.planner.adsId),
			SpliceTimePTS:      int64(preRollPtsOffset * 90000.0),
			SpliceTimeSeconds:  preRollPts,
			Duration:           breakDuration,
			UniqueProgramID:    (sp.planner.adsId),
			SegmentationTypeID: configs.SEG_TYPE_PLACE_OPPORTUNITY_START,
		}
		sp.planner.processingSegments[idx].AdBreakID = sp.planner.adsId
		appendSpliceInsertUnique(&sp.planner.processingSegments[idx], ev)
	}

	breakEnd := breakStart + breakDuration

	for i, adSeg := range sp.adSegments {
		if err := sp.ctx.Err(); err != nil {
			return nil, err
		}
		if isEndOfTimeline(sp.horizon, sp.runningTime) {
			return nil, ErrEndOfTimeline
		}

		sp.sequenceCounter++
		currentPtsOffset = sp.currentPtsOffset
		adItem := adSeg
		adItem.Sequence = sp.sequenceCounter

		adItem.MediaStart = sp.runningTime
		adItem.MediaEnd = sp.runningTime + adItem.Duration
		adItem.StartTime = int64(adItem.MediaStart)
		adItem.PtsOffset = currentPtsOffset

		adItem.IsAd = true
		adItem.AdBreakID = sp.planner.adsId
		adItem.AdBreakStart = breakStart
		adItem.AdBreakDuration = breakDuration

		if i == 0 {
			adItem.AdType = "out"
			appendSpliceInsertUnique(&adItem, models.SpliceInsert{
				Type:               "out",
				EventID:            (sp.planner.adsId),
				SpliceTimePTS:      int64(breakStart * 90000.0),
				SpliceTimeSeconds:  breakStart,
				Duration:           breakDuration,
				UniqueProgramID:    (sp.planner.adsId),
				SegmentationTypeID: configs.SEG_TYPE_ADVERTISEMENT_START,
			})
		}

		if err := sp.addSegment(sp.ctx, adItem.MediaStart, adItem); err != nil {
			return nil, err
		}

		sp.runningTime = adItem.MediaEnd
		sp.currentPtsOffset = sp.currentPtsOffset + adItem.Duration
		sp.totalSegmentsAdded++
		sp.totalAdsAdded++
	}
	// inject preroll cue in event
	for _, delay := range PreRollCUEINDelaysSec {
		preRollPts := breakEnd - delay
		preRollPtsOffset := sp.currentPtsOffset - delay

		if preRollPts < 0 {
			continue
		}

		idx, ok := findSegmentContainingPts(sp.planner.processingSegments, preRollPts)
		if !ok {
			continue
		}
		ev := models.SpliceInsert{
			Type:               "in",
			EventID:            (sp.planner.adsId),
			SpliceTimePTS:      int64(preRollPtsOffset * 90000.0),
			SpliceTimeSeconds:  preRollPts,
			Duration:           breakDuration,
			UniqueProgramID:    (sp.planner.adsId),
			SegmentationTypeID: configs.SEG_TYPE_PLACE_OPPORTUNITY_END,
		}
		sp.planner.processingSegments[idx].AdBreakID = sp.planner.adsId
		appendSpliceInsertUnique(&sp.planner.processingSegments[idx], ev)
	}

	// return CUE in for inject to next segment
	cueOutEvent := &models.SpliceInsert{
		Type:               "in",
		EventID:            (sp.planner.adsId),
		SpliceTimePTS:      int64(breakEnd * 90000.0),
		SpliceTimeSeconds:  breakEnd,
		UniqueProgramID:    (sp.planner.adsId),
		SegmentationTypeID: configs.SEG_TYPE_ADVERTISEMENT_END,
		Duration:           breakDuration,
	}

	sp.lastAdTime = sp.runningTime
	sp.planner.adsId++
	return cueOutEvent, nil
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
		sp.synchSegmentIntoRedis(ctx, false)
	}
	return nil
}

func (sp *SegmentProcessor) synchSegmentIntoRedis(ctx context.Context, synchAllItem bool) {
	if err := ctx.Err(); err != nil {
		return
	}

	sizeItems := len(sp.planner.processingSegments)
	if sizeItems == 0 {
		return // Guard against empty slice panics
	}

	// Determine items to process based on synchAllItem flag
	itemsToProcess := sp.planner.processingSegments
	if !synchAllItem && sizeItems > 5 {
		itemsToProcess = sp.planner.processingSegments[sizeItems-5:]
	}

	for _, segment := range itemsToProcess {
		_, err := json.Marshal(segment)
		if err != nil {
			log.Printf("[Planner] json.Marshal(SegmentItem) failed: %v", err)
			continue
		}

		if utils.IsDev() {
			t := time.Unix(int64(segment.RunningTime), 0)

			sp.logger.VInfo("[Sequence %d] %s IsAd=%t ptsOffset=%f %s",
				segment.Sequence, t.Format(time.TimeOnly), segment.IsAd, segment.PtsOffset, segment.TimeText)
		}

		sp.pipe.ZAdd(ctx, sp.timelineKey(), redis.Z{
			Score: float64(segment.RunningTime),
		})
	}

	processCount := len(itemsToProcess)
	sp.logger.VInfo(`synched data from %s to %s with total=%d`,
		itemsToProcess[0].TimeText,
		itemsToProcess[processCount-1].TimeText,
		processCount,
	)

	if _, err := sp.pipe.Exec(ctx); err != nil {
		log.Printf("[Planner] synchSegmentIntoRedis Exec error: %v", err)
	}

	// Retain last 5 items in memory if synchAllItem is false, otherwise clear all
	if !synchAllItem && sizeItems > 5 {
		sp.planner.processingSegments = sp.planner.processingSegments[sizeItems-5:]
	} else if synchAllItem {
		sp.planner.processingSegments = nil
	}
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
