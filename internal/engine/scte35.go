package engine

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Comcast/scte35-go/pkg/scte35"

	"simlive/internal/models"
)

func buildSpliceInsert(eventID uint32, durSec float64, out bool) (string, error) {
	si := &scte35.SpliceInsert{
		SpliceEventID:              eventID,
		SpliceEventCancelIndicator: false,
		SpliceImmediateFlag:        true,                             // slice point is "now" (DATERANGE-bounded)
		OutOfNetworkIndicator:      out,                              // OON=true => ad break, OON=false => back to program
		Program:                    scte35.NewSpliceInsertProgram(0), // program_splice_flag=true, time_specified_flag=false
	}
	if out && durSec > 0 {
		// AutoReturn=true: when the network splices past the duration,
		// the player returns to network automatically. Duration is in
		// 90kHz ticks (SCTE 35 §9.3.1).
		si.BreakDuration = &scte35.BreakDuration{
			AutoReturn: true,
			Duration:   uint64(durSec * 90000.0),
		}
	}

	sis := scte35.SpliceInfoSection{
		SpliceCommand:   si,
		EncryptedPacket: scte35.EncryptedPacket{EncryptionAlgorithm: scte35.EncryptionAlgorithmNone, CWIndex: 255},
		Tier:            4095,
		SAPType:         scte35.SAPType3,
	}

	b64 := sis.Base64()
	if b64 == "" {
		// sis.Base64() logs an error via scte35.Logger on encode failure
		// and returns ""; surface it to the caller so the engine can fall
		// back to a DATERANGE without the SCTE35-* attribute.
		return "", fmt.Errorf("scte35: Base64() returned empty for eventID=%d", eventID)
	}
	// Sanity-check the round-trip so a CRC32 / encoding bug surfaces
	// here, not silently in a player. DecodeBase64 validates CRC32
	// internally; if it errors, we treat the section as corrupt.
	if _, err := scte35.DecodeBase64(b64); err != nil {
		log.Printf("[Engine] SCTE-35 round-trip decode failed for eventID=%d: %v", eventID, err)
		return "", fmt.Errorf("scte35: round-trip decode failed: %w", err)
	}
	return b64, nil
}

// decodeSpliceInsert parses a base64 splice_info_section back into
// the library's struct. Used by tests and the future verifier hook.
// Returns the same non-nil *SpliceInfoSection the library returns on
// error (so callers can still inspect partial state) along with any
// decode error.
func decodeSpliceInsert(b64 string) (*scte35.SpliceInfoSection, error) {
	return scte35.DecodeBase64(b64)
}

// adBreakEventID derives a stable, positive 31-bit event id from
// the break's start second. Using the break start (not a random or
// monotonic counter) means replans and engine restarts produce the
// same id, so player-side analytics dedup correctly. The mask
// avoids the 0xFFFFFFFF edge case (which the spec reserves for
// "SpliceInsertType = 0x05 with cancellation semantics").
func adBreakEventID(breakStart float64) uint32 {
	return uint32(int64(breakStart) & 0x7FFFFFFF)
}

// --- Media playlist body + SCTE-35 DATERANGE emission ---------------------

// formatBreakStartDate renders the break's start second as an
// RFC3339 UTC string for the DATERANGE START-DATE attribute.
// E.g. 1725014400 -> "2024-08-30T00:00:00Z".
func formatBreakStartDate(breakStart float64) string {
	return time.Unix(int64(breakStart), 0).UTC().Format(time.RFC3339)
}

// formatBreakDuration renders the break's length as a decimal
// seconds string for the DATERANGE DURATION attribute. Precision is
// capped at 3 decimal places so floating-point arithmetic noise
// (e.g. 0.1+0.2-style rounding on 90kHz-derived values) doesn't
// leak into the playlist as ugly 17-digit tails like
// "20.019999999999996". 3 decimals also matches the #EXTINF
// precision used by buildMediaPlaylistBody (fmt %.3f) and gives
// millisecond resolution, which is finer than any sane SSAI
// player needs. After the precision cap, trailing zeros (and a
// lone trailing dot) are stripped so we don't emit "30.000" for
// an exact-second break — keeps the playlist diff-friendly and
// matches the legacy -1-precision behavior. DURATION is
// intentionally emitted unquoted and as the last attribute on
// the line -- see tmp/SCTE35Utils.ts#L51-L59 for the threefive
// TagParser gotcha.
func formatBreakDuration(durSec float64) string {
	// Fast path for special values: NaN / Inf shouldn't reach here
	// in practice (loadAdSegments sums finite Duration floats), but
	// the CUE-OUT/IN path already logs and degrades on bad input,
	// so we just round-trip the Go default to avoid panics.
	if math.IsNaN(durSec) || math.IsInf(durSec, 0) {
		return strconv.FormatFloat(durSec, 'f', 3, 64)
	}
	s := strconv.FormatFloat(durSec, 'f', 3, 64)
	// Strip trailing zeros (and a lone trailing dot) from the
	// fractional part: "30.000" -> "30", "12.340" -> "12.34",
	// "20.020" -> "20.02", "0.001" -> "0.001" (untouched).
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

func buildCueOutLine(seg models.SegmentItem) string {
	id := seg.AdBreakID
	durSec := seg.AdBreakDuration
	start := formatBreakStartDate(seg.AdBreakStart)
	dur := formatBreakDuration(durSec)

	b64, err := buildSpliceInsert(id, durSec, true)
	if err != nil {
		log.Printf("[Engine] CUE-OUT splice_insert encode failed (id=%d): %v -- emitting DATERANGE without SCTE35-OUT", id, err)
		return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%d",CLASS="com.apple.hls.avails",START-DATE="%s",DURATION=%s,END-ON-NEXT=YES`,
			id, start, dur)
	}

	return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%d",CLASS="com.apple.hls.avails",START-DATE="%s",SCTE35-OUT="%s",DURATION=%s,END-ON-NEXT=YES`,
		id, start, b64, dur)
}

// buildCueInLine composes the CUE-IN #EXT-X-DATERANGE line that is
// emitted *before* the first VOD segment after the break. Mirrors
// tmp/SCTE35Utils.ts generateCUE_IN.
//
// ID = same adbreak-<int64(breakStart)> as the matching CUE-OUT
// START-DATE = RFC3339 UTC
// SCTE35-IN = base64 splice_insert (OON=false, SpliceImmediateFlag=true, no BreakDuration)
// DURATION = seconds, UNQUOTED, TRAILING (threefive gotcha)
//
// CUE-IN does not carry CLASS or END-ON-NEXT -- the CUE-OUT's
// END-ON-NEXT=YES tells the player where the break ends.
func buildCueInLine(seg models.SegmentItem) string {
	id := seg.AdBreakID
	start := formatBreakStartDate(seg.AdBreakStart)
	durSec := seg.AdBreakDuration
	dur := formatBreakDuration(durSec)
	b64, err := buildSpliceInsert(id, durSec, false)
	if err != nil {
		log.Printf("[Engine] CUE-IN splice_insert encode failed (id=%d): %v -- emitting DATERANGE without SCTE35-IN", id, err)
		return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%d",START-DATE="%s",DURATION=%s`,
			id, start, dur)
	}
	return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%d",START-DATE="%s",SCTE35-IN="%s",DURATION=%s`,
		id, start, b64, dur)
}

func buildMediaPlaylistBody(segments []models.SegmentItem, targetDuration int, behind ...*models.SegmentItem) (string, error) {
	if len(segments) == 0 {
		return "", fmt.Errorf("buildMediaPlaylistBody: empty segments")
	}

	// var lookBehind *models.SegmentItem
	// if len(behind) > 0 {
	// 	lookBehind = behind[0]
	// }

	header := "#EXTM3U\n"
	header += "#EXT-X-VERSION:3\n"
	header += fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDuration)
	header += "#EXT-X-START:TIME-OFFSET=0\n"
	header += fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n\n", segments[0].Sequence)

	var body strings.Builder
	body.WriteString(header)

	for _, seg := range segments {

		switch {
		case seg.IsAd && seg.AdType == "out":
			body.WriteString(buildCueOutLine(seg))
			body.WriteString("\n")
		case seg.IsAd && seg.AdType == "in":
			// CUE-IN carries the *previous* (last ad) break's
			// metadata, since the CUE-IN is the end of THAT
			// break, not the start of the next VOD segment.
			body.WriteString(buildCueInLine(seg))
			body.WriteString("\n")
		}
		// Per-segment #EXT-X-SCTE35 lines (NEW). The planner
		// stamps 1-2 SpliceInserts on each segment that participates
		// in an ad break (pre-roll on VOD at breakStart-8/-4;
		// slice-point on the first ad; cue-in on the last ad).
		// Emit them BEFORE the segment's #EXTINF, AFTER any
		// DATERANGE the edge detection above already produced, so
		// the DATERANGE remains the lead marker for the VOD<>ad
		// edge and the per-segment inserts follow the segment they
		// are attached to. Order within the slice is preserved
		// (planner writes them in chronological PTS order).
		for _, ev := range seg.SliceInserts {
			body.WriteString(buildSpliceInsertLine(seg, ev))
			body.WriteString("\n")
		}
		body.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration))
		body.WriteString(fmt.Sprintf("seg_%d.ts\n", seg.Sequence))
	}

	return body.String(), nil
}

// buildSpliceInsertLine returns the per-segment #EXT-X-SCTE35 tag
// for one SpliceInsert event stamped on a SegmentItem. The wire
// format is:
//
//	#EXT-X-SCTE35:CUE="<base64>",TYPE=<segType>,ID="<adbreak-id>"
//
// where:
//   - <base64> is the SpliceInfoSection from buildSpliceInsert
//     (same shape as the CUE payload in buildCueOutLine /
//     buildCueInLine: SpliceImmediateFlag=true,
//     OutOfNetworkIndicator=out, BreakDuration if out).
//   - <segType> is the decimal segmentation_type_id (0x30/0x34/0x35)
//     so the player can tell a pre-roll from a slice-point from a
//     cue-in without decoding the base64.
//   - <adbreak-id> is the matching "adbreak-<int64>" string so the
//     player can correlate the per-segment insert with the
//     CUE-OUT/CUE-IN DATERANGE summary tags on the VOD<>ad edges.
//
// ID resolution: prefers seg.AdBreakID (the engine's stable break
// string) so the per-segment insert and the CUE-OUT/CUE-IN agree
// byte-for-byte on the same break. Falls back to
// "adbreak-<int64(SpliceTimeSeconds)>" only if the segment carries
// no AdBreakID (defensive -- shouldn't happen in practice because
// the planner stamps both fields together in insertAdsIfNeeded).
//
// On buildSpliceInsert error the CUE= attribute is dropped and the
// line is emitted with only TYPE+ID, so the rest of the playlist
// survives. Mirrors the failure handling of buildCueOutLine
// (log + degrade, never panic).
//
// See docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
// §3.2.1 for the design rationale.
func buildSpliceInsertLine(seg models.SegmentItem, ev models.SpliceInsert) string {
	id := seg.AdBreakID
	out := ev.Type == "out"
	b64, err := buildSpliceInsert(uint32(ev.EventID), ev.Duration, out)
	if err != nil {
		log.Printf("[Engine] per-segment SCTE-35 encode failed (id=%d, type=0x%x): %v -- emitting #EXT-X-SCTE35 without CUE",
			id, ev.SegmentationTypeID, err)
		return fmt.Sprintf(`#EXT-X-SCTE35:TYPE=%d,ID="%d"`, ev.SegmentationTypeID, id)
	}
	return fmt.Sprintf(`#EXT-X-SCTE35:CUE="%s",TYPE=%d,ID="%d"`, b64, ev.SegmentationTypeID, id)
}
