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
	s := SegmentItem{Sequence: 42, StoragePath: "x", Duration: 6.0, Rendition: "720p"}
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
	if out.Rendition != "720p" {
		t.Fatalf("Rendition = %q, want 720p", out.Rendition)
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
	if s.Rendition != "" {
		t.Fatalf("legacy member: Rendition = %q, want empty", s.Rendition)
	}
	if s.Sequence != 7 {
		t.Fatalf("Sequence = %d, want 7", s.Sequence)
	}
}

// TestSegmentItem_Rendition_MultiRung asserts that two SegmentItems with
// different renditions marshal+unmarshal independently. This is the
// invariant that lets the engine dispatch by tag without losing members.
func TestSegmentItem_Rendition_MultiRung(t *testing.T) {
	a := SegmentItem{Sequence: 1, Rendition: "360p"}
	b := SegmentItem{Sequence: 2, Rendition: "720p"}

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
	if ar.Rendition != "360p" || br.Rendition != "720p" {
		t.Fatalf("roundtrip lost rendition: a=%q b=%q", ar.Rendition, br.Rendition)
	}
}
