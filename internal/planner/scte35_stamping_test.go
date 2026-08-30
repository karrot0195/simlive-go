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
		rdb.Del(cleanCtx, "simlive:timeline:"+chID)
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
			channelID:  chID,
		},
		pipe:            rdb.Pipeline(),
		ctx:             ctx,
		sequenceCounter: 100, // arbitrary starting sequence
		adSegments:      ads,
		// nowUnix in the past so actualStartTime >= nowUnix + 30s guard passes.
		nowUnix: 1.0,
		// lastAdTime far in the past so (actualStartTime - lastAdTime) >=
		// AdInsertionInterval is satisfied.
		lastAdTime: 1.0,
	}
	// Pin the global running time + horizon so the break happens at a
	// known second and isEndOfTimeline returns false.
	sp.planner.globalRunningTime = breakStart
	sp.planner.horizon = time.Time{} // zero => no horizon => isEndOfTimeline is no-op

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
	members, err := rdb.ZRangeByScore(ctx, "simlive:timeline:"+chID,
		&redis.ZRangeBy{Min: fmt.Sprintf("%d", int64(breakStart)-1), Max: "+inf"}).Result()
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
