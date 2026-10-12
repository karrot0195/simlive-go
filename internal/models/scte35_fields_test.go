package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSegmentItem_AdFields_OmitWhenEmpty: a VOD SegmentItem
// (IsAd=false, no break fields) must NOT emit any of the new
// SCTE-35 keys in JSON, so legacy pre-M-SCTE35 members round-trip
// unchanged and the engine's edge detection stays valid (zero
// IsAd on a VOD == false).
func TestSegmentItem_AdFields_OmitWhenEmpty(t *testing.T) {
	s := SegmentItem{Sequence: 1, StoragePath: "x", Duration: 4}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{`"is_ad"`, `"ad_break_id"`, `"ad_break_start"`, `"ad_break_duration"`} {
		if strings.Contains(string(b), banned) {
			t.Fatalf("expected JSON to omit %s when empty, got %s", banned, b)
		}
	}
}

// TestSegmentItem_AdFields_RoundTrip: an ad SegmentItem with all
// four new fields set must round-trip through JSON preserving
// every field. This is the contract the engine relies on when it
// reads the timeline back from Redis and walks the VOD<>ad edges.
func TestSegmentItem_AdFields_RoundTrip(t *testing.T) {
	src := SegmentItem{
		Sequence:        42,
		StoragePath:     "espx-video-lib:espx-video-lib/ads-v3/L2/seg_42.ts",
		Duration:        6.0,
		IsAd:            true,
		AdBreakID:       1,
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
	var out SegmentItem
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.IsAd {
		t.Fatalf("IsAd = false, want true")
	}
	if out.AdBreakID != src.AdBreakID {
		t.Fatalf("AdBreakID = %d, want %q", out.AdBreakID, src.AdBreakID)
	}
	if out.AdBreakStart != src.AdBreakStart {
		t.Fatalf("AdBreakStart = %v, want %v", out.AdBreakStart, src.AdBreakStart)
	}
	if out.AdBreakDuration != src.AdBreakDuration {
		t.Fatalf("AdBreakDuration = %v, want %v", out.AdBreakDuration, src.AdBreakDuration)
	}
}

// TestSegmentItem_LegacyUnmarshal_NoAdFields: JSON written before
// the SCTE-35 work (no ad_* keys) must decode cleanly with all
// four ad fields at their zero values, so old timeline members
// don't get accidentally treated as ads.
func TestSegmentItem_LegacyUnmarshal_NoAdFields(t *testing.T) {
	raw := []byte(`{"sequence":7,"storage_path":"x","duration":4,"media_start":100,"media_end":104}`)
	var s SegmentItem
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.IsAd {
		t.Fatalf("legacy member: IsAd = true, want false")
	}
	if s.AdBreakID != 0 || s.AdBreakStart != 0 || s.AdBreakDuration != 0 {
		t.Fatalf("legacy member: ad fields should be zero-valued, got %+v", s)
	}
}
