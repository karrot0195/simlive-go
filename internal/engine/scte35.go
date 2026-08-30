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

// SCTE-35 builder / decoder helpers used by the engine to emit
// #EXT-X-DATERANGE tags with splice_insert payloads on VOD<->ad
// transitions. Wraps the Comcast scte35-go library
// (github.com/Comcast/scte35-go/pkg/scte35, Apache-2.0) and exposes a
// small, testable surface.
//
// See docs/300826-implement-scte35-cue-markers.md for the design and
// tmp/SCTE35Utils.ts for the tag string format this mirrors.

// buildSpliceInsert returns the base64 splice_info_section for a
// single splice_insert cue. out=true produces a CUE-OUT (OON=true,
// BreakDuration set); out=false produces a CUE-IN (OON=false, no
// BreakDuration so the player returns to network immediately).
//
// durSec is the break length in seconds and is only meaningful for
// CUE-OUT; for CUE-IN it is accepted for API symmetry but ignored
// (the library elides BreakDuration when duration_flag is unset).
// durSec is converted to 90kHz ticks for the BreakDuration.Duration
// field per ANSI/SCTE 35.
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

// buildCueOutLine composes the CUE-OUT #EXT-X-DATERANGE line that is
// emitted *before* the first ad segment's #EXTINF. Mirrors
// tmp/SCTE35Utils.ts generateCUE_OUT.
//
// ID = adbreak-<int64(breakStart)>
// CLASS = "com.apple.hls.avails"  (Apple HLS Interstitials spec)
// START-DATE = RFC3339 UTC
// SCTE35-OUT = base64 splice_insert (OON=true, SpliceImmediateFlag=true,
//
//	BreakDuration.AutoReturn=true, BreakDuration=uint64(durSec*90000))
//
// DURATION = seconds, UNQUOTED, TRAILING (threefive gotcha)
// END-ON-NEXT = YES  (the break ends at the next CUE-IN)
//
// On buildSpliceInsert error the SCTE35-OUT attribute is omitted so
// the rest of the playlist (and the DURATION/END-ON-NEXT metadata
// the player still needs) survives; an error is logged.
func buildCueOutLine(breakStart, durSec float64) string {
	id := fmt.Sprintf("adbreak-%d", int64(breakStart))
	start := formatBreakStartDate(breakStart)
	dur := formatBreakDuration(durSec)
	b64, err := buildSpliceInsert(adBreakEventID(breakStart), durSec, true)
	if err != nil {
		log.Printf("[Engine] CUE-OUT splice_insert encode failed (id=%s): %v -- emitting DATERANGE without SCTE35-OUT", id, err)
		return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%s",CLASS="com.apple.hls.avails",START-DATE="%s",DURATION=%s,END-ON-NEXT=YES`,
			id, start, dur)
	}
	return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%s",CLASS="com.apple.hls.avails",START-DATE="%s",SCTE35-OUT="%s",DURATION=%s,END-ON-NEXT=YES`,
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
func buildCueInLine(breakStart, durSec float64) string {
	id := fmt.Sprintf("adbreak-%d", int64(breakStart))
	start := formatBreakStartDate(breakStart)
	dur := formatBreakDuration(durSec)
	b64, err := buildSpliceInsert(adBreakEventID(breakStart), durSec, false)
	if err != nil {
		log.Printf("[Engine] CUE-IN splice_insert encode failed (id=%s): %v -- emitting DATERANGE without SCTE35-IN", id, err)
		return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%s",START-DATE="%s",DURATION=%s`,
			id, start, dur)
	}
	return fmt.Sprintf(`#EXT-X-DATERANGE:ID="%s",START-DATE="%s",SCTE35-IN="%s",DURATION=%s`,
		id, start, b64, dur)
}

// buildMediaPlaylistBody renders the per-rung media playlist body.
// Pure: same input -> same output. Same purity contract as
// buildMasterPlaylist (rendition.go#L86-L105), so it can be
// unit-tested without Redis / the disk cache.
//
// segments must be sorted by Sequence ascending (the caller in
// generateHLSPlaylist already sorts and filters to the .pts-prepared
// subset, so we don't re-filter here). The DATERANGE detection runs
// on these prepared segments so a CUE-OUT never references a
// segment not in the playlist.
//
// Tag format mirrors tmp/SCTE35Utils.ts:
//   - CUE-OUT (VOD -> ad) emitted BEFORE the first ad EXTINF.
//   - CUE-IN  (ad -> VOD) emitted BEFORE the first VOD EXTINF after
//     the break, reusing the break's id, start, and duration.
//   - DURATION is always UNQUOTED and TRAILING (threefive TagParser
//     parses a line as quoted-vs-unquoted by its last char; trailing
//     unquoted is what works -- see tmp/SCTE35Utils.ts#L51-L59).
func buildMediaPlaylistBody(segments []models.SegmentItem, targetDuration int) (string, error) {
	if len(segments) == 0 {
		return "", fmt.Errorf("buildMediaPlaylistBody: empty segments")
	}

	header := "#EXTM3U\n"
	header += "#EXT-X-VERSION:3\n"
	header += fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDuration)
	header += "#EXT-X-START:TIME-OFFSET=0\n"
	header += fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n\n", segments[0].Sequence)

	var body strings.Builder
	body.WriteString(header)

	for i, seg := range segments {
		// Edge detection. Use the previous segment's IsAd, not the
		// current's, because we emit the marker BEFORE the segment
		// that starts the new state (first ad => CUE-OUT, first VOD
		// after ads => CUE-IN). The very first segment (i==0) has
		// no previous; if it is an ad we still emit CUE-OUT so the
		// player sees the break boundary on a cold start.
		if i == 0 {
			if seg.IsAd {
				body.WriteString(buildCueOutLine(seg.AdBreakStart, seg.AdBreakDuration))
				body.WriteString("\n")
			}
		} else {
			prev := segments[i-1]
			switch {
			case !prev.IsAd && seg.IsAd:
				body.WriteString(buildCueOutLine(seg.AdBreakStart, seg.AdBreakDuration))
				body.WriteString("\n")
			case prev.IsAd && !seg.IsAd:
				// CUE-IN carries the *previous* (last ad) break's
				// metadata, since the CUE-IN is the end of THAT
				// break, not the start of the next VOD segment.
				body.WriteString(buildCueInLine(prev.AdBreakStart, prev.AdBreakDuration))
				body.WriteString("\n")
			}
		}
		body.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration))
		body.WriteString(fmt.Sprintf("seg_%d.ts\n", seg.Sequence))
	}

	return body.String(), nil
}
