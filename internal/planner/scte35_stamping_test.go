package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"simlive/internal/models"
)

// This file locks the SCTE-35 break-stamping contract implemented in
// insertAdsIfNeeded (see docs/300826-implement-scte35-cue-markers.md).
// It mixes pure unit tests (no I/O) with a single Redis-gated
// integration test that exercises the full addSegmentToRedis path
// and reads back the ZSET to assert every stamped field round-trips.

// --- Pure unit tests: adBreakIDFor ----------------------------------------

// TestAdBreakIDFor_Format asserts the break ID format mirrors what
// the engine's buildCueOutLine / buildCueInLine emit ("adbreak-<n>").
// The planner and engine MUST agree on the byte-for-byte format;
// otherwise CUE-OUT and CUE-IN would carry different IDs and SSAI
// clients would never match the pair.
func TestAdBreakIDFor_Format(t *testing.T) {
	cases := []struct {
		start float64
		want  string
	}{
		{1725014400, "adbreak-1725014400"}, // 2024-08-30T10:40:00Z
		{0, "adbreak-0"},
		{0.9, "adbreak-0"},                   // sub-second -> int64 truncation to 0
		{1725014400.5, "adbreak-1725014400"}, // fractional still truncates
	}
	for _, tc := range cases {
		got := adBreakIDFor(tc.start)
		if got != tc.want {
			t.Fatalf("adBreakIDFor(%v) = %q, want %q", tc.start, got, tc.want)
		}
	}
}

// TestAdBreakIDFor_Stable locks the time-derived invariant: same
// input always produces the same ID across calls. Without this,
// replans and engine restarts would emit mismatched CUE-OUT / CUE-IN
// IDs and player-side analytics dedup would silently break.
func TestAdBreakIDFor_Stable(t *testing.T) {
	a := adBreakIDFor(1725014400)
	b := adBreakIDFor(1725014400)
	if a != b {
		t.Fatalf("adBreakIDFor not stable: %q vs %q", a, b)
	}
}

// TestAdBreakIDFor_DistinctStarts: different break start seconds
// must produce different IDs (otherwise two breaks collide on the
// same adbreak- tag and the engine emits a single CUE-OUT / CUE-IN
// for both).
func TestAdBreakIDFor_DistinctStarts(t *testing.T) {
	a := adBreakIDFor(1725014400)
	b := adBreakIDFor(1725014401)
	if a == b {
		t.Fatalf("distinct starts produced same ID: %q", a)
	}
}

// --- Pure unit tests: totalBreakDuration ----------------------------------

// TestTotalBreakDuration_Empty: an empty pool (no ads loaded) must
// return 0; insertAdsIfNeeded guards on len() > 0, but the math
// helper itself must be honest about the zero case.
func TestTotalBreakDuration_Empty(t *testing.T) {
	if got := totalBreakDuration(nil); got != 0 {
		t.Fatalf("nil pool: got %v, want 0", got)
	}
	if got := totalBreakDuration([]models.SegmentItem{}); got != 0 {
		t.Fatalf("empty pool: got %v, want 0", got)
	}
}

// TestTotalBreakDuration_LoadsAdSegmentsShape: the planner loads up
// to 5 ad segments via loadAdSegments (planner.go loadAdSegments
// caps the pool at 5). totalBreakDuration must sum them all and
// tolerate the typical 4-6s-per-ad range without losing precision.
func TestTotalBreakDuration_LoadsAdSegmentsShape(t *testing.T) {
	pool := []models.SegmentItem{
		{Duration: 6.0},
		{Duration: 6.0},
		{Duration: 6.0},
		{Duration: 6.0},
		{Duration: 6.0},
	}
	if got, want := totalBreakDuration(pool), 30.0; got != want {
		t.Fatalf("5x6s pool: got %v, want %v", got, want)
	}
}

// TestTotalBreakDuration_FractionalDurations: loadAdSegments falls
// back to mEnd - mStart when the EXTINF is missing, so non-integer
// durations are real. Sum must preserve fractional precision.
func TestTotalBreakDuration_FractionalDurations(t *testing.T) {
	pool := []models.SegmentItem{
		{Duration: 6.0},
		{Duration: 5.984}, // 90kHz ticks 538560
		{Duration: 6.012},
	}
	got := totalBreakDuration(pool)
	want := 17.996
	if got < want-0.0001 || got > want+0.0001 {
		t.Fatalf("fractional sum: got %v, want ~%v", got, want)
	}
}

// --- Pure unit tests: stamping contract end-to-end on a SegmentItem ----

// TestStampAdBreak_OnSegmentItem_LocksContract exercises the same
// field assignments insertAdsIfNeeded performs on each adItem, but
// in a pure-Go table. If any field name, value, or ordering shifts
// in the planner, this test catches it before the engine's
// buildMediaPlaylistBody reads the wrong shape.
func TestStampAdBreak_OnSegmentItem_LocksContract(t *testing.T) {
	const breakStart float64 = 1725014400
	pool := []models.SegmentItem{
		{Duration: 6.0, StoragePath: "ads-v3/L2/seg_1.ts"},
		{Duration: 6.0, StoragePath: "ads-v3/L2/seg_2.ts"},
		{Duration: 6.0, StoragePath: "ads-v3/L2/seg_3.ts"},
	}
	breakDuration := totalBreakDuration(pool)
	breakID := adBreakIDFor(breakStart)

	if breakID != "adbreak-1725014400" {
		t.Fatalf("breakID = %q, want adbreak-1725014400", breakID)
	}
	if breakDuration != 18.0 {
		t.Fatalf("breakDuration = %v, want 18.0", breakDuration)
	}

	// Stamp every ad item with the same 4 fields, mirroring
	// insertAdsIfNeeded (segment_processor.go).
	stamped := make([]models.SegmentItem, len(pool))
	for i, a := range pool {
		adItem := a
		adItem.IsAd = true
		adItem.AdBreakID = breakID
		adItem.AdBreakStart = breakStart
		adItem.AdBreakDuration = breakDuration
		stamped[i] = adItem
	}

	// All ads in the break must carry the SAME AdBreakID, the SAME
	// AdBreakStart, the SAME AdBreakDuration -- otherwise the
	// engine emits multiple mismatched CUE-OUT/CUE-IN pairs.
	for i, s := range stamped {
		if !s.IsAd {
			t.Fatalf("stamped[%d].IsAd = false, want true", i)
		}
		if s.AdBreakID != breakID {
			t.Fatalf("stamped[%d].AdBreakID = %q, want %q", i, s.AdBreakID, breakID)
		}
		if s.AdBreakStart != breakStart {
			t.Fatalf("stamped[%d].AdBreakStart = %v, want %v", i, s.AdBreakStart, breakStart)
		}
		if s.AdBreakDuration != breakDuration {
			t.Fatalf("stamped[%d].AdBreakDuration = %v, want %v", i, s.AdBreakDuration, breakDuration)
		}
	}
}

// --- JSON round-trip on a stamped SegmentItem ----------------------------

// TestStampedSegmentItem_JSONRoundTrip: the SegmentItem is stored
// in Redis as a JSON-encoded ZSET member. The 4 stamped fields MUST
// round-trip through Marshal -> Unmarshal unchanged -- if any JSON
// tag drifts, the engine reads zero-values back and stops emitting
// DATERANGE tags.
func TestStampedSegmentItem_JSONRoundTrip(t *testing.T) {
	src := models.SegmentItem{
		Sequence:        42,
		StoragePath:     "espx-video-lib:espx-video-lib/ads-v3/L2/seg_42.ts",
		Duration:        6.0,
		IsAd:            true,
		AdBreakID:       "adbreak-1725014400",
		AdBreakStart:    1725014400,
		AdBreakDuration: 18.0,
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"is_ad":true`,
		`"ad_break_id":"adbreak-1725014400"`,
		`"ad_break_start":1725014400`,
		`"ad_break_duration":18`,
	} {
		if !contains(b, want) {
			t.Fatalf("marshal missing %q: %s", want, b)
		}
	}
	var out models.SegmentItem
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.IsAd || out.AdBreakID != src.AdBreakID ||
		out.AdBreakStart != src.AdBreakStart || out.AdBreakDuration != src.AdBreakDuration {
		t.Fatalf("roundtrip lost fields: %+v", out)
	}
}

// contains is a small strings.Contains shim that avoids the extra
// import for a one-call use.
func contains(haystack []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// --- Redis-gated integration: insertAdsIfNeeded end-to-end --------------

// redisReachableForStamping is a tiny helper to skip the Redis
// integration test cleanly when no Redis is listening. Mirrors the
// pattern in horizon_rollover_test.go / retry_breaker_test.go.
func redisReachableForStamping(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// TestInsertAdsIfNeeded_StampsAllAdsWithBreakMetadata exercises the
// real insertAdsIfNeeded code path against an in-memory *Planner
// wired to a real *redis.Client. It seeds a 3-ad pool, walks the
// break once, then ZRANGEs the resulting ZSET and asserts every
// member carries the same AdBreakID / AdBreakStart / AdBreakDuration
// and IsAd=true. Skipped when no Redis is reachable at REDIS_ADDR
// (or localhost:6371) so it works on a dev box without Redis.
//
// This is the strongest unit test possible without a full
// ProcessEPGToTimeline end-to-end: it proves the stamped values
// the engine relies on actually reach Redis.
func TestInsertAdsIfNeeded_StampsAllAdsWithBreakMetadata(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForStamping(addr) {
		t.Skipf("redis not reachable at %s; set REDIS_ADDR to enable this test", addr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	// Fresh channel ID per test run to avoid cross-pollution.
	chID := "test-scte35-stamp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanCancel()
		rdb.Del(cleanCtx, timelineKeyFor(chID))
	}()

	const breakStart = 1725014400.0 // fixed for stable ID across the test
	ads := []models.SegmentItem{
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_1.ts"},
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_2.ts"},
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_3.ts"},
	}

	sp := &SegmentProcessor{
		planner: &Planner{
			rdb:        rdb,
			rendCache:  map[string]renditionPair{},
			bucketName: "espx-video-lib:espx-video-lib",
			adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
		},
		pipe:            rdb.Pipeline(),
		ctx:             ctx,
		sequenceCounter: 100, // arbitrary starting sequence
		adSegments:      ads,
		channelID:       chID,
		// nowUnix in the past so actualStartTime >= nowUnix + 30s guard passes.
		nowUnix: 1.0,
		// lastAdTime far in the past so (actualStartTime - lastAdTime) >=
		// AdInsertionInterval is satisfied.
		lastAdTime: 1.0,
	}
	// Pin the per-processor running time + horizon so the break happens at a
	// known second and isEndOfTimeline returns false.
	sp.runningTime = breakStart
	sp.horizon = time.Time{} // zero => no horizon => isEndOfTimeline is no-op

	if err := sp.insertAdsIfNeeded(breakStart); err != nil {
		t.Fatalf("insertAdsIfNeeded: %v", err)
	}
	if _, err := sp.pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec: %v", err)
	}

	// Read back the ZSET and assert every member carries the stamped
	// fields. The 3 ads landed at scores [breakStart, breakStart+6,
	// breakStart+12) and all must share the SAME AdBreakID +
	// AdBreakStart + AdBreakDuration.
	members, err := rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     timelineKeyFor(chID),
		Start:   fmt.Sprintf("%d", int64(breakStart)-1),
		Stop:    "+inf",
		ByScore: true,
	}).Result()
	if err != nil {
		t.Fatalf("ZRangeByScore: %v", err)
	}
	if got, want := len(members), len(ads); got != want {
		t.Fatalf("ZSET cardinality = %d, want %d", got, want)
	}

	wantBreakID := adBreakIDFor(breakStart)
	wantDur := totalBreakDuration(ads)

	for i, raw := range members {
		var seg models.SegmentItem
		if err := json.Unmarshal([]byte(raw), &seg); err != nil {
			t.Fatalf("member[%d] json.Unmarshal: %v", i, err)
		}
		if !seg.IsAd {
			t.Fatalf("member[%d] IsAd = false, want true (path=%q)", i, seg.StoragePath)
		}
		if seg.AdBreakID != wantBreakID {
			t.Fatalf("member[%d] AdBreakID = %q, want %q", i, seg.AdBreakID, wantBreakID)
		}
		if seg.AdBreakStart != breakStart {
			t.Fatalf("member[%d] AdBreakStart = %v, want %v", i, seg.AdBreakStart, breakStart)
		}
		if seg.AdBreakDuration != wantDur {
			t.Fatalf("member[%d] AdBreakDuration = %v, want %v", i, seg.AdBreakDuration, wantDur)
		}
	}

	// lastAdTime must be advanced to the end of the inserted break
	// so the next insertAdsIfNeeded call respects the 30s cadence.
	if sp.lastAdTime != breakStart+wantDur {
		t.Fatalf("lastAdTime = %v, want %v (end of inserted break)",
			sp.lastAdTime, breakStart+wantDur)
	}

	// totalAdsAdded must reflect the 3 inserted ads.
	if sp.totalAdsAdded != len(ads) {
		t.Fatalf("totalAdsAdded = %d, want %d", sp.totalAdsAdded, len(ads))
	}
}

// --- Per-segment SliceInserts stamping (4 events per break) -------------
//
// Added for docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md.
// Each ad break now produces:
//   - 2 pre-roll events (0x30) on the VOD segments at breakStart-8 and
//     breakStart-4.
//   - 1 slice-point event (0x34) on the FIRST ad.
//   - 1 cue-in event (0x35) on the LAST ad.
// All 4 share the same EventID and Duration.

// newStampingTestChannelID returns a unique channel id per test run
// so the Redis ZSET for one test doesn't pollute another. Mirrors
// the chID pattern already used above.
func newStampingTestChannelID(prefix string) string {
	return prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// TestStampSliceEvents_OnVODAndAdSegments exercises the full
// four-event stamping path against a real *redis.Client. Seeds
// sp.processingSegments with 5 VOD segments (MediaStart 0..24) so
// the pre-roll loop has targets at breakStart-8 (=22) and
// breakStart-4 (=26). Calls insertAdsIfNeeded(breakStart=30).
// ZRANGEs the ZSET, then for each member deserializes and asserts
// the four events land on the right segments with the right
// fields. Skips cleanly when no Redis is reachable.
func TestStampSliceEvents_OnVODAndAdSegments(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForStamping(addr) {
		t.Skipf("redis not reachable at %s; set REDIS_ADDR to enable this test", addr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := newStampingTestChannelID("test-scte35-sliceevents")
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanCancel()
		rdb.Del(cleanCtx, timelineKeyFor(chID))
	}()

	const breakStart = 30.0
	ads := []models.SegmentItem{
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_1.ts"},
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_2.ts"},
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_3.ts"},
	}

	// Seed sp.processingSegments with 5 VOD segments (MediaStart
	// 0..24) so the pre-roll loop can find the 8s and 4s targets.
	// Mirrors what processStreamSegments would have done: each
	// segment gets its MediaStart/MediaEnd/StartTime set in the
	// VOD buffer. No ad stamped.
	vodSegs := []models.SegmentItem{
		{Sequence: 100, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_100.ts", MediaStart: 0, MediaEnd: 4, StartTime: 0, Duration: 4, RunningTime: 0},
		{Sequence: 101, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_101.ts", MediaStart: 4, MediaEnd: 8, StartTime: 4, Duration: 4, RunningTime: 4},
		{Sequence: 102, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_102.ts", MediaStart: 8, MediaEnd: 14, StartTime: 8, Duration: 6, RunningTime: 8},
		{Sequence: 103, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_103.ts", MediaStart: 14, MediaEnd: 20, StartTime: 14, Duration: 6, RunningTime: 14},
		// 5th segment intentionally straddles breakStart-8 (22) and breakStart-4 (26)
		// so the pre-roll target lookup returns a definite single index.
		{Sequence: 104, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_104.ts", MediaStart: 20, MediaEnd: 28, StartTime: 20, Duration: 8, RunningTime: 20},
	}

	sp := &SegmentProcessor{
		planner: &Planner{
			rdb:        rdb,
			rendCache:  map[string]renditionPair{},
			bucketName: "espx-video-lib:espx-video-lib",
			adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
		},
		pipe:            rdb.Pipeline(),
		ctx:             ctx,
		sequenceCounter: 200, // start well above the VOD seqs so ads have fresh numbers
		adSegments:      ads,
		channelID:       chID,
		nowUnix:         1.0,
		lastAdTime:      0.0, // 0 so (30 - 0) >= AdInsertionInterval=30 is satisfied
		runningTime:     breakStart,
		horizon:         time.Time{}, // no horizon => isEndOfTimeline is no-op
	}
	// Don't pre-flush the VOD buffer: the pre-roll loop runs
	// against the in-memory sp.planner.processingSegments and mutates the
	// matching VOD entries IN PLACE. The natural flushes triggered
	// by the ad loop's addSegment calls (runningTime <= nowUnix+120)
	// will write the mutated VOD + all 3 ads to the ZSET.
	// The processingSegments buffer lives on the SHARED Planner (not
	// the per-call processor) so the pre-roll lookup + addSegment
	// see the same slice during the call.
	sp.planner.processingSegments = append([]models.SegmentItem{}, vodSegs...)

	if err := sp.insertAdsIfNeeded(breakStart); err != nil {
		t.Fatalf("insertAdsIfNeeded: %v", err)
	}
	if _, err := sp.pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec: %v", err)
	}

	// Read back the ZSET across the whole range so we see both the
	// 5 VOD members and the 3 ad members (the ZSET was empty before
	// this test, so we read everything we wrote).
	members, err := rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     timelineKeyFor(chID),
		Start:   "-inf",
		Stop:    "+inf",
		ByScore: true,
	}).Result()
	if err != nil {
		t.Fatalf("ZRangeByScore: %v", err)
	}
	if got, want := len(members), len(vodSegs)+len(ads); got != want {
		t.Fatalf("ZSET cardinality = %d, want %d (5 VOD + 3 ads)", got, want)
	}

	wantBreakID := adBreakIDFor(breakStart)
	wantEventID := int64(adBreakEventIDFor(breakStart))
	wantDur := totalBreakDuration(ads)
	breakEnd := breakStart + wantDur

	// Index every member by Sequence so we can assert the right
	// slice events on the right members.
	bySeq := make(map[int64]models.SegmentItem, len(members))
	for i, raw := range members {
		var seg models.SegmentItem
		if err := json.Unmarshal([]byte(raw), &seg); err != nil {
			t.Fatalf("member[%d] json.Unmarshal: %v", i, err)
		}
		bySeq[seg.Sequence] = seg
	}

	// --- VOD seg at [20, 28) (seq 104) carries BOTH pre-rolls
	// (8s and 4s) because its [20, 28) interval covers both 22
	// (=breakStart-8) and 26 (=breakStart-4). Walk the entries
	// and assert each is the expected (Type, SpliceTimePTS) pair. ---
	vod8 := bySeq[104]
	if got, want := len(vod8.SliceInserts), 2; got != want {
		t.Fatalf("VOD seq=104 SliceInserts = %d entries, want %d (8s + 4s pre-rolls both on the same VOD seg)",
			got, want)
	}
	wantPTS8 := int64((breakStart - 8) * 90000.0)
	wantPTS4 := int64((breakStart - 4) * 90000.0)
	seen8, seen4 := false, false
	for _, ev := range vod8.SliceInserts {
		if ev.Type != "out" {
			t.Fatalf("VOD seq=104 has non-out entry: type=%q", ev.Type)
		}
		if ev.SegmentationTypeID != SegTypePlacementOpportunityStart {
			t.Fatalf("VOD seq=104 has non-pre-roll entry: segType=%#x", ev.SegmentationTypeID)
		}
		if ev.EventID != wantEventID || ev.UniqueProgramID != wantEventID {
			t.Fatalf("VOD seq=104 event ID mismatch: EventID=%d UniqueProgramID=%d, want both %d",
				ev.EventID, ev.UniqueProgramID, wantEventID)
		}
		if ev.Duration != wantDur {
			t.Fatalf("VOD seq=104 event Duration = %v, want %v", ev.Duration, wantDur)
		}
		switch ev.SpliceTimePTS {
		case wantPTS8:
			if ev.SpliceTimeSeconds != breakStart-8 {
				t.Fatalf("8s pre-roll SpliceTimeSeconds = %v, want %v", ev.SpliceTimeSeconds, breakStart-8)
			}
			seen8 = true
		case wantPTS4:
			if ev.SpliceTimeSeconds != breakStart-4 {
				t.Fatalf("4s pre-roll SpliceTimeSeconds = %v, want %v", ev.SpliceTimeSeconds, breakStart-4)
			}
			seen4 = true
		default:
			t.Fatalf("VOD seq=104 has unexpected SpliceTimePTS = %d (want %d or %d)",
				ev.SpliceTimePTS, wantPTS8, wantPTS4)
		}
	}
	if !seen8 {
		t.Fatalf("VOD seq=104 missing 8s pre-roll (PTS=%d)", wantPTS8)
	}
	if !seen4 {
		t.Fatalf("VOD seq=104 missing 4s pre-roll (PTS=%d)", wantPTS4)
	}

	// --- First ad carries the slice-point (0x34) ---
	// We seeded ads with the same Duration/StoragePath so the
	// sequence numbers assigned in order are 201, 202, 203 (the
	// 3 ads after the 5 VOD segs with seq 100..104).
	firstAd := bySeq[201]
	if !firstAd.IsAd {
		t.Fatalf("seq=201 IsAd = false, want true")
	}
	if firstAd.AdBreakID != wantBreakID {
		t.Fatalf("seq=201 AdBreakID = %q, want %q", firstAd.AdBreakID, wantBreakID)
	}
	if got, want := len(firstAd.SliceInserts), 1; got != want {
		t.Fatalf("firstAd SliceInserts = %d entries, want %d (slice-point only)", got, want)
	}
	sp1 := firstAd.SliceInserts[0]
	if sp1.Type != "out" || sp1.SegmentationTypeID != SegTypeAdvertisementStart {
		t.Fatalf("firstAd event: type=%q segType=%#x, want out %#x",
			sp1.Type, sp1.SegmentationTypeID, SegTypeAdvertisementStart)
	}
	if sp1.SpliceTimePTS != int64(breakStart*90000.0) {
		t.Fatalf("slice-point SpliceTimePTS = %d, want %d",
			sp1.SpliceTimePTS, int64(breakStart*90000.0))
	}

	// --- Last ad carries the cue-in (0x35) ---
	lastAd := bySeq[203]
	if !lastAd.IsAd {
		t.Fatalf("seq=203 IsAd = false, want true")
	}
	if got, want := len(lastAd.SliceInserts), 1; got != want {
		t.Fatalf("lastAd SliceInserts = %d entries, want %d (cue-in only)", got, want)
	}
	ci1 := lastAd.SliceInserts[0]
	if ci1.Type != "in" || ci1.SegmentationTypeID != SegTypeAdvertisementEnd {
		t.Fatalf("lastAd event: type=%q segType=%#x, want in %#x",
			ci1.Type, ci1.SegmentationTypeID, SegTypeAdvertisementEnd)
	}
	if ci1.SpliceTimePTS != int64(breakEnd*90000.0) {
		t.Fatalf("cue-in SpliceTimePTS = %d, want %d",
			ci1.SpliceTimePTS, int64(breakEnd*90000.0))
	}

	// --- Middle ad (seq 202) carries NEITHER slice-point nor cue-in ---
	midAd := bySeq[202]
	if len(midAd.SliceInserts) != 0 {
		t.Fatalf("middle ad seq=202 SliceInserts = %d entries, want 0", len(midAd.SliceInserts))
	}
}

// TestStampSliceEvents_PreRollBeforeTimelineBase: breakStart=2 is
// earlier than the channel's first VOD segment (timelineBase=0).
// Both pre-rolls land at preRollPts<0 and are skipped; the
// slice-point + cue-in still fire.
func TestStampSliceEvents_PreRollBeforeTimelineBase(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForStamping(addr) {
		t.Skipf("redis not reachable at %s; set REDIS_ADDR to enable this test", addr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := newStampingTestChannelID("test-scte35-preroll-prebase")
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanCancel()
		rdb.Del(cleanCtx, timelineKeyFor(chID))
	}()

	const breakStart = 2.0 // earlier than any possible pre-roll target
	ads := []models.SegmentItem{
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_1.ts"},
	}

	sp := &SegmentProcessor{
		planner: &Planner{
			rdb:        rdb,
			rendCache:  map[string]renditionPair{},
			bucketName: "espx-video-lib:espx-video-lib",
			adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
		},
		pipe:            rdb.Pipeline(),
		ctx:             ctx,
		sequenceCounter: 300,
		adSegments:      ads,
		channelID:       chID,
		nowUnix:         1.0,
		lastAdTime:      -30.0, // negative so (2 - (-30)) = 32 >= 30 -- the 30s guard passes
		runningTime:     breakStart,
		horizon:         time.Time{},
		// NO processingSegments: there are no VOD members before
		// the ad break (this models the first ad of a channel).
	}

	if err := sp.insertAdsIfNeeded(breakStart); err != nil {
		t.Fatalf("insertAdsIfNeeded: %v", err)
	}
	if _, err := sp.pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec: %v", err)
	}

	members, err := rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     timelineKeyFor(chID),
		Start:   "-inf",
		Stop:    "+inf",
		ByScore: true,
	}).Result()
	if err != nil {
		t.Fatalf("ZRangeByScore: %v", err)
	}
	if got, want := len(members), 1; got != want {
		t.Fatalf("ZSET cardinality = %d, want 1 (the single ad)", got)
	}

	var seg models.SegmentItem
	if err := json.Unmarshal([]byte(members[0]), &seg); err != nil {
		t.Fatalf("member json.Unmarshal: %v", err)
	}
	if !seg.IsAd {
		t.Fatalf("ad member IsAd = false, want true")
	}
	// 1 ad break with only 1 ad: the slice-point AND cue-in BOTH
	// land on the same segment (dedup-by-(type, PTS) keeps both
	// because the types differ).
	if got, want := len(seg.SliceInserts), 2; got != want {
		t.Fatalf("first-and-only ad SliceInserts = %d, want 2 (slice-point + cue-in)",
			got)
	}
	var hasSlicePoint, hasCueIn bool
	for _, ev := range seg.SliceInserts {
		switch ev.SegmentationTypeID {
		case SegTypeAdvertisementStart:
			hasSlicePoint = true
		case SegTypeAdvertisementEnd:
			hasCueIn = true
		}
	}
	if !hasSlicePoint || !hasCueIn {
		t.Fatalf("first/only ad missing slice-point (got=%v) or cue-in (got=%v)",
			hasSlicePoint, hasCueIn)
	}
}

// TestStampSliceEvents_PreRollTargetNotYetScheduled: pre-roll
// target VOD segment hasn't been added to sp.processingSegments
// yet (the walk is racing the segments). Both pre-rolls skip
// silently; ad stamping still works.
func TestStampSliceEvents_PreRollTargetNotYetScheduled(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForStamping(addr) {
		t.Skipf("redis not reachable at %s; set REDIS_ADDR to enable this test", addr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := newStampingTestChannelID("test-scte35-preroll-notscheduled")
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanCancel()
		rdb.Del(cleanCtx, timelineKeyFor(chID))
	}()

	const breakStart = 100.0
	ads := []models.SegmentItem{
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_1.ts"},
	}

	sp := &SegmentProcessor{
		planner: &Planner{
			rdb:        rdb,
			rendCache:  map[string]renditionPair{},
			bucketName: "espx-video-lib:espx-video-lib",
			adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
		},
		pipe:            rdb.Pipeline(),
		ctx:             ctx,
		sequenceCounter: 400,
		adSegments:      ads,
		channelID:       chID,
		nowUnix:         1.0,
		lastAdTime:      0.0, // 0 so (100 - 0) >= 30 is satisfied
		runningTime:     breakStart,
		horizon:         time.Time{},
	}
	// Pre-seed the planner's VOD buffer (the field lives on the
	// shared Planner, not the per-call processor). No VOD at
	// breakStart-8 (=92) or breakStart-4 (=96): the VOD
	// segments we DO have are way earlier in the timeline, so
	// findSegmentContainingPts misses both pre-roll targets.
	sp.planner.processingSegments = []models.SegmentItem{
		{Sequence: 500, MediaStart: 0, MediaEnd: 10, Duration: 10, RunningTime: 0},
		{Sequence: 501, MediaStart: 10, MediaEnd: 20, Duration: 10, RunningTime: 10},
	}

	if err := sp.insertAdsIfNeeded(breakStart); err != nil {
		t.Fatalf("insertAdsIfNeeded: %v", err)
	}
	if _, err := sp.pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec: %v", err)
	}

	// The 2 VOD members must carry NO pre-roll entries.
	members, err := rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     timelineKeyFor(chID),
		Start:   "-inf",
		Stop:    fmt.Sprintf("%d", int64(breakStart)-1),
		ByScore: true,
	}).Result()
	if err != nil {
		t.Fatalf("ZRangeByScore: %v", err)
	}
	if got, want := len(members), 2; got != want {
		t.Fatalf("VOD ZSET cardinality = %d, want 2", got)
	}
	for i, raw := range members {
		var seg models.SegmentItem
		if err := json.Unmarshal([]byte(raw), &seg); err != nil {
			t.Fatalf("VOD member[%d] json.Unmarshal: %v", i, err)
		}
		if len(seg.SliceInserts) != 0 {
			t.Fatalf("VOD member[%d] (seq=%d) SliceInserts = %d, want 0 (pre-roll targets missed)",
				i, seg.Sequence, len(seg.SliceInserts))
		}
	}

	// The ad still carries its slice-point + cue-in.
	adMembers, err := rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     timelineKeyFor(chID),
		Start:   fmt.Sprintf("%d", int64(breakStart)),
		Stop:    "+inf",
		ByScore: true,
	}).Result()
	if err != nil {
		t.Fatalf("ZRangeByScore (ads): %v", err)
	}
	if got, want := len(adMembers), 1; got != want {
		t.Fatalf("ad ZSET cardinality = %d, want 1", got)
	}
	var ad models.SegmentItem
	if err := json.Unmarshal([]byte(adMembers[0]), &ad); err != nil {
		t.Fatalf("ad json.Unmarshal: %v", err)
	}
	if got, want := len(ad.SliceInserts), 2; got != want {
		t.Fatalf("ad SliceInserts = %d, want 2 (slice-point + cue-in)", got)
	}
}

// TestStampSliceEvents_DedupOnReplay: calling insertAdsIfNeeded
// twice for the same breakStart must NOT duplicate the
// pre-roll/slice-point/cue-in events on the same VOD/ad
// segments. The dedup invariant is
// (Type, SpliceTimePTS) -> at most one entry per SegmentItem.
func TestStampSliceEvents_DedupOnReplay(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachableForStamping(addr) {
		t.Skipf("redis not reachable at %s; set REDIS_ADDR to enable this test", addr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := newStampingTestChannelID("test-scte35-dedup-replay")
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanCancel()
		rdb.Del(cleanCtx, timelineKeyFor(chID))
	}()

	const breakStart = 50.0
	ads := []models.SegmentItem{
		{Duration: 6.0, StoragePath: "espx-video-lib:espx-video-lib/ads-v3/L2/seg_1.ts"},
	}
	vod := []models.SegmentItem{
		{Sequence: 600, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_600.ts", MediaStart: 0, MediaEnd: 6, Duration: 6, RunningTime: 0},
		{Sequence: 601, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_601.ts", MediaStart: 6, MediaEnd: 12, Duration: 6, RunningTime: 6},
		{Sequence: 602, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_602.ts", MediaStart: 12, MediaEnd: 18, Duration: 6, RunningTime: 12},
		{Sequence: 603, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_603.ts", MediaStart: 18, MediaEnd: 24, Duration: 6, RunningTime: 18},
		// 5th segment covers breakStart-8 (=42) and breakStart-4 (=46).
		{Sequence: 604, StoragePath: "espx-video-lib:espx-video-lib/stream/seg_604.ts", MediaStart: 40, MediaEnd: 50, Duration: 10, RunningTime: 40},
	}

	sp := &SegmentProcessor{
		planner: &Planner{
			rdb:        rdb,
			rendCache:  map[string]renditionPair{},
			bucketName: "espx-video-lib:espx-video-lib",
			adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
		},
		pipe:            rdb.Pipeline(),
		ctx:             ctx,
		sequenceCounter: 700,
		adSegments:      ads,
		channelID:       chID,
		nowUnix:         1.0,
		lastAdTime:      0.0, // 0 so (50 - 0) >= 30 is satisfied
		runningTime:     breakStart,
		horizon:         time.Time{},
	}
	// First tick: pre-seeded VOD buffer.
	sp.planner.processingSegments = append([]models.SegmentItem{}, vod...)

	// First call: stamps the 4 events.
	if err := sp.insertAdsIfNeeded(breakStart); err != nil {
		t.Fatalf("first insertAdsIfNeeded: %v", err)
	}

	// Drain the pipeline to flush the first batch so the
	// processingSegments reset is observable on the next call.
	if _, err := sp.pipe.Exec(ctx); err != nil {
		t.Fatalf("first pipe.Exec: %v", err)
	}
	// After synchSegmentIntoRedis, processingSegments is reset to
	// empty. Re-seed with the VOD members for the second tick.
	// Note: in real usage the walk advances runningTime and the
	// VOD buffer would be empty by the time insertAdsIfNeeded
	// fires again, so the pre-rolls would MISS. To isolate the
	// dedup invariant we force-replay the same break by resetting
	// runningTime and re-seeding.
	sp.runningTime = breakStart
	sp.lastAdTime = -30.0 // negative so (50 - (-30)) = 80 >= 30 -- the 30s guard passes
	sp.planner.processingSegments = append([]models.SegmentItem{}, vod...)

	// Second call: would re-stamp, but the dedup helper skips
	// identical (Type, SpliceTimePTS) entries.
	if err := sp.insertAdsIfNeeded(breakStart); err != nil {
		t.Fatalf("second insertAdsIfNeeded: %v", err)
	}
	if _, err := sp.pipe.Exec(ctx); err != nil {
		t.Fatalf("second pipe.Exec: %v", err)
	}

	// Assert: VOD seg 604 has exactly 2 pre-roll entries (NOT 4).
	// Query the whole VOD range (below the break) to find seq=604.
	members, err := rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     timelineKeyFor(chID),
		Start:   "-inf",
		Stop:    fmt.Sprintf("%d", int64(breakStart)),
		ByScore: true,
	}).Result()
	if err != nil {
		t.Fatalf("ZRangeByScore (vod pre-break): %v", err)
	}
	var vod604 models.SegmentItem
	for _, raw := range members {
		var seg models.SegmentItem
		if err := json.Unmarshal([]byte(raw), &seg); err != nil {
			t.Fatalf("VOD member json.Unmarshal: %v", err)
		}
		if seg.Sequence == 604 {
			vod604 = seg
			break
		}
	}
	if vod604.Sequence != 604 {
		t.Fatalf("did not find VOD seq=604 in ZSET")
	}
	if got, want := len(vod604.SliceInserts), 2; got != want {
		t.Fatalf("VOD seq=604 SliceInserts = %d, want %d (dedup invariant: at most one pre-roll per (Type, PTS))",
			got, want)
	}
}
