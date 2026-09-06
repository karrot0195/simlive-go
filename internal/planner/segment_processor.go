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
	// SCTE-35 segmentation_type_id values per ANSI/SCTE 35 §10.2.3
	// (seg_type_id field inside the segmentation_descriptor).
	// Mirrors tmp/PrepareSimulatedLive.ts `SEG_TYPE_*` constants.
	// The planner stamps these on every SpliceInsert; the engine
	// reads them off the model and translates to a TYPE= attribute
	// on the emitted #EXT-X-SCTE35 tag (and into the segmentation
	// descriptor when the bitstream-side injector is added later).
	SegTypePlacementOpportunityStart = 0x30 // pre-roll (8s/4s warning)
	SegTypeAdvertisementStart        = 0x34 // slice point / cue-out
	SegTypeAdvertisementEnd          = 0x35 // cue-in
)

// PreRollDelaysSec lists the seconds-before-ad-break offsets the
// planner attaches a 0x30 pre-roll splice insert to. Order is
// emitted in order (longest delay first) so the playlist carries
// the events in chronological order. Mirrors tmp/PRE_ROLL_DELAYS.
//
// Tunable: change the slice values to add/remove pre-roll windows.
// Each entry produces one extra #EXT-X-SCTE35 tag per ad break in
// the engine's media playlist, and one extra SpliceInsert on the
// matching VOD SegmentItem in Redis.
//
// See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
// §3.1.1.
var PreRollDelaysSec = []float64{8, 4}

// PTS_DURATION is the 90kHz tick duration of one video frame at
// 24000/1001 fps (≈23.976 — the NTSC film rate used by most HLS
// VOD sources). The planner adds it to ptsEnd to derive the
// start of the NEXT frame (instead of the same frame's end
// boundary), so MediaEnd = (ptsEnd + PTS_DURATION) / 90000
// gives a non-overlapping [MediaStart, MediaEnd) interval that
// plays cleanly through the engine's PTS/DTS rewrite.
//
// Value: (1001/24000) * 90000 = 3753.75 ticks/frame.
//
// See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
// §3.1.2.
const PTS_DURATION = (1001.0 / 24000.0) * 90000.0

// SegmentProcessor holds the per-channel state for a single
// ProcessEPGToTimeline call. The fields here are channel-scoped
// (one per call) -- see the Planner.NOTE for the rationale. The
// two SCTE-35 ad-stamping fields (isBlockingProcessing,
// processingSegments) live on the SHARED Planner instead, so
// insertAdsIfNeeded and addSegment see the same buffer without a
// copy. The per-channel in-flight guard in
// cmd/planner/main.go runPlannerTick ensures the buffer is only
// touched by one call at a time.
type SegmentProcessor struct {
	planner         *Planner
	pipe            redis.Pipeliner
	ctx             context.Context
	sequenceCounter int64
	lastAdTime      float64
	totalAdsAdded   int
	nowUnix         float64
	adSegments      []models.SegmentItem

	// Per-call walk state — owned by THIS processor instance, not the shared
	// Planner. runningTime is the current timeline position, totalSegmentsAdded
	// counts members written, channelID is the channel being walked (it keys
	// the Redis ZSET), and horizon is the EndOfDay wall-clock boundary that
	// stops the walk. Living here instead of on the Planner makes
	// ProcessEPGToTimeline safe to call concurrently for DIFFERENT channels:
	// the old design mutated shared Planner fields (globalRunningTime,
	// totalSegmentsAdded, channelID, horizon) and required the epgMu mutex
	//
	// PLANNER_WORKERS=1 clamp — which serialized a multi-channel fleet to one
	// channel at a time (~30s–2min per date cold start).
	runningTime        float64
	totalSegmentsAdded int64
	channelID          string
	horizon            time.Time

	// PTS mapping state for the current stream
	currentPtsOffset float64
	// Local timeline anchor: MediaStart of the first segment in the timeline.
	// Every segment gets PtsOffset = MediaStart - timelineBase (local seconds).
	timelineBase float64

	// currentRendition is the active rung label (e.g. "360p") for this
	// processor instance, set from sp.planner.activeRenditions()[0] at the
	// top of processStreamSegments. Every member (VOD + ads) written by this
	// processor carries this tag so the engine can dispatch it to the right
	// per-rung directory. See docs/300826-multi-rendition-abr-ladder.md §3.
	// currentRendition string

	logger utils.Logger
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
	if epgStartUnix > sp.runningTime {
		sp.runningTime = epgStartUnix
	}

	// Initialize the first ad insertion marker if not set yet
	if sp.lastAdTime == 0 {
		sp.lastAdTime = sp.runningTime
	}

	return sp.processStreamSegments(streamID)
}

func (sp *SegmentProcessor) processStreamSegments(streamID string) error {
	masterPath := fmt.Sprintf("%s/%s/vod.m3u8", sp.planner.bucketName, streamID)
	upstreamRends := utils.GetRenditionsByMaster(masterPath, sp.planner.rclone)

	// M3.2: the upstream playlist advertises its L_* tier dirs (e.g. ["L_0","L_2"]).
	// We map each tier to a rendition LABEL (360p/720p/1080p) so the engine
	// can download each rung from its own tier bucket. The fallback map
	// (fallbackRenditionForTier) is the source of truth for L_* -> label.
	// We synthesize the full 3-rung label set so the master playlist always
	// lists 360p/720p/1080p even when the upstream only provides 2 tiers.
	// renditions := []string{"360p", "720p", "1080p"}

	// Read the highest-quality upstream m3u8 to drive the VOD timeline
	// (duration, pts_start, pts_end). The engine will pull the OTHER rungs
	// from their own L_* buckets on demand via StoragePath's {rendition}
	// placeholder.
	var primaryRung string
	if len(upstreamRends) > 0 {
		primaryRung = upstreamRends[0]
	} else {
		primaryRung = "L_0"
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

			// 1. Check and insert ads right before the video segment (when scheduled)
			if err := sp.insertAdsIfNeeded(sp.runningTime); err != nil {
				return err
			}

			// Anchor the PTS offset on the current runningTime at the segment start
			if firstSegmentInStream {
				targetPtsStart := sp.runningTime * 90000.0
				sp.currentPtsOffset = targetPtsStart - float64(ptsStart)
				firstSegmentInStream = false
			}

			adjustedPtsStart := float64(ptsStart) + sp.currentPtsOffset
			adjustedPtsEnd := float64(ptsEnd) + PTS_DURATION + sp.currentPtsOffset

			mediaStart := adjustedPtsStart / 90000.0
			mediaEnd := adjustedPtsEnd / 90000.0
			calculatedDuration := mediaEnd - mediaStart
			if calculatedDuration <= 0 {
				calculatedDuration = currentDuration
			}

			// If ads were just inserted, runningTime was pushed forward; resync mediaStart for the video
			if sp.runningTime > mediaStart {
				mediaStart = sp.runningTime
				mediaEnd = mediaStart + calculatedDuration
			}

			sp.logger.Info(fmt.Sprintf("start processing for %s", streamID))
			// 2. Process and write the regular video segment to Redis (single function owns the sequence)
			if err := sp.processSegment(streamID, upstreamRends, line, calculatedDuration, ptsStart, ptsEnd, mediaStart, mediaEnd); err != nil {
				return err
			}

			sp.logger.Info(fmt.Sprintf("finished processing for %s", streamID))

			// Advance runningTime by the actual video segment
			sp.runningTime = mediaEnd
		}
	}
	return nil
}

func (sp *SegmentProcessor) processSegment(streamID string, renditions []string, line string, duration float64, ptsStart, ptsEnd int64, mediaStart, mediaEnd float64) error {
	sp.sequenceCounter++ // Single +1 sequence bump here for video
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
		Renditions:  renditions, // M1: engine dispatches by tag.
	}

	if mediaStart >= (sp.nowUnix - WindowBufferSeconds) {
		if err := sp.addSegment(sp.ctx, mediaStart, segment); err != nil {
			return err
		}
		sp.totalSegmentsAdded++
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

// processingVerifyNearingAds toggles the shared
// sp.planner.isBlockingProcessing flag. When set, addSegment
// appends to the planner's processingSegments buffer but skips
// the Redis flush -- this prevents a near-ad burst (within
// AdInsertionInterval-12s of the last ad break) from fragmenting
// the in-memory buffer with tiny flushes. The flag resets to
// false once 8s have elapsed since the last ad, so the next
// non-blocking tick resumes flushing.
//
// Per-call guard: the per-channel in-flight guard in
// cmd/planner/main.go runPlannerTick ensures this method only
// runs while the SegmentProcessor is the sole writer of
// sp.planner.isBlockingProcessing for its channel.
func (sp *SegmentProcessor) processingVerifyNearingAds(actualStartTime float64) {
	if (actualStartTime - sp.lastAdTime) > float64(AdInsertionInterval-12) {
		sp.planner.isBlockingProcessing = true
	} else if sp.planner.isBlockingProcessing && (actualStartTime-sp.lastAdTime) > 8 {
		sp.planner.isBlockingProcessing = false
	}
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
	//
	// SCTE-35 break stamping (see docs/300826-implement-scte35-cue-markers.md):
	// every ad segment in this break shares a stable AdBreakID derived from
	// the break's start second, plus the AdBreakStart and AdBreakDuration
	// the engine needs to render #EXT-X-DATERANGE / splice_insert
	// payloads. breakDuration is the sum of the pool's individual ad
	// segment durations (the same pool trimmed to 5 by loadAdSegments).
	breakStart := sp.runningTime
	breakDuration := totalBreakDuration(sp.adSegments)
	breakID := adBreakIDFor(breakStart)

	// === Pre-roll (SegType 0x30) events on the VOD segments ===
	//
	// Walks PreRollDelaysSec (default 8s, 4s) and attaches a
	// Placement-Opportunity-Start SpliceInsert to the VOD segment
	// whose [MediaStart, MediaEnd) interval contains
	// (breakStart - delay). The event tells the player "an ad is
	// coming in N seconds" so SSAI clients can pre-fetch creatives
	// and tear down analytics sessions BEFORE the slice point.
	//
	// This must run BEFORE the ad loop so sp.planner.processingSegments
	// still holds the pre-ad-insert VOD buffer (the ad loop itself
	// does not touch VOD entries — it only appends).
	//
	// See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
	// §3.1.3.
	for _, delay := range PreRollDelaysSec {
		preRollPts := breakStart - delay
		if preRollPts < 0 {
			// First ad of the channel — no preceding VOD on the
			// local timeline. Defensive skip: the existing
			// EXT-X-DATERANGE CUE-OUT on the first ad already
			// covers this edge.
			log.Printf("[Planner] pre-roll skipped: preRollPts=%v (breakStart=%v, delay=%v) is before timelineBase (channel=%s)",
				preRollPts, breakStart, delay, sp.channelID)
			continue
		}
		idx, ok := findSegmentContainingPts(sp.planner.processingSegments, preRollPts)
		if !ok {
			// Pre-roll target not yet scheduled (the walk is racing
			// the segments). Same defensive behavior as the TS
			// reference: skip silently. Replans and the next tick
			// will eventually catch up.
			log.Printf("[Planner] pre-roll target not scheduled yet: preRollPts=%v (breakStart=%v, delay=%v, channel=%s)",
				preRollPts, breakStart, delay, sp.channelID)
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

	// Ad segment loop. The slice-point (0x34) event is stamped on
	// the FIRST ad's local copy BEFORE addSegment so the event
	// lands in the ZSET the first time that ad is flushed. The
	// cue-in (0x35) is also stamped on the LAST ad's local copy
	// INSIDE the loop -- breakEnd = breakStart + breakDuration is
	// a constant known at the top of the function (it's just the
	// sum of ad durations, invariant to loop iteration), so we
	// don't need to wait for the post-loop.
	//
	// The dedup-by-(Type, PTS) rule in appendSpliceInsertUnique
	// handles the single-ad-break case (where firstAd == lastAd
	// and both events land on the same ad) safely.
	breakEnd := breakStart + breakDuration

	for i, adSeg := range sp.adSegments {
		if isEndOfTimeline(sp.horizon, sp.runningTime) {
			return ErrEndOfTimeline
		}

		sp.sequenceCounter++ // Standard +1 sequence bump
		adItem := adSeg
		adItem.Sequence = sp.sequenceCounter

		// Map the ad right after the current runningTime
		adItem.MediaStart = sp.runningTime
		adItem.MediaEnd = sp.runningTime + adItem.Duration
		adItem.StartTime = int64(adItem.MediaStart)
		ptsOffset := sp.setTimelineBase(adItem.MediaStart)
		adItem.PtsOffset = &ptsOffset

		// Stamp the SCTE-35 break metadata (engine reads these to render
		// the CUE-OUT / CUE-IN DATERANGE pair on the VOD<>ad edges).
		adItem.IsAd = true
		adItem.AdBreakID = breakID
		adItem.AdBreakStart = breakStart
		adItem.AdBreakDuration = breakDuration

		// Slice-point on the FIRST ad: must happen BEFORE addSegment
		// so the event lands in the ZSET the first time the ad is
		// flushed. breakStart*90000 ticks. Capture a pointer to the
		// local adItem for the post-loop cue-in on the LAST ad
		// (single-ad breaks will overwrite firstAdItem).
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
		// Cue-in on the LAST ad: also stamped before addSegment for
		// the same reason. breakEnd is a constant known at the top
		// of this function (breakStart + breakDuration, invariant
		// to loop iteration) so we don't need to defer to a
		// post-loop stamping block.
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

	// Update the last ad insertion marker so the next insertion triggers after 30 more seconds
	sp.lastAdTime = sp.runningTime
	sp.planner.adsId = sp.planner.adsId + 1
	return nil
}

// timelineKey returns the Redis ZSET key for this processor's channel + rung.
// M2: each rung has its own ZSET (simlive:timeline:{ch}:{rend}), so the
// processor's currentRendition is part of the key. Per-call (not a shared
// Planner field) so concurrent channels write to their own keys.
func (sp *SegmentProcessor) timelineKey() string {
	return timelineKeyFor(sp.channelID)
}

// flushBatch flushes the processor's pipeline. Mirrors the old
// Planner.flushBatch(pipe, ctx).
func (sp *SegmentProcessor) flushBatch() error {
	_, err := sp.pipe.Exec(context.Background())
	return err
}

// flush is the final step of ProcessEPGToTimeline: it sets the timeline
// key's TTL and flushes the pipeline. Mirrors the old
// Planner.flushRedisPipeline.
func (sp *SegmentProcessor) flush() error {
	sp.pipe.Expire(context.Background(), sp.timelineKey(), RedisExpiryHours*time.Hour)
	_, err := sp.pipe.Exec(context.Background())
	return err
}

// addSegment writes one timeline member to the channel's Redis ZSET.
// Mirrors the old Planner.addSegmentToRedis but keys off the processor's
// per-call channelID/horizon instead of shared Planner state. Batches the
// pipeline every WindowItemSize segments and flushes immediately for
// members inside the live window.
//
// The shared sp.planner.processingSegments buffer holds the in-memory
// copy of every unflushed member. Both addSegment and synchSegmentIntoRedis
// reference the same slice; the per-channel in-flight guard in
// cmd/planner/main.go ensures no two addSegment calls append concurrently.
func (sp *SegmentProcessor) addSegment(ctx context.Context, runningTime float64, data models.SegmentItem) error {
	if isEndOfTimeline(sp.horizon, runningTime) {
		return ErrEndOfTimeline
	}

	data.TimeText = utils.TimeNumToText(data.StartTime)
	data.RunningTime = runningTime
	sp.planner.processingSegments = append(sp.planner.processingSegments, data)

	if sp.planner.isBlockingProcessing {
		return nil
	}

	if runningTime <= (float64(time.Now().Unix()) + 120) {
		sp.synchSegmentIntoRedis(ctx)
	} else if sp.totalSegmentsAdded > 0 && sp.totalSegmentsAdded%WindowItemSize == 0 {
		sp.synchSegmentIntoRedis(ctx)
	}
	return nil
}

// synchSegmentIntoRedis drains sp.planner.processingSegments into the
// channel's timeline ZSET, JSON-marshaling each SegmentItem
// (see the wire-format note below) and Exec'ing the pipeline.
// The buffer is reset on the way out so the next addSegment
// starts clean.
//
// Wire format invariant: the Member of each ZAdd must be a
// STRING (the JSON-marshaled SegmentItem), not a raw struct.
// go-redis v9's proto.Writer only supports primitive types,
// time.Time, and encoding.BinaryMarshaler for the Member field
// (it returns "can't marshal %T (implement
// encoding.BinaryMarshaler)" otherwise). MarshalBinary would
// couple models.SegmentItem to encoding/gob, locking us out of
// arbitrary JSON shape changes; JSON keeps the on-the-wire
// format readable and decoupled from the type. The engine reads
// the ZSET members back via ZRangeByScore and unmarshals them
// with json.Unmarshal, so the on-the-wire shape MUST match the
// json tags on SegmentItem.
func (sp *SegmentProcessor) synchSegmentIntoRedis(ctx context.Context) {
	for _, segment := range sp.planner.processingSegments {
		// JSON-marshal the SegmentItem before ZAdd: go-redis v9's
		// proto.Writer only supports primitive types, time.Time, and
		// encoding.BinaryMarshaler for the Member field (it returns
		// "can't marshal %T (implement encoding.BinaryMarshaler)"
		// otherwise). MarshalBinary would couple models.SegmentItem
		// to encoding/gob, which would lock us out of arbitrary
		// JSON shape changes; JSON keeps the on-the-wire format
		// readable and decoupled from the type.
		//
		// The ZSET member is what the engine reads back via
		// ZRangeByScore, so it MUST be valid JSON of the same
		// shape that comes out of json.Marshal(SegmentItem). The
		// encoding/json tags on SegmentItem are the source of
		// truth -- do not add a MarshalBinary method.
		member, err := json.Marshal(segment)
		if err != nil {
			// Defensive: json.Marshal of a SegmentItem can't
			// actually fail (no func/chan fields), but if a
			// future field breaks that contract, fail loud here
			// instead of silently writing an empty ZSET member.
			log.Printf("[Planner] json.Marshal(SegmentItem) failed: %v -- skipping ZAdd for sequence=%d", err, segment.Sequence)
			continue
		}
		sp.pipe.ZAdd(sp.ctx, sp.timelineKey(), redis.Z{
			Score:  float64(segment.RunningTime),
			Member: string(member),
		})
	}
	sp.pipe.BatchProcess(ctx)
	if _, err := sp.pipe.Exec(context.Background()); err != nil {
		log.Printf("[Planner] synchSegmentIntoRedis Exec: %v", err)
	}
	sp.planner.processingSegments = []models.SegmentItem{}
}

// adBreakIDFor retur ns the SCTE-35 stable break identifier for a
// given break-start second. The format matches the engine's
// `buildCueOutLine` / `buildCueInLine` so the planner and the
// engine agree on the ID byte-for-byte. The function is pure so
// the test can lock the contract without a Planner / Redis.
//
// Time-derived (not a counter) so planner replans and restarts
// produce the same ID. With AdInsertionInterval=30s + the 5-ad
// pool cap, two breaks cannot start at the same unix second on
// the same channel; if cadence ever tightens, switch to
// (breakStart<<8) | tickSequence for unique IDs.
//
// See docs/300826-implement-scte35-cue-markers.md §6 "Known Gaps".
func adBreakIDFor(breakStart float64) string {
	return fmt.Sprintf("adbreak-%d", int64(breakStart))
}

// totalBreakDuration returns the SCTE-35 break length in seconds,
// summed across every ad segment in the pool. Mirrors the
// "Σ ad[i].Duration" inline computation that previously lived in
// insertAdsIfNeeded; lifted to a pure helper so the unit test can
// assert it without a Planner. An empty pool returns 0.
func totalBreakDuration(ads []models.SegmentItem) float64 {
	total := 0.0
	for _, a := range ads {
		total += a.Duration
	}
	return total
}

// adBreakEventIDFor returns the SCTE-35 SpliceEventID (uint32) for
// a given break-start second. The 0x7FFFFFFF mask avoids the
// 0xFFFFFFFF reserved value (SCTE 35 §9.3.4, "SpliceInsertType =
// 0x05 with cancellation semantics") and the high-bit ambiguity
// for negative breakStart inputs.
//
// MUST match the engine's adBreakEventID byte-for-byte so the
// planner's per-segment SliceInsert event IDs and the engine's
// DATERANGE CUE-OUT / CUE-IN IDs agree on the same break -- if
// they drift, player-side analytics dedup silently breaks.
//
// Pure so the test can lock the contract without a Planner /
// Redis. See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
// §3.1.2.
func adBreakEventIDFor(breakStart float64) uint32 {
	return uint32(int64(breakStart) & 0x7FFFFFFF)
}

// findSegmentContainingPts returns the index of the segment in
// `segs` whose [MediaStart, MediaEnd) interval contains `ptsSec`
// (seconds on the channel's local timeline). Returns (-1, false)
// when no segment covers the point.
//
// Mirrors tmp/PrepareSimulatedLive.ts `findSegmentContainingPts`.
// Inclusive on the left edge (pts == MediaStart counts as
// "inside"), exclusive on the right edge (pts == MediaEnd counts
// as "after this segment"). On ties (FP rounding noise on the
// right edge of two adjacent intervals) the FIRST matching
// segment wins -- the TS reference's behavior is also "first
// match" via Array.find, so we stay byte-for-byte compatible.
//
// Pure, no I/O. Safe to call against sp.planner.processingSegments
// (slice of values, not pointers) inside the for-loop body.
//
// See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
// §3.1.2.
func findSegmentContainingPts(segs []models.SegmentItem, ptsSec float64) (int, bool) {
	if len(segs) == 0 {
		return -1, false
	}
	for i, s := range segs {
		if ptsSec >= s.MediaStart && (ptsSec < s.MediaEnd || i+1 < len(segs) && ptsSec < segs[i+1].MediaStart) {
			return i, true
		}
	}
	return -1, false
}

// appendSpliceInsertUnique appends `ev` to `target.SliceInserts`
// UNLESS an entry with the same (Type, SpliceTimePTS) is already
// there. Mirrors tmp/PrepareSimulatedLive.ts `pushSpliceEvent`:
//
//	if (!exists) { target.spliceEvents.push(event); }
//
// where `exists = target.spliceEvents.some(e => e.type === t &&
// e.spliceTimePts === p)`. The dedup key (Type, SpliceTimePTS)
// was chosen by the TS reference because the planner can run
// multiple ticks for the same break (replan, restart) and we
// want a single VOD segment to carry at most one pre-roll event
// per (delay, channel, break).
//
// Mutates `target` in place via pointer; the caller must pass a
// pointer into a slice's backing array (NOT a copy). See
// insertAdsIfNeeded for the only in-tree caller.
//
// Pure (no I/O). Safe to call from tests against a local
// SegmentItem.
//
// See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
// §3.1.2.
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
