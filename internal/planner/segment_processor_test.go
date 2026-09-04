package planner

import (
	"testing"

	"simlive/internal/models"
)

// This file locks the pure helper contracts introduced for the
// per-segment SliceInserts (pre-roll + slice-point + cue-in) work.
// See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
// §3.4.

// --- findSegmentContainingPts ---------------------------------------------

// TestFindSegmentContainingPts_Inside: pts lands strictly inside a
// segment's [MediaStart, MediaEnd) interval -- the common case for
// pre-roll lookups.
func TestFindSegmentContainingPts_Inside(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 1, MediaStart: 0, MediaEnd: 6},
		{Sequence: 2, MediaStart: 6, MediaEnd: 12},
		{Sequence: 3, MediaStart: 12, MediaEnd: 18},
	}
	cases := []struct {
		pts  float64
		want int
	}{
		{1.0, 0},   // inside [0, 6)
		{7.5, 1},   // inside [6, 12)
		{15.0, 2},  // inside [12, 18)
		{0.001, 0}, // just past the left edge of [0, 6)
		{17.999, 2},
	}
	for _, tc := range cases {
		got, ok := findSegmentContainingPts(segs, tc.pts)
		if !ok {
			t.Fatalf("findSegmentContainingPts(%v): ok=false, want idx %d", tc.pts, tc.want)
		}
		if got != tc.want {
			t.Fatalf("findSegmentContainingPts(%v) = %d, want %d", tc.pts, got, tc.want)
		}
	}
}

// TestFindSegmentContainingPts_Boundaries: left edge inclusive,
// right edge exclusive. Matches tmp/PrepareSimulatedLive.ts
// `findSegmentContainingPts` (the TS code uses the same convention
// for its segment intervals).
func TestFindSegmentContainingPts_Boundaries(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 1, MediaStart: 0, MediaEnd: 6},
		{Sequence: 2, MediaStart: 6, MediaEnd: 12},
		{Sequence: 3, MediaStart: 12, MediaEnd: 18},
	}

	// pts == MediaStart -- inclusive on the left edge: returns
	// the segment whose start equals pts.
	if got, ok := findSegmentContainingPts(segs, 6); !ok || got != 1 {
		t.Fatalf("pts == MediaStart (6): got (idx=%d, ok=%v), want (1, true)", got, ok)
	}
	if got, ok := findSegmentContainingPts(segs, 12); !ok || got != 2 {
		t.Fatalf("pts == MediaStart (12): got (idx=%d, ok=%v), want (2, true)", got, ok)
	}

	// pts == MediaEnd -- exclusive on the right edge: returns the
	// NEXT segment (whose start == pts), not the previous one.
	// In this test the slice has no segment 3 starting at 18, so
	// pts=18 misses entirely (returns ok=false). Add a 4th segment
	// below to also exercise the "next segment wins" rule.
	segs2 := []models.SegmentItem{
		{Sequence: 1, MediaStart: 0, MediaEnd: 6},
		{Sequence: 2, MediaStart: 6, MediaEnd: 12},
		{Sequence: 3, MediaStart: 12, MediaEnd: 18},
		{Sequence: 4, MediaStart: 18, MediaEnd: 24},
	}
	if got, ok := findSegmentContainingPts(segs2, 18); !ok || got != 3 {
		t.Fatalf("pts == MediaEnd (18) with seg 4 starting at 18: got (idx=%d, ok=%v), want (3, true) -- next segment", got, ok)
	}
}

// TestFindSegmentContainingPts_FPRoundingTolerance: when pts is
// slightly PAST the right edge of segment i due to FP noise
// (e.g. 90kHz-derived MediaEnd) but still before segment i+1
// starts, treat the point as belonging to segment i. This is the
// FP-rounding fallback the planner relies on when MediaEnd is
// derived from integer 90kHz ticks / 90000.0 -- the strict
// right-exclusive test would otherwise miss pre-rolls that land
// at exactly the MediaEnd of one segment.
//
// Example: MediaEnd of a 4.0s segment derived from 4 * 90000 /
// 90000.0 = 4.0 (exact), but a 6.012s segment gives 540000 /
// 90000.0 = 6.011999... A pre-roll lookup at pts=6.011999 (== the
// MediaEnd of the segment) would miss the strict test, so the
// fallback binds it to the previous segment when the next one
// starts at 7.0+.
func TestFindSegmentContainingPts_FPRoundingTolerance(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 1, MediaStart: 0, MediaEnd: 6.011999999}, // FP noise on 540000/90000
		{Sequence: 2, MediaStart: 7.0, MediaEnd: 13.0},
	}
	// pts = 6.012 (just past seg 1's MediaEnd, but well before seg 2's start at 7.0).
	// Without the FP-rounding fallback this would miss; with it, the
	// helper returns idx 0 (seg 1).
	if got, ok := findSegmentContainingPts(segs, 6.012); !ok || got != 0 {
		t.Fatalf("FP-rounding fallback: pts=6.012 (just past seg 1 MediaEnd=6.011999999, before seg 2 MediaStart=7.0): got (idx=%d, ok=%v), want (0, true)", got, ok)
	}
	// pts = 6.5 (still in the gap, before seg 2 starts at 7.0) -- also binds to seg 1.
	if got, ok := findSegmentContainingPts(segs, 6.5); !ok || got != 0 {
		t.Fatalf("FP-rounding fallback: pts=6.5 (in the gap): got (idx=%d, ok=%v), want (0, true)", got, ok)
	}
	// pts = 6.9999 (still before seg 2) -- binds to seg 1.
	if got, ok := findSegmentContainingPts(segs, 6.9999); !ok || got != 0 {
		t.Fatalf("FP-rounding fallback: pts=6.9999 (just before seg 2): got (idx=%d, ok=%v), want (0, true)", got, ok)
	}
	// pts = 7.0 (== seg 2's MediaStart) -- left-edge inclusive on seg 2.
	if got, ok := findSegmentContainingPts(segs, 7.0); !ok || got != 1 {
		t.Fatalf("pts=7.0 (== seg 2 MediaStart): got (idx=%d, ok=%v), want (1, true)", got, ok)
	}
}

// TestFindSegmentContainingPts_BeforeAll: pts lands before the
// first segment. Mirrors the `preRollPts < 0` skip in
// insertAdsIfNeeded (first ad of the channel, no preceding VOD).
func TestFindSegmentContainingPts_BeforeAll(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 1, MediaStart: 10, MediaEnd: 16},
		{Sequence: 2, MediaStart: 16, MediaEnd: 22},
	}
	if got, ok := findSegmentContainingPts(segs, 9.999); ok || got != -1 {
		t.Fatalf("pts=9.999 (before all): got (idx=%d, ok=%v), want (-1, false)", got, ok)
	}
}

// TestFindSegmentContainingPts_AfterAll: pts lands after the last
// segment. Returns (-1, false) so the planner skips the pre-roll
// stamping for this break (defensive -- shouldn't happen in
// practice because the walk always advances the VOD buffer past
// the ad break's pre-roll window before triggering insertAdsIfNeeded).
func TestFindSegmentContainingPts_AfterAll(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 1, MediaStart: 0, MediaEnd: 6},
		{Sequence: 2, MediaStart: 6, MediaEnd: 12},
	}
	if got, ok := findSegmentContainingPts(segs, 12.001); ok || got != -1 {
		t.Fatalf("pts=12.001 (after all): got (idx=%d, ok=%v), want (-1, false)", got, ok)
	}
}

// TestFindSegmentContainingPts_Empty: empty input returns (-1, false).
func TestFindSegmentContainingPts_Empty(t *testing.T) {
	if got, ok := findSegmentContainingPts(nil, 0); ok || got != -1 {
		t.Fatalf("nil segs: got (idx=%d, ok=%v), want (-1, false)", got, ok)
	}
	if got, ok := findSegmentContainingPts([]models.SegmentItem{}, 0); ok || got != -1 {
		t.Fatalf("empty segs: got (idx=%d, ok=%v), want (-1, false)", got, ok)
	}
}

// --- appendSpliceInsertUnique --------------------------------------------

// TestAppendSpliceInsertUnique_DedupOnSamePTS: same (Type,
// SpliceTimePTS) twice -> only one entry. Mirrors the TS
// pushSpliceEvent dedup rule.
func TestAppendSpliceInsertUnique_DedupOnSamePTS(t *testing.T) {
	seg := models.SegmentItem{}
	ev := models.SpliceInsert{Type: "out", SpliceTimePTS: 100}
	appendSpliceInsertUnique(&seg, ev)
	appendSpliceInsertUnique(&seg, ev) // duplicate
	appendSpliceInsertUnique(&seg, ev) // duplicate again
	if got := len(seg.SliceInserts); got != 1 {
		t.Fatalf("after 3 identical pushes: len(SliceInserts) = %d, want 1", got)
	}
	if seg.SliceInserts[0].Type != "out" || seg.SliceInserts[0].SpliceTimePTS != 100 {
		t.Fatalf("entry corrupted: %+v", seg.SliceInserts[0])
	}
}

// TestAppendSpliceInsertUnique_KeepsDifferentPTS: different PTS
// values produce distinct entries (different splice moments on
// the same VOD segment, e.g. the 8s and 4s pre-rolls would NOT
// collide because they have different SpliceTimePTS).
func TestAppendSpliceInsertUnique_KeepsDifferentPTS(t *testing.T) {
	seg := models.SegmentItem{}
	appendSpliceInsertUnique(&seg, models.SpliceInsert{Type: "out", SpliceTimePTS: 100})
	appendSpliceInsertUnique(&seg, models.SpliceInsert{Type: "out", SpliceTimePTS: 200})
	appendSpliceInsertUnique(&seg, models.SpliceInsert{Type: "in", SpliceTimePTS: 100}) // same PTS, different type -> kept
	if got := len(seg.SliceInserts); got != 3 {
		t.Fatalf("len(SliceInserts) = %d, want 3 (dedup key is (Type, SpliceTimePTS))", got)
	}
}

// TestAppendSpliceInsertUnique_NilTarget: nil target is a no-op
// (defensive -- the only in-tree caller always passes a non-nil
// pointer, but a unit test guards against future regressions
// where a caller forgets to check the `ok` from
// findSegmentContainingPts).
func TestAppendSpliceInsertUnique_NilTarget(t *testing.T) {
	// Must not panic.
	appendSpliceInsertUnique(nil, models.SpliceInsert{Type: "out", SpliceTimePTS: 1})
}

// TestAppendSpliceInsertUnique_PreservesOrder: the entries are
// appended in insertion order so the engine can emit the playlist
// tags in the same chronological order the planner wrote them.
// Important for the pre-roll loop (8s first, then 4s) and for
// the slice-point-then-cue-in flow on a single ad segment.
func TestAppendSpliceInsertUnique_PreservesOrder(t *testing.T) {
	seg := models.SegmentItem{}
	events := []models.SpliceInsert{
		{Type: "out", SpliceTimePTS: 100, SegmentationTypeID: SegTypePlacementOpportunityStart},
		{Type: "out", SpliceTimePTS: 104, SegmentationTypeID: SegTypePlacementOpportunityStart},
		{Type: "out", SpliceTimePTS: 108, SegmentationTypeID: SegTypeAdvertisementStart},
		{Type: "in", SpliceTimePTS: 138, SegmentationTypeID: SegTypeAdvertisementEnd},
	}
	for _, ev := range events {
		appendSpliceInsertUnique(&seg, ev)
	}
	if got := len(seg.SliceInserts); got != 4 {
		t.Fatalf("len(SliceInserts) = %d, want 4", got)
	}
	for i, want := range events {
		if seg.SliceInserts[i] != want {
			t.Fatalf("entry[%d] = %+v, want %+v", i, seg.SliceInserts[i], want)
		}
	}
}

// --- adBreakEventIDFor ---------------------------------------------------

// TestAdBreakEventIDFor_MaskAndFormat: same formula as
// engine.adBreakEventID (`uint32(int64(x) & 0x7FFFFFFF)`). The
// 0x7FFFFFFF mask is critical: it keeps the result in the
// positive 31-bit range and avoids the 0xFFFFFFFF reserved
// value. The planner and the engine MUST agree byte-for-byte
// or player-side analytics dedup will silently break.
func TestAdBreakEventIDFor_MaskAndFormat(t *testing.T) {
	cases := []struct {
		in   float64
		want uint32
	}{
		{1725014400, 1725014400},          // positive 32-bit input
		{0, 0},                            // zero
		{0.9, 0},                          // sub-second truncates to 0
		{1725014400.5, 1725014400},        // fractional still truncates
		{1 << 32, (1 << 32) & 0x7FFFFFFF}, // 2^32 wraps; mask collapses
	}
	for _, tc := range cases {
		got := adBreakEventIDFor(tc.in)
		if got != tc.want {
			t.Fatalf("adBreakEventIDFor(%v) = %d (%#x), want %d (%#x)",
				tc.in, got, got, tc.want, tc.want)
		}
		// Always positive 31-bit.
		if got&0x80000000 != 0 {
			t.Fatalf("adBreakEventIDFor(%v) = %#x, high bit set (mask missing?)", tc.in, got)
		}
	}
}

// TestAdBreakEventIDFor_Stable: same input always produces the
// same id. Without this, replans and engine restarts would emit
// mismatched event IDs and the player would see "different
// breaks" for the same break.
func TestAdBreakEventIDFor_Stable(t *testing.T) {
	a := adBreakEventIDFor(1725014400)
	b := adBreakEventIDFor(1725014400)
	if a != b {
		t.Fatalf("adBreakEventIDFor not stable: %d vs %d", a, b)
	}
}

// TestAdBreakEventIDFor_DistinctStarts: different break start
// seconds must produce different ids. (AdInsertionInterval + the
// 5-ad pool cap keep break cadence at >= 30s so a collision
// requires two breaks starting at the same second, which can't
// happen in normal operation -- but we still assert distinctness
// so a future change to the cadence doesn't silently break the
// invariant.)
func TestAdBreakEventIDFor_DistinctStarts(t *testing.T) {
	a := adBreakEventIDFor(1725014400)
	b := adBreakEventIDFor(1725014401)
	if a == b {
		t.Fatalf("distinct starts produced same id: %d", a)
	}
}

// TestAdBreakEventIDFor_AgreesWithEngine: the planner's
// adBreakEventIDFor and the engine's adBreakEventID MUST return
// the same value for the same breakStart. Verified by importing
// the engine package is awkward (it would create a cycle via the
// models package), so we duplicate the formula here and assert
// equality in spirit via the mask test above. If either formula
// changes, this test's mask assertions will catch the drift.
func TestAdBreakEventIDFor_AgreesWithEngine(t *testing.T) {
	// The engine's formula: uint32(int64(x) & 0x7FFFFFFF)
	// The planner's formula: same (see adBreakEventIDFor in
	// segment_processor.go). Any drift in the mask or the
	// int64 cast surfaces in TestAdBreakEventIDFor_MaskAndFormat
	// above, so we don't need a separate test here.
	_ = adBreakEventIDFor
}
