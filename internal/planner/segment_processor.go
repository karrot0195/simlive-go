package planner

import (
	"context"
	"encoding/json"
	"fmt"
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

// streamStorageBase returns the bucket-inclusive storage base path for a
// stream. The XML EPG's stream_id already carries the storage bucket (rclone
// remote:bucket/... form); StreamPath is prepended only for legacy
// JSON-sourced streams.
func streamStorageBase(stream models.PartnerProgrammeStream) string {
	if stream.StreamPath != "" {
		return path.Join(stream.StreamPath, stream.StreamID)
	}
	return stream.StreamID
}

// segmentStoragePath renders the engine storage path template for one content
// segment; streamID already includes the storage bucket.
func segmentStoragePath(streamID, line string) string {
	return fmt.Sprintf("%s/{rendition}/%s", streamID, line)
}

// adBreakContext describes one EPG-declared ad programme as a break on the
// timeline. The id is derived from the programme start so it is stable across
// planner replans.
type adBreakContext struct {
	id       uint32
	start    float64
	duration float64
}

// adBreakForProgramme returns the ad-break context for an EPG programme flagged
// as ads, or nil for regular content. Ads are now driven solely by the EPG
// (programme.is_ads); interval-based insertion has been removed.
func adBreakForProgramme(isAds bool, startUnix, endUnix float64) *adBreakContext {
	if !isAds {
		return nil
	}
	duration := endUnix - startUnix
	if duration < 0 {
		duration = 0
	}
	return &adBreakContext{
		id:       uint32(int64(startUnix)),
		start:    startUnix,
		duration: duration,
	}
}

// applyAdMarker stamps the break identity on a segment and, on the first and
// last segments of the break, emits the SCTE-35 cue-out / cue-in boundaries.
func applyAdMarker(seg *models.SegmentItem, ctx *adBreakContext, isFirst, isLast bool) {
	if seg == nil || ctx == nil {
		return
	}
	seg.IsAd = true
	seg.AdBreakID = ctx.id
	seg.AdBreakStart = ctx.start
	seg.AdBreakDuration = ctx.duration

	if isFirst {
		seg.AdType = "out"
		appendSpliceInsertUnique(seg, models.SpliceInsert{
			Type:               "out",
			EventID:            ctx.id,
			SpliceTimePTS:      int64(ctx.start * configs.TICKS),
			SpliceTimeSeconds:  ctx.start,
			Duration:           ctx.duration,
			UniqueProgramID:    ctx.id & 0xFFFF,
			SegmentationTypeID: configs.SEG_TYPE_ADVERTISEMENT_START,
		})
	}
	if isLast {
		seg.AdType = "in"
		breakEnd := ctx.start + ctx.duration
		appendSpliceInsertUnique(seg, models.SpliceInsert{
			Type:               "in",
			EventID:            ctx.id,
			SpliceTimePTS:      int64(breakEnd * configs.TICKS),
			SpliceTimeSeconds:  breakEnd,
			Duration:           ctx.duration,
			UniqueProgramID:    ctx.id & 0xFFFF,
			SegmentationTypeID: configs.SEG_TYPE_ADVERTISEMENT_END,
		})
	}
}

type SegmentProcessor struct {
	planner            *Planner
	pipe               redis.Pipeliner
	ctx                context.Context
	sequenceCounter    int64
	nowUnix            float64
	processingSegments []models.SegmentItem

	runningTime        float64
	totalSegmentsAdded int64
	channelID          string
	horizon            time.Time

	currentPtsOffset float64

	logger *utils.Logger
}

// anchorTimelineStart picks where the clock starts for one schedule item.
// Normally that is the item's own start, or wherever the previous item left
// off. When the item is already in progress on a fresh plan, the clock starts
// at the live edge instead: walking the item from its beginning would emit
// segments that are already past, drop them, and leave a hole until the asset
// runs out.
func anchorTimelineStart(epgStart float64, itemEnd time.Time, nowLocal time.Time, running float64) float64 {
	start := running
	if epgStart > start {
		start = epgStart
	}
	liveStart := float64(nowLocal.Unix() - int64(utils.GetPlannerSettings().WindowBufferSeconds))
	if liveStart > start && (itemEnd.IsZero() || liveStart < float64(itemEnd.Unix())) {
		start = liveStart
	}
	return start
}

func (sp *SegmentProcessor) ProcessScheduleItem(item models.PartnerScheduleItem, channel models.PlayoutChannel, nowLocal time.Time) error {
	if err := sp.ctx.Err(); err != nil {
		return err
	}

	if len(item.Programme.ProgrammeStreams) == 0 {
		return nil
	}
	// get main stream with IsVod=true and IsTrailer=false
	var stream *models.PartnerProgrammeStream

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

	startTime, err := utils.ParseLocalTime(item.LocalDateStart)
	if err != nil {
		return err
	}
	var endTime time.Time
	if et, err := utils.ParseLocalTime(item.LocalDateEnd); err == nil {
		endTime = et
		if endTime.Before(nowLocal) {
			return nil
		}
	}

	epgStartUnix := float64(startTime.Unix())
	sp.runningTime = anchorTimelineStart(epgStartUnix, endTime, nowLocal, sp.runningTime)

	// Ads come from EPG programmes flagged as ads (interval insertion removed)
	// and from inline <ads/> markers in the XML EPG.
	adCtx := adBreakForProgramme(item.Programme.IsAds, float64(startTime.Unix()), float64(endTime.Unix()))

	itemEnd := 0.0
	if !endTime.IsZero() {
		itemEnd = float64(endTime.Unix())
	}
	return sp.processStreamSegments(*stream, adCtx, item.Programme.Ads, itemEnd)
}

func (sp *SegmentProcessor) processAdBreak(ad models.PartnerAdBreak, adCtx *adBreakContext) error {
	// Advance to the ad's scheduled start, never backwards: the timeline is a
	// forward-only clock, and rewinding it would punch a hole or duplicate
	// coverage around the break.
	if adCtx.start > sp.runningTime {
		sp.runningTime = adCtx.start
	}

	masterPath := path.Join(ad.Path, "vod.m3u8")
	upstreamRends := utils.GetRenditionsByMaster(masterPath, sp.planner.rclone)
	primaryRung := "L0"
	if len(upstreamRends) > 0 {
		primaryRung = upstreamRends[0]
	}
	m3u8Path := fmt.Sprintf("%s/%s/vod.m3u8", ad.Path, primaryRung)
	m3u8Content, err := sp.planner.rclone.Cat(m3u8Path)
	if err != nil {
		// no restore
		return fmt.Errorf("failed to fetch ad m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	type segSpec struct {
		line             string
		ptsStart, ptsEnd int64
		duration         float64
	}
	var specs []segSpec
	var ptsStart, ptsEnd int64
	for _, line := range lines {
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
			calculatedDuration := (float64(ptsEnd) - float64(ptsStart)) / configs.TICKS
			specs = append(specs, segSpec{line: line, ptsStart: ptsStart, ptsEnd: ptsEnd, duration: calculatedDuration})
		}
	}
	if len(specs) == 0 {
		// no restore
		return nil
	}

	// stamp pre-roll cues not needed for ads; process segments
	for i, s := range specs {
		if isEndOfTimeline(sp.horizon, sp.runningTime) {
			// no restore
			return ErrEndOfTimeline
		}
		segmentPtsOffset := sp.currentPtsOffset
		mediaStart := sp.runningTime
		mediaEnd := sp.runningTime + s.duration
		isFirst := i == 0
		isLast := i == len(specs)-1

		segmentPath := fmt.Sprintf("%s/%s", ad.Path, s.line)
		// StoragePath template expected by engine: {bucket}/{path}/{rendition}/file
		// ad.Path already includes bucket, so use it directly with {rendition} placeholder.
		segmentPath = fmt.Sprintf("%s/{rendition}/%s", ad.Path, s.line)

		segment := models.SegmentItem{
			Sequence:    0, // set below
			StoragePath: segmentPath,
			Duration:    s.duration,
			StartTime:   int64(mediaStart),
			PtsStart:    s.ptsStart,
			PtsEnd:      s.ptsEnd,
			MediaStart:  mediaStart,
			MediaEnd:    mediaEnd,
			PtsOffset:   segmentPtsOffset,
			Renditions:  upstreamRends,
		}
		sp.sequenceCounter++
		segment.Sequence = sp.sequenceCounter
		applyAdMarker(&segment, adCtx, isFirst, isLast)

		if mediaStart >= (sp.nowUnix - float64(utils.GetPlannerSettings().WindowBufferSeconds)) {
			if err := sp.addSegment(sp.ctx, mediaStart, segment); err != nil {
				// no restore
				return err
			}
			sp.totalSegmentsAdded++
		}
		sp.runningTime = mediaEnd
		sp.currentPtsOffset = sp.currentPtsOffset + s.duration
	}
	// leave runningTime at ad end (do not restore) so VOD resumes after ad
	return nil
}

func (sp *SegmentProcessor) processStreamSegments(stream models.PartnerProgrammeStream, adCtx *adBreakContext, ads []models.PartnerAdBreak, itemEnd float64) error {
	// stream.StreamID is the bucket-inclusive storage path from the XML EPG
	// (rclone remote:bucket/...); StreamPath only applies to legacy
	// JSON-sourced streams.
	streamID := streamStorageBase(stream)
	masterPath := path.Join(streamID, "vod.m3u8")

	upstreamRends := utils.GetRenditionsByMaster(masterPath, sp.planner.rclone)

	primaryRung := "L_0"
	if len(upstreamRends) > 0 {
		primaryRung = upstreamRends[0]
	}

	m3u8Path := fmt.Sprintf("%s/%s.m3u8", streamID, primaryRung)

	if stream.SchemaVersion == 3 {
		m3u8Path = fmt.Sprintf("%s/playout.m3u8", streamID)
	}

	m3u8Content, err := sp.planner.rclone.Cat(m3u8Path)
	if err != nil {
		return fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")

	// First pass: collect the segment descriptors so the first/last ad segment
	// of an EPG ad programme can be identified for cue-out / cue-in boundaries.
	type segSpec struct {
		line             string
		ptsStart, ptsEnd int64
		duration         float64
	}
	var specs []segSpec
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
			calculatedDuration := (float64(ptsEnd) - float64(ptsStart)) / configs.TICKS
			specs = append(specs, segSpec{line: line, ptsStart: ptsStart, ptsEnd: ptsEnd, duration: calculatedDuration})
		}
	}

	if len(specs) == 0 {
		return nil
	}

	// Pre-roll cues on the preceding content segment when an ad break starts.
	if adCtx != nil {
		sp.stampPreRollCues(adCtx)
	}

	// Prepare sorted ad list.
	type adInfo struct {
		start float64
		ad    models.PartnerAdBreak
	}
	var adList []adInfo
	for _, ad := range ads {
		adStart, err := time.Parse(configs.PARTNER_XML_TIME_LAYOUT, ad.Start)
		if err != nil {
			continue
		}
		// Never replay a break whose scheduled start is already behind the
		// clock; it is past content, not a fill for the current slot.
		if float64(adStart.Unix()) < sp.runningTime {
			continue
		}
		adList = append(adList, adInfo{start: float64(adStart.Unix()), ad: ad})
	}
	// sort by start
	// simple insertion sort
	for a := 1; a < len(adList); a++ {
		for b := a; b > 0 && adList[b-1].start > adList[b].start; b-- {
			adList[b-1], adList[b] = adList[b], adList[b-1]
		}
	}

	i := 0
	adIdx := 0
	for i < len(specs) || adIdx < len(adList) {
		if isEndOfTimeline(sp.horizon, sp.runningTime) {
			return ErrEndOfTimeline
		}

		// Stop at the programme boundary so content cannot overrun the next
		// programme's slot and drift the whole timeline off the EPG grid.
		if itemEnd > 0 && sp.runningTime >= itemEnd {
			break
		}

		// Inject ad if due.
		if adIdx < len(adList) && (i >= len(specs) || adList[adIdx].start <= sp.runningTime) {
			ad := adList[adIdx].ad
			adStart := adList[adIdx].start
			if i >= len(specs) && adStart > sp.runningTime {
				// Content is exhausted but the programme's slot still has time:
				// play the remaining breaks back-to-back instead of jumping
				// forward to their wall-clock start and leaving a hole.
				adStart = sp.runningTime
			}
			adCtx2 := &adBreakContext{
				id:       uint32(adList[adIdx].start),
				start:    adStart,
				duration: float64(ad.Duration),
			}
			if err := sp.processAdBreak(ad, adCtx2); err != nil {
				sp.logger.Warn("ad break failed", "path", ad.Path, "error", err)
			}
			adIdx++
			continue
		}

		s := specs[i]
		segmentPtsOffset := sp.currentPtsOffset
		mediaStart := sp.runningTime
		mediaEnd := sp.runningTime + s.duration

		isFirst := (i == 0 && adIdx == 0)
		isLast := (i == len(specs)-1 && adIdx >= len(adList))

		if err := sp.processSegment(streamID, upstreamRends, s.line, segmentPtsOffset, s.duration, s.ptsStart, s.ptsEnd, mediaStart, mediaEnd, adCtx, isFirst, isLast); err != nil {
			return err
		}

		sp.runningTime = mediaEnd
		sp.currentPtsOffset = sp.currentPtsOffset + s.duration
		i++
	}
	return nil
}

func (sp *SegmentProcessor) processSegment(streamID string, renditions []string, line string, ptsOffset float64, duration float64, ptsStart, ptsEnd int64, mediaStart, mediaEnd float64, adCtx *adBreakContext, isFirst, isLast bool) error {
	sp.sequenceCounter++
	segmentPath := segmentStoragePath(streamID, line)
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

	applyAdMarker(&segment, adCtx, isFirst, isLast)

	if mediaStart >= (sp.nowUnix - float64(utils.GetPlannerSettings().WindowBufferSeconds)) {
		if err := sp.addSegment(sp.ctx, mediaStart, segment); err != nil {
			return err
		}
		sp.totalSegmentsAdded++
	}

	return nil
}

// stampPreRollCues attaches the placement-opportunity (pre-roll) splice
// inserts to the content segment(s) immediately preceding an ad break.
func (sp *SegmentProcessor) stampPreRollCues(adCtx *adBreakContext) {
	for _, delay := range PreRollDelaysSec {
		preRollPts := adCtx.start - delay
		if preRollPts < 0 {
			continue
		}
		idx, ok := findSegmentContainingPts(sp.processingSegments, preRollPts)
		if !ok {
			continue
		}
		ev := models.SpliceInsert{
			Type:               "out",
			EventID:            adCtx.id,
			SpliceTimePTS:      int64(preRollPts * configs.TICKS),
			SpliceTimeSeconds:  preRollPts,
			Duration:           adCtx.duration,
			UniqueProgramID:    adCtx.id & 0xFFFF,
			SegmentationTypeID: configs.SEG_TYPE_PLACE_OPPORTUNITY_START,
		}
		sp.processingSegments[idx].AdBreakID = adCtx.id
		appendSpliceInsertUnique(&sp.processingSegments[idx], ev)
	}
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
	sp.processingSegments = append(sp.processingSegments, data)

	if runningTime <= (float64(time.Now().Unix())+float64(utils.GetPlannerSettings().WindowBufferSeconds)) || (sp.totalSegmentsAdded > 0 && sp.totalSegmentsAdded%int64(utils.GetPlannerSettings().WindowItemSize) == 0) {
		sp.synchSegmentIntoRedis(ctx, false)
	}
	return nil
}

func (sp *SegmentProcessor) synchSegmentIntoRedis(ctx context.Context, synchAllItem bool) {
	if err := ctx.Err(); err != nil {
		return
	}

	sizeItems := len(sp.processingSegments)
	if sizeItems == 0 {
		return // Guard against empty slice panics
	}

	// Sync every accumulated segment to Redis so none are dropped between
	// intermediate flushes. Only the in-memory retention below is limited.
	itemsToProcess := sp.processingSegments

	for _, segment := range itemsToProcess {
		member, err := json.Marshal(segment)
		if err != nil {
			sp.logger.Error("[Planner] json.Marshal(SegmentItem) failed", "error", err)
			continue
		}

		if utils.IsDev() {
			t := time.Unix(int64(segment.RunningTime), 0)

			sp.logger.Info("[Sequence]", "seq", segment.Sequence, "time", t.Format(time.TimeOnly), "isAd", segment.IsAd, "ptsOffset", segment.PtsOffset, "timeText", segment.TimeText)
		}

		sp.pipe.ZAdd(ctx, sp.timelineKey(), redis.Z{
			Score:  float64(segment.RunningTime),
			Member: string(member),
		})
	}

	processCount := len(itemsToProcess)
	sp.logger.Info("synched data", "from", itemsToProcess[0].TimeText, "to", itemsToProcess[processCount-1].TimeText, "total", processCount)

	if _, err := sp.pipe.Exec(ctx); err != nil {
		sp.logger.Error("[Planner] synchSegmentIntoRedis Exec error", "error", err)
	}

	// Retain last 5 items in memory if synchAllItem is false, otherwise clear all
	if !synchAllItem && sizeItems > 5 {
		sp.processingSegments = sp.processingSegments[sizeItems-5:]
	} else if synchAllItem {
		sp.processingSegments = nil
	}
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
