package engine

import (
	"strings"
	"testing"

	"github.com/Comcast/scte35-go/pkg/scte35"
)

// TestBuildSpliceInsert_Out: CUE-OUT produces an OON=true splice_insert
// with SpliceImmediateFlag=true, BreakDuration set, BreakDuration.Duration
// in 90kHz ticks, AutoReturn=true. Round-trip decode must validate the
// library's automatic CRC32.
func TestBuildSpliceInsert_Out(t *testing.T) {
	const durSec = 30.0
	const eventID uint32 = 0x12345678

	b64, err := buildSpliceInsert(eventID, durSec, true)
	if err != nil {
		t.Fatalf("buildSpliceInsert: %v", err)
	}
	if b64 == "" {
		t.Fatalf("empty base64")
	}

	sis, derr := scte35.DecodeBase64(b64)
	if derr != nil {
		t.Fatalf("DecodeBase64: %v", err)
	}

	si, ok := sis.SpliceCommand.(*scte35.SpliceInsert)
	if !ok {
		t.Fatalf("SpliceCommand type = %T, want *scte35.SpliceInsert", sis.SpliceCommand)
	}
	if si.SpliceEventID != eventID {
		t.Fatalf("SpliceEventID = %d, want %d", si.SpliceEventID, eventID)
	}
	if !si.OutOfNetworkIndicator {
		t.Fatalf("OutOfNetworkIndicator = false, want true for CUE-OUT")
	}
	if !si.SpliceImmediateFlag {
		t.Fatalf("SpliceImmediateFlag = false, want true (DATERANGE-bounded)")
	}
	if !si.DurationFlag() {
		t.Fatalf("DurationFlag = false, want true for CUE-OUT with BreakDuration")
	}
	if si.BreakDuration == nil {
		t.Fatalf("BreakDuration = nil, want set for CUE-OUT")
	}
	if !si.BreakDuration.AutoReturn {
		t.Fatalf("BreakDuration.AutoReturn = false, want true")
	}
	if got, want := si.BreakDuration.Duration, uint64(durSec*90000); got != want {
		t.Fatalf("BreakDuration.Duration = %d, want %d (90kHz ticks)", got, want)
	}
	if sis.Tier != 4095 {
		t.Fatalf("Tier = %d, want 4095", sis.Tier)
	}
}

// TestBuildSpliceInsert_In: CUE-IN produces an OON=false splice_insert
// with NO BreakDuration (back to program immediately).
func TestBuildSpliceInsert_In(t *testing.T) {
	const durSec = 30.0 // ignored for CUE-IN
	const eventID uint32 = 0xABCDEF01

	b64, err := buildSpliceInsert(eventID, durSec, false)
	if err != nil {
		t.Fatalf("buildSpliceInsert: %v", err)
	}

	sis, derr := scte35.DecodeBase64(b64)
	if derr != nil {
		t.Fatalf("DecodeBase64: %v", derr)
	}

	si, ok := sis.SpliceCommand.(*scte35.SpliceInsert)
	if !ok {
		t.Fatalf("SpliceCommand type = %T, want *scte35.SpliceInsert", sis.SpliceCommand)
	}
	if si.OutOfNetworkIndicator {
		t.Fatalf("OutOfNetworkIndicator = true, want false for CUE-IN")
	}
	if !si.SpliceImmediateFlag {
		t.Fatalf("SpliceImmediateFlag = false, want true")
	}
	if si.DurationFlag() {
		t.Fatalf("DurationFlag = true, want false (CUE-IN has no BreakDuration)")
	}
	if si.BreakDuration != nil {
		t.Fatalf("BreakDuration = %+v, want nil for CUE-IN", si.BreakDuration)
	}
}

// TestBuildSpliceInsert_Duration: fractional seconds -> 90kHz ticks
// without silent truncation. 12.345 * 90000 = 1111050.
func TestBuildSpliceInsert_Duration(t *testing.T) {
	const durSec = 12.345
	b64, err := buildSpliceInsert(0xDEADBEEF, durSec, true)
	if err != nil {
		t.Fatalf("buildSpliceInsert: %v", err)
	}
	sis, derr := scte35.DecodeBase64(b64)
	if derr != nil {
		t.Fatalf("DecodeBase64: %v", derr)
	}
	si := sis.SpliceCommand.(*scte35.SpliceInsert)
	if si.BreakDuration == nil {
		t.Fatalf("BreakDuration = nil")
	}
	const want = uint64(12.345 * 90000.0) // 1111050
	if si.BreakDuration.Duration != want {
		t.Fatalf("BreakDuration.Duration = %d, want %d", si.BreakDuration.Duration, want)
	}
}

// TestAdBreakEventID_Stable: same input -> same output across calls;
// distinct starts -> distinct IDs. The 0x7FFFFFFF mask must be applied
// (negative breakStart would otherwise produce a huge uint32).
func TestAdBreakEventID_Stable(t *testing.T) {
	a := adBreakEventID(1725014400.0)
	b := adBreakEventID(1725014400.0)
	if a != b {
		t.Fatalf("adBreakEventID not stable: %d vs %d", a, b)
	}
	c := adBreakEventID(1725014401.0)
	if a == c {
		t.Fatalf("distinct start seconds produced same eventID: %d", a)
	}
	// Mask check: adBreakEventID must always be in [0, 0x7FFFFFFF].
	if a&0x80000000 != 0 {
		t.Fatalf("adBreakEventID(1725014400) = %#x, high bit set (mask missing?)", a)
	}
}

// TestDecodeSpliceInsert_BadInput: invalid base64 returns a non-nil
// error (the library still returns a non-nil section for inspection,
// which is fine for callers that want partial state).
func TestDecodeSpliceInsert_BadInput(t *testing.T) {
	_, err := decodeSpliceInsert("not-base64!@#$")
	if err == nil {
		t.Fatalf("expected error on bad base64 input")
	}
}

// TestBuildCueOutLine_Format: lock in the exact DATERANGE line format
// (mirrors tmp/SCTE35Utils.ts generateCUE_OUT). If any field shifts,
// downstream SSAI clients break.
func TestBuildCueOutLine_Format(t *testing.T) {
	// 1725014400 unix = 2024-08-30T10:40:00Z. Picked to be unmistakably
	// non-midnight so an off-by-tz or off-by-second regression in the
	// RFC3339 formatter fails this test.
	const start float64 = 1725014400 // 2024-08-30T10:40:00Z
	const dur = 30.0
	line := buildCueOutLine(start, dur)
	// Mandatory attributes in order, mirroring tmp/SCTE35Utils.ts.
	for _, want := range []string{
		`ID="adbreak-1725014400"`,
		`CLASS="com.apple.hls.avails"`,
		`START-DATE="2024-08-30T10:40:00Z"`,
		`SCTE35-OUT="`,
		`,DURATION=30`,
		`,END-ON-NEXT=YES`,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("CUE-OUT missing %q in:\n%s", want, line)
		}
	}
	// Trailing-unquoted-attr gotcha (threefive TagParser).
	if !strings.HasSuffix(line, "END-ON-NEXT=YES") {
		t.Fatalf("CUE-OUT must end with unquoted END-ON-NEXT=YES (threefive gotcha):\n%s", line)
	}
	// SCTE35-OUT base64 must decode and carry OON=true.
	si, derr := decodeSpliceInsert(extractAttr(line, "SCTE35-OUT"))
	if derr != nil {
		t.Fatalf("SCTE35-OUT decode: %v", derr)
	}
	cmd := si.SpliceCommand.(*scte35.SpliceInsert)
	if !cmd.OutOfNetworkIndicator {
		t.Fatalf("CUE-OUT SCTE35-OutOfNetworkIndicator = false, want true")
	}
}

// TestBuildCueInLine_Format: lock in the CUE-IN DATERANGE line format
// (no CLASS, no END-ON-NEXT; trailing unquoted DURATION).
func TestBuildCueInLine_Format(t *testing.T) {
	const start float64 = 1725014400 // 2024-08-30T10:40:00Z
	const dur = 30.0
	line := buildCueInLine(start, dur)
	for _, want := range []string{
		`ID="adbreak-1725014400"`,
		`START-DATE="2024-08-30T10:40:00Z"`,
		`SCTE35-IN="`,
		`,DURATION=30`,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("CUE-IN missing %q in:\n%s", want, line)
		}
	}
	for _, banned := range []string{`CLASS=`, `END-ON-NEXT`} {
		if strings.Contains(line, banned) {
			t.Fatalf("CUE-IN must not carry %q:\n%s", banned, line)
		}
	}
	// Trailing-unquoted-attr gotcha.
	if !strings.HasSuffix(line, "DURATION=30") {
		t.Fatalf("CUE-IN must end with unquoted DURATION=30 (threefive gotcha):\n%s", line)
	}
	// SCTE35-IN base64 must decode and carry OON=false.
	si, derr := decodeSpliceInsert(extractAttr(line, "SCTE35-IN"))
	if derr != nil {
		t.Fatalf("SCTE35-IN decode: %v", derr)
	}
	cmd := si.SpliceCommand.(*scte35.SpliceInsert)
	if cmd.OutOfNetworkIndicator {
		t.Fatalf("CUE-IN OutOfNetworkIndicator = true, want false")
	}
}

// extractAttr is a tiny helper to pull the value of a DATERANGE
// attribute (e.g. SCTE35-OUT="...") from a built line. Sufficient
// for tests; the player parser handles the real spec.
func extractAttr(line, name string) string {
	needle := name + "=\""
	i := strings.Index(line, needle)
	if i < 0 {
		return ""
	}
	rest := line[i+len(needle):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestFormatBreakDuration_PrecisionCappedAt3 locks the precision of
// the DATERANGE DURATION attribute at 3 decimal places. The reason
// this matters: loadAdSegments derives AdBreakDuration by summing
// pool durations that often come from 90kHz ticks / 90000.0, which
// introduces tiny FP errors (e.g. 6.012 + 6.012 + 6.012 + 1.984 =
// 20.019999999999996 instead of 20.02). Without a precision cap
// the playlist gets an ugly 17-digit tail that confuses SSAI
// clients and log scrapers. 3 decimals matches the #EXTINF
// precision used by buildMediaPlaylistBody and gives millisecond
// resolution — far finer than any sane player needs.
func TestFormatBreakDuration_PrecisionCappedAt3(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{30.0, "30"},                          // trailing zeros stripped
		{12.345, "12.345"},                    // 3 decimals preserved
		{20.02, "20.02"},                      // 2 decimals preserved
		{20.019999999999996, "20.02"},         // FP noise rounded to 3
		{20.019, "20.019"},                    // exact 3-decimal input
		{20.0190001, "20.019"},                // near-3-decimal rounded down
		{20.0199999, "20.02"},                 // near-3-decimal rounded up
		{0.001, "0.001"},                      // smallest non-zero
		{0, "0"},                              // zero
		{1.0 / 3.0, "0.333"},                  // repeating decimal rounded
		{0.1 + 0.2, "0.3"},                    // classic FP error: 0.30000000000000004 -> 0.3
	}
	for _, tc := range cases {
		got := formatBreakDuration(tc.in)
		if got != tc.want {
			t.Fatalf("formatBreakDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFormatBreakDuration_NeverMoreThan3Decimals guards against a
// regression where someone bumps the precision back up to -1. If
// any input produces a 4+ decimal output, the precision cap is
// broken and the playlist will leak FP noise again.
func TestFormatBreakDuration_NeverMoreThan3Decimals(t *testing.T) {
	// A spread of values that historically produced ugly FP tails.
	inputs := []float64{
		20.02,
		20.019999999999996,
		0.1 + 0.2,
		6.012 + 6.012 + 6.012 + 1.984, // 20.01999... per the user-reported case
		1.0 / 7.0,
		12345.6789,
	}
	for _, in := range inputs {
		out := formatBreakDuration(in)
		dot := strings.IndexByte(out, '.')
		if dot < 0 {
			// integer output (e.g. "30") — no decimals, still fine.
			continue
		}
		if len(out)-dot-1 > 3 {
			t.Fatalf("formatBreakDuration(%v) = %q has %d decimals (>3); precision cap broken",
				in, out, len(out)-dot-1)
		}
	}
}
