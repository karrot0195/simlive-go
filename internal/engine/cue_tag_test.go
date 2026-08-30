package engine

import (
	"encoding/json"
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
