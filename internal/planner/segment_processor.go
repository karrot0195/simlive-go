package planner

import (
	"context"
	"fmt"
	"simlive/internal/models"
	"simlive/internal/utils"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type SegmentProcessor struct {
	planner         *Planner
	pipe            redis.Pipeliner
	ctx             context.Context
	sequenceCounter int64
	lastAdTime      float64
	totalAdsAdded   int
	nowUnix         float64
	adSegments      []models.SegmentItem

	// PTS mapping state for the current stream
	currentPtsOffset float64
	// Local timeline anchor: MediaStart of the first segment in the timeline.
	// Every segment gets PtsOffset = MediaStart - timelineBase (local seconds).
	timelineBase float64
}

func (sp *SegmentProcessor) ProcessScheduleItem(item models.ESPXScheduleItem, channel models.PlayoutChannel, nowLocal time.Time) error {
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
	if epgStartUnix > sp.planner.globalRunningTime {
		sp.planner.globalRunningTime = epgStartUnix
	}

	// Initialize the first ad insertion marker if not set yet
	if sp.lastAdTime == 0 {
		sp.lastAdTime = sp.planner.globalRunningTime
	}

	return sp.processStreamSegments(streamID)
}

func (sp *SegmentProcessor) processStreamSegments(streamID string) error {
	pair := sp.planner.resolveRenditions(streamID)
	m3u8Path := fmt.Sprintf("%s/%s/%s/vod.m3u8", sp.planner.bucketName, streamID, pair.vod)
	m3u8Content, err := sp.planner.rclone.Cat(m3u8Path)
	if err != nil {
		return fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var currentDuration float64
	var ptsStart, ptsEnd int64
	firstSegmentInStream := true

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
			if sp.planner.isEndOfTimeline(sp.planner.processStartTime, sp.planner.globalRunningTime) {
				return ErrEndOfTimeline
			}

			// 1. Check and insert ads right before the video segment (when scheduled)
			if err := sp.insertAdsIfNeeded(sp.planner.globalRunningTime); err != nil {
				return err
			}

			// Anchor the PTS offset on the current globalRunningTime at the segment start
			if firstSegmentInStream {
				targetPtsStart := sp.planner.globalRunningTime * 90000.0
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

			// If ads were just inserted, globalRunningTime was pushed forward; resync mediaStart for the video
			if sp.planner.globalRunningTime > mediaStart {
				mediaStart = sp.planner.globalRunningTime
				mediaEnd = mediaStart + calculatedDuration
			}

			// 2. Process and write the regular video segment to Redis (single function owns the sequence)
			if err := sp.processSegment(streamID, pair.vod, line, calculatedDuration, ptsStart, ptsEnd, mediaStart, mediaEnd); err != nil {
				return err
			}

			// Advance globalRunningTime by the actual video segment
			sp.planner.globalRunningTime = mediaEnd
		}
	}
	return nil
}

func (sp *SegmentProcessor) processSegment(streamID, vodDir, line string, duration float64, ptsStart, ptsEnd int64, mediaStart, mediaEnd float64) error {
	sp.sequenceCounter++ // Single +1 sequence bump here for video
	segmentPath := fmt.Sprintf("%s/%s/%s/%s", sp.planner.bucketName, streamID, vodDir, line)
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
	}

	if mediaStart >= (sp.nowUnix - WindowBufferSeconds) {
		if err := sp.planner.addSegmentToRedis(sp.pipe, sp.ctx, mediaStart, segment); err != nil {
			return err
		}
		sp.planner.totalSegmentsAdded++
	}

	return nil
}

// setTimelineBase anchors the local timeline on the first segment and returns
// the PtsOffset (local seconds) for the current segment: MediaStart - timelineBase.
func (sp *SegmentProcessor) setTimelineBase(mediaStart float64) float64 {
	if sp.timelineBase == 0 {
		sp.timelineBase = mediaStart
	}
	return mediaStart - sp.timelineBase
}

func (sp *SegmentProcessor) insertAdsIfNeeded(actualStartTime float64) error {
	if len(sp.adSegments) == 0 {
		return nil
	}

	// Check whether 30s (AdInsertionInterval) have elapsed since the last ad insertion
	if actualStartTime < sp.nowUnix || (actualStartTime-sp.lastAdTime) < float64(AdInsertionInterval) {
		return nil
	}

	// Insert the ad segment block into the timeline
	for _, adSeg := range sp.adSegments {
		if sp.planner.isEndOfTimeline(sp.planner.processStartTime, sp.planner.globalRunningTime) {
			return ErrEndOfTimeline
		}

		sp.sequenceCounter++ // Standard +1 sequence bump
		adItem := adSeg
		adItem.Sequence = sp.sequenceCounter

		// Map the ad right after the current globalRunningTime
		adItem.MediaStart = sp.planner.globalRunningTime
		adItem.MediaEnd = sp.planner.globalRunningTime + adItem.Duration
		adItem.StartTime = int64(adItem.MediaStart)
		ptsOffset := sp.setTimelineBase(adItem.MediaStart)
		adItem.PtsOffset = &ptsOffset

		if err := sp.planner.addSegmentToRedis(sp.pipe, sp.ctx, adItem.MediaStart, adItem); err != nil {
			return err
		}

		sp.planner.globalRunningTime = adItem.MediaEnd
		sp.planner.totalSegmentsAdded++
		sp.totalAdsAdded++
	}

	// Update the last ad insertion marker so the next insertion triggers after 30 more seconds
	sp.lastAdTime = sp.planner.globalRunningTime
	return nil
}
