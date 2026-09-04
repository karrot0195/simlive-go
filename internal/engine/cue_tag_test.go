package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"simlive/internal/models"
)

// TestBuildMediaPlaylistBody_NoAds: a VOD-only window produces the same
// shape as today's generateHLSPlaylist (no DATERANGE lines). The .pts
// filter is the caller's job; buildMediaPlaylistBody trusts its input.
func TestBuildMediaPlaylistBody_NoAds(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 100, Duration: 4.0},
		{Sequence: 101, Duration: 4.0},
		{Sequence: 102, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 4)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}
	if strings.Contains(out, "EXT-X-DATERANGE") {
		t.Fatalf("no ads expected, but DATERANGE found:\n%s", out)
	}
	for _, want := range []string{
		"#EXTM3U\n",
		"#EXT-X-VERSION:3\n",
		"#EXT-X-TARGETDURATION:4\n",
		"#EXT-X-START:TIME-OFFSET=0\n",
		"#EXT-X-MEDIA-SEQUENCE:100\n",
		"#EXTINF:4.000,\n",
		"seg_100.ts\n",
		"seg_101.ts\n",
		"seg_102.ts\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestBuildMediaPlaylistBody_CueOutBeforeAd: VOD->VOD->AD->AD->VOD
// sequence must emit a CUE-OUT DATERANGE line BEFORE the first ad's
// #EXTINF, with all the required attributes (ID, CLASS, START-DATE,
// SCTE35-OUT, DURATION, END-ON-NEXT=YES trailing).
func TestBuildMediaPlaylistBody_CueOutBeforeAd(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 200, Duration: 4.0},
		{Sequence: 201, Duration: 4.0},
		{Sequence: 202, Duration: 6.0, IsAd: true, AdBreakID: "adbreak-1725014400", AdBreakStart: 1725014400, AdBreakDuration: 30},
		{Sequence: 203, Duration: 6.0, IsAd: true, AdBreakID: "adbreak-1725014400", AdBreakStart: 1725014400, AdBreakDuration: 30},
		{Sequence: 204, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 6)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}
	// Exactly one CUE-OUT and one CUE-IN.
	if got := strings.Count(out, "SCTE35-OUT="); got != 1 {
		t.Fatalf("SCTE35-OUT count = %d, want 1:\n%s", got, out)
	}
	if got := strings.Count(out, "SCTE35-IN="); got != 1 {
		t.Fatalf("SCTE35-IN count = %d, want 1:\n%s", got, out)
	}
	// CUE-OUT must come BEFORE the first ad EXTINF.
	cueOutIdx := strings.Index(out, `SCTE35-OUT=`)
	firstAdIdx := strings.Index(out, "#EXTINF:6.000,")
	if cueOutIdx < 0 || firstAdIdx < 0 || cueOutIdx > firstAdIdx {
		t.Fatalf("CUE-OUT must precede first ad EXTINF: cueOut=%d ad=%d\n%s", cueOutIdx, firstAdIdx, out)
	}
	// CUE-IN must come AFTER the last ad EXTINF and BEFORE the
	// first post-ad EXTINF.
	cueInIdx := strings.Index(out, `SCTE35-IN=`)
	lastAdIdx := strings.Index(out, "seg_203.ts\n")
	postVodIdx := strings.Index(out, "#EXTINF:4.000,\nseg_204.ts")
	if cueInIdx < 0 || lastAdIdx < 0 || postVodIdx < 0 {
		t.Fatalf("missing markers in:\n%s", out)
	}
	if !(lastAdIdx < cueInIdx && cueInIdx < postVodIdx) {
		t.Fatalf("CUE-IN must sit between last ad and first post-ad VOD: lastAd=%d cueIn=%d postVod=%d\n%s",
			lastAdIdx, cueInIdx, postVodIdx, out)
	}
}

// TestBuildMediaPlaylistBody_CueOutWhenFirstSegIsAd: cold start
// with an ad as the first segment must still emit CUE-OUT before
// the first EXTINF (the player can't infer the break boundary from
// prior state).
func TestBuildMediaPlaylistBody_CueOutWhenFirstSegIsAd(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 300, Duration: 6.0, IsAd: true, AdBreakID: "adbreak-100", AdBreakStart: 100, AdBreakDuration: 12},
		{Sequence: 301, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 6)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}
	if !strings.Contains(out, `SCTE35-OUT=`) {
		t.Fatalf("first-seg-is-ad must still emit CUE-OUT:\n%s", out)
	}
	if !strings.Contains(out, `SCTE35-IN=`) {
		t.Fatalf("ad->VOD must emit CUE-IN:\n%s", out)
	}
}

// TestBuildMediaPlaylistBody_MultipleBreaks: two separate breaks
// in the window produce two CUE-OUT / CUE-IN pairs with distinct
// IDs.
func TestBuildMediaPlaylistBody_MultipleBreaks(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 400, Duration: 4.0},
		{Sequence: 401, Duration: 6.0, IsAd: true, AdBreakID: "adbreak-1", AdBreakStart: 1, AdBreakDuration: 6},
		{Sequence: 402, Duration: 4.0},
		{Sequence: 403, Duration: 6.0, IsAd: true, AdBreakID: "adbreak-2", AdBreakStart: 2, AdBreakDuration: 6},
		{Sequence: 404, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 6)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}
	if got := strings.Count(out, "SCTE35-OUT="); got != 2 {
		t.Fatalf("SCTE35-OUT count = %d, want 2:\n%s", got, out)
	}
	if got := strings.Count(out, "SCTE35-IN="); got != 2 {
		t.Fatalf("SCTE35-IN count = %d, want 2:\n%s", got, out)
	}
	if !strings.Contains(out, `ID="adbreak-1"`) || !strings.Contains(out, `ID="adbreak-2"`) {
		t.Fatalf("expected both break IDs:\n%s", out)
	}
}

// TestBuildMediaPlaylistBody_Empty: empty segments -> error (the caller
// in generateHLSPlaylist already short-circuits on empty, but the pure
// helper must be honest about the contract).
func TestBuildMediaPlaylistBody_Empty(t *testing.T) {
	if _, err := buildMediaPlaylistBody(nil, 4); err == nil {
		t.Fatalf("expected error on empty segments")
	}
}

// TestBuildMediaPlaylistBody_TrailingUnquotedAttr: lock in the
// threefive TagParser gotcha (see tmp/SCTE35Utils.ts#L51-L59) --
// CUE-OUT line must end with unquoted END-ON-NEXT=YES; CUE-IN
// line must end with unquoted DURATION=<n>.
func TestBuildMediaPlaylistBody_TrailingUnquotedAttr(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 500, Duration: 4.0},
		{Sequence: 501, Duration: 6.0, IsAd: true, AdBreakID: "adbreak-9", AdBreakStart: 9, AdBreakDuration: 6},
		{Sequence: 502, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 6)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}
	lines := strings.Split(out, "\n")
	var cueOutLine, cueInLine string
	for _, ln := range lines {
		if strings.Contains(ln, "SCTE35-OUT=") {
			cueOutLine = ln
		}
		if strings.Contains(ln, "SCTE35-IN=") {
			cueInLine = ln
		}
	}
	if cueOutLine == "" || cueInLine == "" {
		t.Fatalf("missing CUE-OUT/IN in:\n%s", out)
	}
	if !strings.HasSuffix(cueOutLine, "END-ON-NEXT=YES") {
		t.Fatalf("CUE-OUT must end with unquoted END-ON-NEXT=YES:\n%s", cueOutLine)
	}
	if !strings.HasSuffix(cueInLine, `DURATION=6`) {
		t.Fatalf("CUE-IN must end with unquoted DURATION=6:\n%s", cueInLine)
	}
}

// TestBuildMediaPlaylistBody_SegmentItemRoundTrip: the SegmentItem
// shape the planner stamps (IsAd + 3 ad fields) survives a JSON
// round-trip -- this is the contract the engine relies on when it
// reads the timeline back from Redis.
func TestBuildMediaPlaylistBody_SegmentItemRoundTrip(t *testing.T) {
	src := models.SegmentItem{
		Sequence: 600, Duration: 6.0,
		IsAd:            true,
		AdBreakID:       "adbreak-1725014400",
		AdBreakStart:    1725014400,
		AdBreakDuration: 30,
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"is_ad":true`,
		`"ad_break_id":"adbreak-1725014400"`,
		`"ad_break_start":1725014400`,
		`"ad_break_duration":30`,
	} {
		if !strings.Contains(string(b), want) {
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

// --- Per-segment #EXT-X-SCTE35 emission (slice inserts) -----------------
//
// Added for docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md.
// The planner stamps 1-2 SpliceInserts on each segment that participates
// in an ad break; the engine emits them as per-segment
// #EXT-X-SCTE35 lines in the media playlist.

// TestBuildMediaPlaylistBody_EmitsSpliceInserts_BeforeEXTINF: a
// VOD segment with a pre-roll SpliceInserts entry gets a
// #EXT-X-SCTE35 line emitted BEFORE the segment's #EXTINF (and
// after any DATERANGE the edge detection already produced).
// Lock the contract so SSAI clients can rely on the per-segment
// splice event being on the segment that carries the
// pre-roll/slice-point/cue-in.
func TestBuildMediaPlaylistBody_EmitsSpliceInserts_BeforeEXTINF(t *testing.T) {
	segs := []models.SegmentItem{
		// VOD seg at breakStart-8 with the 8s pre-roll.
		{
			Sequence: 700, Duration: 4.0,
			SliceInserts: []models.SpliceInsert{
				{
					Type:               "out",
					EventID:            1725014400,
					SpliceTimePTS:      (1725014400 - 8) * 90000,
					SpliceTimeSeconds:  1725014400 - 8,
					Duration:           30.0,
					UniqueProgramID:    1725014400,
					SegmentationTypeID: 0x30,
				},
			},
		},
		// VOD seg at breakStart-4 with the 4s pre-roll.
		{
			Sequence: 701, Duration: 4.0,
			SliceInserts: []models.SpliceInsert{
				{
					Type:               "out",
					EventID:            1725014400,
					SpliceTimePTS:      (1725014400 - 4) * 90000,
					SpliceTimeSeconds:  1725014400 - 4,
					Duration:           30.0,
					UniqueProgramID:    1725014400,
					SegmentationTypeID: 0x30,
				},
			},
		},
		// 2 ads: first carries the slice-point, last carries the cue-in.
		{
			Sequence: 702, Duration: 6.0,
			IsAd:            true,
			AdBreakID:       "adbreak-1725014400",
			AdBreakStart:    1725014400,
			AdBreakDuration: 12,
			SliceInserts: []models.SpliceInsert{
				{
					Type:               "out",
					EventID:            1725014400,
					SpliceTimePTS:      1725014400 * 90000,
					SpliceTimeSeconds:  1725014400,
					Duration:           12.0,
					UniqueProgramID:    1725014400,
					SegmentationTypeID: 0x34,
				},
			},
		},
		{
			Sequence: 703, Duration: 6.0,
			IsAd:            true,
			AdBreakID:       "adbreak-1725014400",
			AdBreakStart:    1725014400,
			AdBreakDuration: 12,
			SliceInserts: []models.SpliceInsert{
				{
					Type:               "in",
					EventID:            1725014400,
					SpliceTimePTS:      (1725014400 + 12) * 90000,
					SpliceTimeSeconds:  1725014400 + 12,
					UniqueProgramID:    1725014400,
					SegmentationTypeID: 0x35,
				},
			},
		},
		// Post-ad VOD.
		{Sequence: 704, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 6)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}

	// Exactly 4 #EXT-X-SCTE35 lines (2 pre-rolls + slice-point + cue-in).
	if got := strings.Count(out, "#EXT-X-SCTE35:"); got != 4 {
		t.Fatalf("EXT-X-SCTE35 count = %d, want 4:\n%s", got, out)
	}

	// Each #EXT-X-SCTE35 line must sit BEFORE the matching segment's
	// #EXTINF (and after any DATERANGE for the same edge).
	type check struct {
		seg     int64
		tagAttr string
	}
	checks := []check{
		{700, "TYPE=48"}, // 0x30 = 48 (pre-roll)
		{701, "TYPE=48"}, // 0x30 = 48 (pre-roll)
		{702, "TYPE=52"}, // 0x34 = 52 (slice-point)
		{703, "TYPE=53"}, // 0x35 = 53 (cue-in)
	}
	for _, c := range checks {
		tagIdx := strings.Index(out, c.tagAttr)
		extinfIdx := strings.Index(out, fmt.Sprintf("seg_%d.ts", c.seg))
		if tagIdx < 0 {
			t.Fatalf("tag with %s not found in:\n%s", c.tagAttr, out)
		}
		if extinfIdx < 0 {
			t.Fatalf("seg_%d.ts not found in:\n%s", c.seg, out)
		}
		if tagIdx >= extinfIdx {
			t.Fatalf("tag (%s) at idx %d must precede seg_%d.ts at idx %d",
				c.tagAttr, tagIdx, c.seg, extinfIdx)
		}
	}

	// All 4 tags share the same adbreak id.
	if got := strings.Count(out, `ID="adbreak-1725014400"`); got != 4 {
		t.Fatalf("matching adbreak ID count = %d, want 4 (one per splice event):\n%s", got, out)
	}

	// CUE-OUT (DATERANGE) must come BEFORE the slice-point tag
	// (DATERANGE is the lead marker for the VOD<>ad edge; the
	// slice-point tag is a per-segment insert on the first ad).
	daterangeOut := strings.Index(out, "SCTE35-OUT=")
	slicePointTag := strings.Index(out, "TYPE=52")
	if daterangeOut < 0 || slicePointTag < 0 {
		t.Fatalf("missing DATERANGE CUE-OUT or slice-point tag in:\n%s", out)
	}
	if daterangeOut >= slicePointTag {
		t.Fatalf("DATERANGE CUE-OUT (idx %d) must precede slice-point tag (idx %d):\n%s",
			daterangeOut, slicePointTag, out)
	}
}

// TestBuildMediaPlaylistBody_EmptySliceInserts: a segment with
// SliceInserts=nil (or empty) must not emit any #EXT-X-SCTE35
// line. The VOD-only path stays byte-identical to the pre-feature
// playlist, preserving the existing player compatibility.
func TestBuildMediaPlaylistBody_EmptySliceInserts(t *testing.T) {
	segs := []models.SegmentItem{
		{Sequence: 800, Duration: 4.0},
		{Sequence: 801, Duration: 4.0},
		{Sequence: 802, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 4)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}
	if strings.Contains(out, "#EXT-X-SCTE35") {
		t.Fatalf("VOD-only playlist must not contain any #EXT-X-SCTE35 tag, got:\n%s", out)
	}
}

// TestBuildMediaPlaylistBody_AllFourEventsInOrder: 4 events on
// the right segments come out in the right order: 2 pre-rolls on
// their VOD positions, slice-point before the first ad's EXTINF,
// cue-in before the post-ad VOD's EXTINF (so on the LAST ad's
// segment, not on the post-ad VOD).
func TestBuildMediaPlaylistBody_AllFourEventsInOrder(t *testing.T) {
	segs := []models.SegmentItem{
		// VOD seg with 2 pre-rolls (8s and 4s) on the same segment.
		{
			Sequence: 900, Duration: 10.0,
			SliceInserts: []models.SpliceInsert{
				{
					Type: "out", EventID: 1,
					SpliceTimePTS: 8 * 90000, SpliceTimeSeconds: 8,
					Duration: 30, UniqueProgramID: 1, SegmentationTypeID: 0x30,
				},
				{
					Type: "out", EventID: 1,
					SpliceTimePTS: 4 * 90000, SpliceTimeSeconds: 4,
					Duration: 30, UniqueProgramID: 1, SegmentationTypeID: 0x30,
				},
			},
		},
		// First ad: slice-point.
		{
			Sequence: 901, Duration: 6.0,
			IsAd:            true,
			AdBreakID:       "adbreak-10",
			AdBreakStart:    10,
			AdBreakDuration: 6,
			SliceInserts: []models.SpliceInsert{
				{
					Type: "out", EventID: 1,
					SpliceTimePTS: 10 * 90000, SpliceTimeSeconds: 10,
					Duration: 6, UniqueProgramID: 1, SegmentationTypeID: 0x34,
				},
			},
		},
		// Last ad: cue-in.
		{
			Sequence: 902, Duration: 6.0,
			IsAd:            true,
			AdBreakID:       "adbreak-10",
			AdBreakStart:    10,
			AdBreakDuration: 6,
			SliceInserts: []models.SpliceInsert{
				{
					Type: "in", EventID: 1,
					SpliceTimePTS: 16 * 90000, SpliceTimeSeconds: 16,
					UniqueProgramID: 1, SegmentationTypeID: 0x35,
				},
			},
		},
		// Post-ad VOD.
		{Sequence: 903, Duration: 4.0},
	}
	out, err := buildMediaPlaylistBody(segs, 6)
	if err != nil {
		t.Fatalf("buildMediaPlaylistBody: %v", err)
	}

	// 4 #EXT-X-SCTE35 tags in total.
	if got := strings.Count(out, "#EXT-X-SCTE35:"); got != 4 {
		t.Fatalf("EXT-X-SCTE35 count = %d, want 4:\n%s", got, out)
	}

	// Stronger check: locate the EXTINF for each segment and
	// verify the #EXT-X-SCTE35 tags fall into the right slots
	// (before seg_900, before seg_901, before seg_902, before seg_903).
	extinf900 := strings.Index(out, "seg_900.ts")
	extinf901 := strings.Index(out, "seg_901.ts")
	extinf902 := strings.Index(out, "seg_902.ts")
	extinf903 := strings.Index(out, "seg_903.ts")
	tagCount := strings.Count(out, "#EXT-X-SCTE35:")

	// Count tags before each seg_N.ts marker.
	countBefore := func(end int) int {
		return strings.Count(out[:end], "#EXT-X-SCTE35:")
	}
	if got := countBefore(extinf900); got != 2 {
		t.Fatalf("EXT-X-SCTE35 tags before seg_900.ts = %d, want 2 (8s + 4s pre-rolls):\n%s", got, out)
	}
	if got := countBefore(extinf901); got != 3 {
		t.Fatalf("EXT-X-SCTE35 tags before seg_901.ts = %d, want 3 (2 pre-rolls + slice-point):\n%s", got, out)
	}
	if got := countBefore(extinf902); got != 4 {
		t.Fatalf("EXT-X-SCTE35 tags before seg_902.ts = %d, want 4 (2 pre-rolls + slice-point + cue-in):\n%s", got, out)
	}
	if got := countBefore(extinf903); got != 4 {
		t.Fatalf("EXT-X-SCTE35 tags before seg_903.ts = %d, want 4 (cue-in is on the LAST ad, not the post-ad VOD):\n%s", got, out)
	}
	if tagCount != 4 {
		t.Fatalf("total EXT-X-SCTE35 tags = %d, want 4:\n%s", tagCount, out)
	}
}
