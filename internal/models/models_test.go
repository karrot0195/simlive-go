package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSegmentItem_Rendition_OmitsWhenEmpty asserts the contract that the
// engine relies on: a SegmentItem with Rendition="" must NOT emit a
// "rendition" key in its JSON form, so legacy pre-M1 members round-trip
// unchanged and the engine's DefaultRendition rescue path stays valid.
func TestSegmentItem_Rendition_OmitsWhenEmpty(t *testing.T) {
	s := SegmentItem{Sequence: 1, StoragePath: "x", Duration: 4}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"rendition"`) {
		t.Fatalf("expected JSON to omit rendition when empty, got %s", b)
	}
}

// TestSegmentItem_Rendition_Roundtrip asserts that the Rendition field
// survives a JSON round-trip when set. This is the inverse of the omitempty
// contract: explicit values must be preserved.
func TestSegmentItem_Rendition_Roundtrip(t *testing.T) {
	s := SegmentItem{Sequence: 42, StoragePath: "x", Duration: 6.0, Renditions: []string{"720p"}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"rendition":"720p"`) {
		t.Fatalf("expected JSON to contain rendition=720p, got %s", b)
	}
	var out SegmentItem
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Renditions[0] != "720p" {
		t.Fatalf("Rendition = %q, want 720p", out.Renditions)
	}
}

// TestSegmentItem_LegacyUnmarshal_NoRenditionField asserts that JSON written
// before M1 (no "rendition" key) decodes to Rendition="" -- the engine's
// default-rescue path then maps it to DefaultRendition at dispatch time.
func TestSegmentItem_LegacyUnmarshal_NoRenditionField(t *testing.T) {
	raw := []byte(`{"sequence":7,"storage_path":"x","duration":4,"media_start":100,"media_end":104}`)
	var s SegmentItem
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(s.Renditions) == 0 {
		t.Fatalf("legacy member: Rendition = %q, want empty", s.Renditions)
	}
	if s.Sequence != 7 {
		t.Fatalf("Sequence = %d, want 7", s.Sequence)
	}
}

// TestSegmentItem_Rendition_MultiRung asserts that two SegmentItems with
// different renditions marshal+unmarshal independently. This is the
// invariant that lets the engine dispatch by tag without losing members.
func TestSegmentItem_Rendition_MultiRung(t *testing.T) {
	a := SegmentItem{Sequence: 1, Renditions: []string{"360p"}}
	b := SegmentItem{Sequence: 2, Renditions: []string{"720p"}}

	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !strings.Contains(string(ab), `"rendition":"360p"`) {
		t.Fatalf("a missing rendition tag: %s", ab)
	}
	if !strings.Contains(string(bb), `"rendition":"720p"`) {
		t.Fatalf("b missing rendition tag: %s", bb)
	}

	var ar, br SegmentItem
	_ = json.Unmarshal(ab, &ar)
	_ = json.Unmarshal(bb, &br)
	if ar.Renditions[0] != "360p" || br.Renditions[0] != "720p" {
		t.Fatalf("roundtrip lost rendition: a=%q b=%q", ar.Renditions, br.Renditions)
	}
}

// --- SliceInserts (per-segment splice events) -----------------------------
//
// Added for the per-segment pre-roll + slice-point + cue-in stamping
// introduced in docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md.
// The field is omitempty so VOD-only members (the >95% case) and
// pre-feature timeline members round-trip to JSON unchanged.

// TestSegmentItem_SliceInserts_OmitWhenEmpty: a VOD SegmentItem
// (SliceInserts nil) must NOT emit a "splice_inserts" key in its
// JSON form, so legacy pre-feature members round-trip unchanged
// and the engine's pre-roll emission path stays a no-op for them.
func TestSegmentItem_SliceInserts_OmitWhenEmpty(t *testing.T) {
	s := SegmentItem{Sequence: 1, StoragePath: "x", Duration: 4}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"splice_inserts"`) {
		t.Fatalf("expected JSON to omit splice_inserts when empty, got %s", b)
	}
}

// TestSegmentItem_SliceInserts_RoundTrip: a SegmentItem with a
// populated SliceInserts slice must round-trip through JSON
// preserving every field on every entry. This is the contract
// the engine relies on when it reads the timeline back from
// Redis and emits #EXT-X-SCTE35 lines in the media playlist.
func TestSegmentItem_SliceInserts_RoundTrip(t *testing.T) {
	src := SegmentItem{
		Sequence:    42,
		StoragePath: "x",
		Duration:    6.0,
		SliceInserts: []SpliceInsert{
			{
				Type:               "out",
				EventID:            1725014400,
				SpliceTimePTS:      1725014392 * 90000, // pre-roll at breakStart-8
				SpliceTimeSeconds:  1725014392,
				Duration:           30.0,
				UniqueProgramID:    1725014400,
				SegmentationTypeID: SegTypePlacementOpportunityStart,
			},
			{
				Type:               "out",
				EventID:            1725014400,
				SpliceTimePTS:      1725014396 * 90000, // pre-roll at breakStart-4
				SpliceTimeSeconds:  1725014396,
				Duration:           30.0,
				UniqueProgramID:    1725014400,
				SegmentationTypeID: SegTypePlacementOpportunityStart,
			},
		},
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"splice_inserts":[`,
		`"type":"out"`,
		`"eventId":1725014400`,
		`"spliceTimePts":155251295280000`, // (1725014392 * 90000) = 155251295280000 ticks
		`"spliceTimeSeconds":1725014392`,
		`"duration":30`,
		`"uniqueProgramId":1725014400`,
		`"segmentationTypeId":48`, // 0x30 == 48
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("marshal missing %q: %s", want, b)
		}
	}
	var out SegmentItem
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.SliceInserts) != len(src.SliceInserts) {
		t.Fatalf("len(SliceInserts) = %d, want %d", len(out.SliceInserts), len(src.SliceInserts))
	}
	for i, ev := range src.SliceInserts {
		got := out.SliceInserts[i]
		if got != ev {
			t.Fatalf("entry[%d] = %+v, want %+v", i, got, ev)
		}
	}
}

// TestSegmentItem_SliceInserts_LegacyRescue: JSON written before
// the SliceInserts feature (no splice_inserts key) must decode
// cleanly to a nil slice, so pre-feature timeline members don't
// get accidentally treated as carrying splice events.
func TestSegmentItem_SliceInserts_LegacyRescue(t *testing.T) {
	raw := []byte(`{"sequence":7,"storage_path":"x","duration":4,"media_start":100,"media_end":104}`)
	var s SegmentItem
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.SliceInserts != nil {
		t.Fatalf("legacy member: SliceInserts = %+v, want nil", s.SliceInserts)
	}
}
