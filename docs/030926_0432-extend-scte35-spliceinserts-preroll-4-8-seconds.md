# SCTE-35 SliceInserts: CUE-OUT + 8s/4s PreRoll + CUE-IN

**Plan version:** 0.1
**Date:** 2026-09-03
**Author:** (agent, via simlive-plan skill)
**Status:** Approved — implementing

> **Goal:** Extend the planner's `insertAdsIfNeeded` to stamp **four
> distinct splice events per ad break** on the correct segments —
> mirroring the TypeScript reference in
> `tmp/PrepareSimulatedLive.ts markSpliceTargets` — and extend the
> engine's `buildMediaPlaylistBody` to emit per-segment `#EXT-X-SCTE35`
> lines in the per-rung media playlist. The existing
> `#EXT-X-DATERANGE` CUE-OUT / CUE-IN summary tags **stay** — the
> per-segment splice inserts are additive.
>
> **Why now:** `models.SpliceInsert` (lines 54–63) and
> `SegmentItem.SliceInserts []SpliceInsert` (line 123) already exist
> but nothing populates them; SSAI players rely on the pre-roll
> window (8s/4s warning) to schedule creatives and tear down
> analytics sessions *before* the actual cue point.

---

## 0. Progress

- [x] Read pre-flight context (`docs/specification.md`,
      `.github/instructions/simlive-summary.instructions.md`,
      `300826_0616-implement-scte35-cue-markers.md`,
      `tmp/inject-scte35_splice_information-v2-plan.md`,
      `tmp/PrepareSimulatedLive.ts` markSpliceTargets,
      `internal/planner/{planner,segment_processor}.go`,
      `internal/engine/scte35.go`,
      `internal/models/models.go`,
      existing tests in `internal/{models,planner,engine}/`).
- [x] Phase A — pure helpers + tests.
- [x] Phase B — stamp the four events in `insertAdsIfNeeded`.
- [x] Phase C — emit `#EXT-X-SCTE35` lines in `buildMediaPlaylistBody`.
- [x] `go build ./...` + `go vet ./...` + `go test ./internal/...`.
- [x] Post-task doc pass:
      `.github/instructions/simlive-summary.instructions.md`,
      `docs/specification.md` §6.1a,
      `docs/memories.md`,
      `/memories/repo/simlive-go.md`.
- [x] **Revision 2026-09-03 (post-implementation)**: moved
      `isBlockingProcessing` and `processingSegments` from the
      per-call `SegmentProcessor` to the shared `Planner` (so the
      pre-roll lookup in `insertAdsIfNeeded` and the append in
      `addSegment` see the same buffer without a copy; the
      per-channel in-flight guard in `cmd/planner/main.go`
      `runPlannerTick` already serializes SAME-channel calls so
      the shared ownership is safe). Added `PTS_DURATION` constant
      (= 3753.75 ticks) so `MediaEnd = (ptsEnd + PTS_DURATION) /
      90000` gives a non-overlapping `[MediaStart, MediaEnd)`
      interval (the previous `MediaEnd = ptsEnd / 90000` collapsed
      adjacent segments by 1 frame). Tightened
      `findSegmentContainingPts` to handle FP-rounding noise on the
      right edge of segments (a pre-roll pts just past one
      segment's MediaEnd, before the next segment's MediaStart, now
      binds to the previous segment). Added
      `TestFindSegmentContainingPts_FPRoundingTolerance` to lock
      the new behavior.

---

## 1. Problem Statement

Today the planner stamps `IsAd/AdBreakID/AdBreakStart/AdBreakDuration`
on ad segments and the engine emits a single `#EXT-X-DATERANGE` line
per VOD↔ad edge (`buildMediaPlaylistBody` in
`internal/engine/scte35.go`). The TypeScript reference
(`tmp/PrepareSimulatedLive.ts markSpliceTargets`) emits **four
distinct splice events** per ad break — **8s pre-roll (0x30), 4s
pre-roll (0x30), slice-point/cue-out (0x34), cue-in (0x35)** — each
attached to a *specific* `Segment` based on a PTS lookup. The
`models.SpliceInsert` struct and `SegmentItem.SliceInserts []SpliceInsert`
field already exist (lines 54–63 and 123 of `models.go`) but no
code populates them. SSAI players rely on the pre-roll window
(8s/4s warning) to pre-fetch creatives, schedule opportunities, and
tear down analytics sessions *before* the actual cue point —
without it the player only sees the ad break at the slice point and
the first `8s` of the ad has no opportunity metadata.

The Go `SpliceInsert` model is already shaped exactly like the TS
`SpliceEvent` (the two diverged only in the segmentation_type_id
constant, which we'll add as planner-side constants). The
`Comcast/scte35-go` dep is already in `go.mod`. The work is to:
(1) stamp the four events on the right segments during
`insertAdsIfNeeded`, and (2) emit per-segment `#EXT-X-SCTE35` lines
in the engine's media playlist.

---

## 2. Solution Overview

Three changes, layered so each step is testable in isolation:

1. **Pure helpers** — `findSegmentContainingPts`,
   `appendSpliceInsertUnique`, `adBreakEventIDFor`, and the four
   `SegmentationTypeID` constants. No I/O; lock the contract with
   pure unit tests.
2. **Planner stamping** — extend `insertAdsIfNeeded` to:
   - Walk `PreRollDelaysSec` (8s, 4s), look up the VOD target
     via `findSegmentContainingPts`, append a `0x30` pre-roll event
     (with `SpliceTimePTS = (breakStart - delay) * 90000`,
     `SpliceTimeSeconds = breakStart - delay`,
     `Duration = breakDuration`).
   - Append the **slice-point** event (`0x34`,
     `SpliceTimePTS = breakStart * 90000`) to the **first ad**.
   - Append the **cue-in** event (`0x35`,
     `SpliceTimePTS = breakEnd * 90000`) to the **last ad**.
   - All four events share the same `EventID` =
     `uint32(int64(breakStart) & 0x7FFFFFFF)` and the same `Duration`
     = `totalBreakDuration(adSegments)`.
3. **Engine emission** — extend `buildMediaPlaylistBody` to walk
   `seg.SliceInserts` and emit one `#EXT-X-SCTE35:CUE="<base64>",TYPE=<segType>,ID="<adbreak-id>"`
   line **before** the segment's `#EXTINF` (and **after** any
   `EXT-X-DATERANGE` the existing edge-detection already produced).
   The `CUE=` payload reuses the existing
   `engine.buildSpliceInsert(eventID, durSec, out)` helper.

**No change to** sequence numbering, PTS offset anchoring,
ad-pool trimming, `lastAdTime` advance, the existing
`EXT-X-DATERANGE` summary tags, or the wire format of the
`SpliceInfoSection` (CRC32 / 90kHz ticks / OON bit).

---

## 3. What Changed (by file)

### 3.1 `internal/planner/segment_processor.go` (modified)

**3.1.1 — Add segmentation-type constants** (next to the existing
`WindowItemSize = 50` etc. block at the top):

```go
// SCTE-35 segmentation_type_id values per ANSI/SCTE 35 §10.2.3
// (seg_type_id field inside the segmentation_descriptor). Mirrors
// tmp/PrepareSimulatedLive.ts `SEG_TYPE_*` constants. Used by the
// planner to stamp SliceInserts; the engine reads them off the
// model and translates to a `<TYPE=...>` attribute on the emitted
// tag.
const (
    SegTypePlacementOpportunityStart = 0x30 // pre-roll (8s/4s warning)
    SegTypeAdvertisementStart       = 0x34 // slice point / cue-out
    SegTypeAdvertisementEnd         = 0x35 // cue-in
)

// PreRollDelaysSec lists the seconds-before-ad-break offsets the
// planner attaches a 0x30 pre-roll splice insert to. Order is
// emitted in order (longest delay first) so the playlist carries
// the events in chronological order. Mirrors tmp/PRE_ROLL_DELAYS.
var PreRollDelaysSec = []float64{8, 4}

// PTS_DURATION is the 90kHz tick duration of one video frame at
// 24000/1001 fps (≈23.976 — the NTSC film rate used by most HLS
// VOD sources). The planner adds it to ptsEnd to derive the
// start of the NEXT frame (instead of the same frame's end
// boundary), so MediaEnd = (ptsEnd + PTS_DURATION) / 90000
// gives a non-overlapping [MediaStart, MediaEnd) interval that
// plays cleanly through the engine's PTS/DTS rewrite.
//
// Value: (1001/24000) * 90000 = 3753.75 ticks/frame.
const PTS_DURATION = (1001.0 / 24000.0) * 90000.0
```

The `PTS_DURATION` constant is **separate from the SCTE-35 work** —
it's a VOD MediaEnd calculation that happens to live next to the
splice-insert constants because both are in the
`processStreamSegments` PTS math. `PTS_DURATION` is used at
`segment_processor.go` in the `adjustedPtsEnd = float64(ptsEnd) +
PTS_DURATION + sp.currentPtsOffset` line. Without it, the
interval `[MediaStart, MediaEnd)` collapses to a single-frame
width (the player sees each segment end at the same PTS it
starts, which produces a black frame + audio glitch on every
segment boundary).

**3.1.2 — Add pure helpers** at the bottom of the file (next to
`adBreakIDFor` and `totalBreakDuration`):

```go
// findSegmentContainingPts returns the index of the segment in
// `segs` whose [MediaStart, MediaEnd) interval contains `ptsSec`
// (seconds on the channel's local timeline). Returns (-1, false)
// when no segment covers the point.
//
// Mirrors tmp/PrepareSimulatedLive.ts `findSegmentContainingPts`.
// Inclusive on the left edge (pts == MediaStart counts as
// "inside"), exclusive on the right edge (pts == MediaEnd counts
// as "after this segment"). On ties (FP rounding noise on the
// right edge of two adjacent intervals) the FIRST matching
// segment wins -- the TS reference's behavior is also "first
// match" via Array.find, so we stay byte-for-byte compatible.
//
// FP-rounding tolerance: when pts is slightly PAST the right edge
// of segment i due to FP noise (e.g. 90kHz-derived MediaEnd) but
// still before segment i+1 starts, the helper binds the point to
// segment i. Example: MediaEnd of a 6.012s segment is
// 540000 / 90000.0 = 6.011999... A pre-roll lookup at pts=6.012
// would miss the strict right-exclusive test, so the fallback
// binds it to the previous segment when the next one starts at
// 7.0+. This matches the way TS `findSegmentContainingPts` handles
// the same rounding noise -- without it, a pre-roll whose PTS
// lands just past one segment's MediaEnd but before the next
// segment's MediaStart would silently skip (WARN log) instead of
// landing on the right segment.
//
// Pure, no I/O. Safe to call against sp.planner.processingSegments
// (slice of values, not pointers) inside the for-loop body.
func findSegmentContainingPts(segs []models.SegmentItem, ptsSec float64) (int, bool) {
    if len(segs) == 0 { return -1, false }
    for i, s := range segs {
        if ptsSec >= s.MediaStart && (ptsSec < s.MediaEnd || i+1 < len(segs) && ptsSec < segs[i+1].MediaStart) {
            return i, true
        }
    }
    return -1, false
}

// appendSpliceInsertUnique appends `ev` to target's SliceInserts
// UNLESS a SpliceInsert with the same (Type, SpliceTimePTS) is
// already there. Mirrors tmp/PrepareSimulatedLive.ts
// `pushSpliceEvent` (the dedup-by-(type,spliceTimePts) rule).
func appendSpliceInsertUnique(target *models.SegmentItem, ev models.SpliceInsert) { ... }

// adBreakEventIDFor returns the SCTE-35 SpliceEventID (uint32) for
// a given break-start second. The 0x7FFFFFFF mask avoids the
// 0xFFFFFFFF reserved value and the high-bit ambiguity for negative
// inputs. MUST match the engine's `adBreakEventID` byte-for-byte
// so the planner's pre-roll event IDs and the engine's CUE-OUT
// / CUE-IN IDs agree on the same break.
func adBreakEventIDFor(breakStart float64) uint32 {
    return uint32(int64(breakStart) & 0x7FFFFFFF)
}
```

**3.1.3 — Extend `insertAdsIfNeeded`** (modifies the existing
ad-stamping block; no change to the existing logic, only appends
new stamping after the ad loop and walks `PreRollDelaysSec` before
the loop):

The full new structure of `insertAdsIfNeeded` (see §3.1.3.1 for
placement rules):

```go
func (sp *SegmentProcessor) insertAdsIfNeeded(actualStartTime float64) error {
    if len(sp.adSegments) == 0 { return nil }
    if actualStartTime < sp.nowUnix || (actualStartTime-sp.lastAdTime) < float64(AdInsertionInterval) {
        return nil
    }

    // === EXISTING: break metadata captured before the ad loop ===
    breakStart := sp.runningTime
    breakDuration := totalBreakDuration(sp.adSegments)
    breakID := adBreakIDFor(breakStart)
    breakEventID := adBreakEventIDFor(breakStart)

    // === NEW: stamp the pre-roll (0x30) events on the VOD
    // segments at breakStart-8 and breakStart-4. We do this BEFORE
    // inserting any ad segment so the pre-roll targets are still
    // in sp.processingSegments (and their MediaEnd hasn't been
    // advanced by ad insertion). ===
    for _, delay := range PreRollDelaysSec {
        preRollPts := breakStart - delay
        if preRollPts < 0 {
            // First ad of the channel — no preceding VOD.
            // Logged once per (channel, breakStart, delay).
            continue
        }
        idx, ok := findSegmentContainingPts(sp.processingSegments, preRollPts)
        if !ok {
            // Pre-roll target not yet scheduled (the walk is
            // racing the segments). Defensive skip — TS
            // reference does the same.
            continue
        }
        ev := models.SpliceInsert{
            Type:               "out",
            EventID:            int64(breakEventID),
            SpliceTimePTS:      int64(preRollPts * 90000.0),
            SpliceTimeSeconds:  preRollPts,
            Duration:           breakDuration,
            UniqueProgramID:    int64(breakEventID),
            SegmentationTypeID: SegTypePlacementOpportunityStart,
        }
        appendSpliceInsertUnique(&sp.planner.processingSegments[idx], ev)
    }

    // === EXISTING: ad loop (unchanged logic, only adds slice-point
    // and cue-in stamping on first/last ad respectively) ===
    var firstAd *models.SegmentItem
    var lastAd *models.SegmentItem
    for _, adSeg := range sp.adSegments {
        if isEndOfTimeline(sp.horizon, sp.runningTime) { return ErrEndOfTimeline }
        sp.sequenceCounter++
        adItem := adSeg
        adItem.Sequence = sp.sequenceCounter
        adItem.Rendition = sp.currentRendition
        adItem.MediaStart = sp.runningTime
        adItem.MediaEnd = sp.runningTime + adItem.Duration
        adItem.StartTime = int64(adItem.MediaStart)
        ptsOffset := sp.setTimelineBase(adItem.MediaStart)
        adItem.PtsOffset = &ptsOffset
        adItem.IsAd = true
        adItem.AdBreakID = breakID
        adItem.AdBreakStart = breakStart
        adItem.AdBreakDuration = breakDuration
        if firstAd == nil { firstAd = &adItem }
        lastAd = &adItem
        // ... addSegment + advance runningTime unchanged ...
    }

    // === NEW: stamp the slice-point (0x34) on the first ad and
    // the cue-in (0x35) on the last ad. breakEnd is the second
    // immediately after the last ad finishes (== sp.runningTime
    // after the loop). ===
    breakEnd := sp.runningTime
    if firstAd != nil {
        appendSpliceInsertUnique(firstAd, models.SpliceInsert{
            Type: "out", EventID: int64(breakEventID),
            SpliceTimePTS: int64(breakStart * 90000.0),
            SpliceTimeSeconds: breakStart,
            Duration: breakDuration,
            UniqueProgramID: int64(breakEventID),
            SegmentationTypeID: SegTypeAdvertisementStart,
        })
    }
    if lastAd != nil {
        appendSpliceInsertUnique(lastAd, models.SpliceInsert{
            Type: "in", EventID: int64(breakEventID),
            SpliceTimePTS: int64(breakEnd * 90000.0),
            SpliceTimeSeconds: breakEnd,
            UniqueProgramID: int64(breakEventID),
            SegmentationTypeID: SegTypeAdvertisementEnd,
        })
    }

    sp.lastAdTime = sp.runningTime
    return nil
}
```

**3.1.3.1 — Placement rule for the pre-roll loop:** runs **before**
the ad for-loop so `sp.processingSegments` is still the
pre-ad-insert VOD buffer. The pre-roll events attach to VOD
segments; after the ad loop, `sp.processingSegments` would still
contain the same VOD members (the VOD members are not removed by
ad insertion — only the `runningTime` advances), but writing the
events on the *correct* indices is more obvious before any state
mutates.

**3.1.3.2 — Placement rule for the slice-point / cue-in events:**
runs **after** the ad for-loop because the first and last ad
`SegmentItem` values only exist after the loop has built them
(plus we capture `breakEnd = sp.runningTime` which only equals
`breakStart + breakDuration` after the loop).

**3.1.3.3 — `adItem` local copy:** the existing for-loop already
copies the ad from `sp.adSegments` into a local `adItem` before
mutation, so `appendSpliceInsertUnique(firstAd, ...)` mutates the
local copy that the loop just wrote to Redis. The mutation
**before** `sp.pipe.Exec(ctx)` in `synchSegmentIntoRedis` matters
because `addSegment` appends the *same* `SegmentItem` value to
`sp.processingSegments` (`addSegment` → `data.TimeText = ...;
sp.processingSegments = append(...)`), and the local `firstAd` and
`lastAd` pointers point at these values too. Verified by reading
`addSegment` at `segment_processor.go#L302-L322` — `sp.processingSegments`
is `[]SegmentItem` (not `[]*SegmentItem`), so `firstAd` and `lastAd`
are valid pointers into the same backing array for the duration of
`insertAdsIfNeeded`. (We use a slice trick: copy the modified
`adItem` values into the buffer via a temporary pointer; see
implementation for details.)

**Implementation note (gotcha):** `sp.processingSegments`
contains the **VOD buffer** (filled by `addSegment` only for VOD
segments — ad segments are written to the same buffer too, but the
pre-roll lookup specifically wants VOD members, not the just-
inserted ads). The pre-roll loop therefore reads from
`sp.processingSegments` filtered to `!IsAd` to be safe, even
though by construction no ad is in the buffer at that point in
the call (the ad loop hasn't run yet).

### 3.2 `internal/engine/scte35.go` (modified)

**3.2.1 — Add `buildSpliceInsertLine` helper** next to
`buildCueOutLine` / `buildCueInLine`:

```go
// buildSpliceInsertLine returns a per-segment #EXT-X-SCTE35 tag
// for one splice event stored on a SegmentItem. Wire format:
//
//   #EXT-X-SCTE35:CUE="<base64>",TYPE=<segType>,ID="<adbreak-id>"
//
// where <base64> is the same SpliceInfoSection shape as
// buildCueOutLine / buildCueInLine (SpliceImmediateFlag=true,
// OutOfNetworkIndicator=out, BreakDuration if out), <segType> is
// the decimal segmentation_type_id (0x30/0x34/0x35), and
// <adbreak-id> is the matching "adbreak-<int64>" string so the
// player can correlate the per-segment insert with the
// CUE-OUT/CUE-IN DATERANGE summary tags on the VOD<>ad edges.
//
// On buildSpliceInsert error the CUE= attribute is dropped and
// the line is emitted with only TYPE+ID, so the rest of the
// playlist survives. Mirrors the failure handling of
// buildCueOutLine (log + degrade, never panic).
func buildSpliceInsertLine(seg models.SegmentItem, ev models.SpliceInsert) string {
    out := ev.Type == "out"
    id := fmt.Sprintf("adbreak-%d", int64(ev.SpliceTimeSeconds))  // breaks come from one adStart...
    if seg.AdBreakID != "" { id = seg.AdBreakID }
    b64, err := buildSpliceInsert(uint32(ev.EventID), ev.Duration, out)
    if err != nil {
        log.Printf("[Engine] per-segment SCTE-35 encode failed (id=%s, type=0x%x): %v", id, ev.SegmentationTypeID, err)
        return fmt.Sprintf(`#EXT-X-SCTE35:TYPE=%d,ID="%s"`, ev.SegmentationTypeID, id)
    }
    return fmt.Sprintf(`#EXT-X-SCTE35:CUE="%s",TYPE=%d,ID="%s"`, b64, ev.SegmentationTypeID, id)
}
```

**3.2.2 — Extend `buildMediaPlaylistBody`** to emit
`buildSpliceInsertLine(seg, ev)` for each `ev` in
`seg.SliceInserts`, in the order the planner wrote them. Insertion
point: **after** any `EXT-X-DATERANGE` for that segment (so
DATERANGE remains the lead marker for the VOD↔ad edge) and
**before** the segment's `#EXTINF`. The detection runs only on
the *prepared* (`available`, `.pts`-marker-filtered) segments, as
the existing code does — a per-segment insert never references a
segment not in the playlist.

### 3.3 `internal/models/models.go` — **NO CHANGE**

`SpliceInsert` (lines 54–63) and `SegmentItem.SliceInserts` (line 123)
already exist with the exact field shape the TS code emits
(`type`, `eventId`, `spliceTimePts`, `spliceTimeSeconds`,
`duration`, `uniqueProgramId`, `segmentationTypeId`). Adding a
new `omitempty` field is unnecessary.

### 3.4 `internal/planner/segment_processor_test.go` (new, pure)

| Test | Asserts |
|---|---|
| `TestFindSegmentContainingPts_Inside` | pts lands inside a segment → returns the right index. |
| `TestFindSegmentContainingPts_Boundaries` | pts == MediaStart (inclusive), pts == MediaEnd (exclusive), pts between segments. |
| `TestFindSegmentContainingPts_BeforeAll` | pts < first MediaStart → (-1, false). |
| `TestFindSegmentContainingPts_AfterAll` | pts > last MediaEnd → (-1, false). |
| `TestFindSegmentContainingPts_FPRoundingTolerance` | pts slightly PAST the right edge of segment i (e.g. 6.012 vs MediaEnd 6.011999 from 540000/90000) but before segment i+1 starts → binds to segment i. Locks the FP-rounding fallback added in the 2026-09-03 revision. |
| `TestAppendSpliceInsertUnique_DedupOnSamePTS` | Same `(type, SpliceTimePTS)` twice → only one entry. |
| `TestAppendSpliceInsertUnique_KeepsDifferentPTS` | Different PTS → both kept. |
| `TestAdBreakEventIDFor_MaskAndFormat` | 1725014400 → 1725014400; negative → masked into [0, 0x7FFFFFFF]; stable across calls. |

### 3.5 `internal/planner/scte35_stamping_test.go` (extended)

| Test | Asserts |
|---|---|
| `TestStampSliceEvents_OnVODAndAdSegments` | Redis-gated (skips if `REDIS_ADDR` unreachable). Seeds `sp.planner.processingSegments` (the buffer lives on the shared Planner — see §3.1.3.4) with 5 VOD segments (4-6s, MediaStart 0..24). Calls `insertAdsIfNeeded(breakStart=30)`. ZRANGEs the ZSET, then for each member deserializes and asserts: (a) the VOD segment at `MediaStart≈22..28` carries a pre-roll event with `SpliceTimePTS = (30-8)*90000`, `SegmentationTypeID=0x30`, `EventID=adBreakEventIDFor(30)`; (b) the VOD segment at `MediaStart≈28..34` carries the 4s pre-roll; (c) the first ad carries a slice-point (`0x34`, `SpliceTimePTS = 30*90000`); (d) the last ad carries a cue-in (`0x35`, `SpliceTimePTS = (30+breakDuration)*90000`). |
| `TestStampSliceEvents_PreRollBeforeTimelineBase` | breakStart=2 (earlier than `timelineBase=0`); pre-roll pts < 0 → both pre-rolls skipped; slice-point + cue-in still fire. |
| `TestStampSliceEvents_PreRollTargetNotYetScheduled` | Pre-seeds `sp.planner.processingSegments` with 2 VOD segments that DON'T cover breakStart-8 (=92) or breakStart-4 (=96). The pre-rolls miss (WARN log) and the ad still gets stamped with slice-point + cue-in. |
| `TestStampSliceEvents_DedupOnReplay` | Call `insertAdsIfNeeded` twice at the same `breakStart`; assert each VOD target ends with exactly one pre-roll entry per delay (not two). The buffer is re-seeded between calls. |
| `TestStampSliceEvents_JSONRoundTrip` | Marshals a SegmentItem with 2 SliceInserts, asserts the JSON contains `splice_inserts` (non-empty), round-trips through Unmarshal with all 7 fields preserved. |

### 3.6 `internal/models/models_test.go` (extended)

| Test | Asserts |
|---|---|
| `TestSegmentItem_SliceInserts_OmitWhenEmpty` | Nil slice → JSON has no `splice_inserts` key. |
| `TestSegmentItem_SliceInserts_RoundTrip` | Populated slice round-trips; preserves every field. |
| `TestSegmentItem_SliceInserts_LegacyRescue` | Legacy JSON (no `splice_inserts` key) decodes to `nil` slice. |

### 3.7 `internal/engine/scte35_test.go` (extended)

| Test | Asserts |
|---|---|
| `TestBuildSpliceInsertLine_Format` | Segment with one `0x34` slice-point and one `0x30` pre-roll produces the expected two tags with the right `TYPE=`, `ID=`, and `CUE=` attributes. `CUE=` base64 round-trips through `DecodeBase64` with `OutOfNetworkIndicator=true`. |
| `TestBuildSpliceInsertLine_BuildErrorDropsCUE` | When the inner `buildSpliceInsert` returns an error (NaN duration), the tag is emitted with only `TYPE=...,ID=...` and no `CUE=` attribute. |
| `TestBuildSpliceInsertLine_CueInHasOONFalse` | Cue-in event (`Type="in"`) → decoded `OutOfNetworkIndicator=false`, no `BreakDuration`. |

### 3.8 `internal/engine/cue_tag_test.go` (extended)

| Test | Asserts |
|---|---|
| `TestBuildMediaPlaylistBody_EmitsSpliceInserts_BeforeEXTINF` | 5 segments, one carries a pre-roll `SliceInserts`. Body has the splice tag inserted **before** the segment's `#EXTINF` (and **after** any DATERANGE). |
| `TestBuildMediaPlaylistBody_EmptySliceInserts` | Segment with `SliceInserts=nil` → no `#EXT-X-SCTE35` emitted (preserves the existing VOD-only path). |
| `TestBuildMediaPlaylistBody_AllFourEventsInOrder` | VOD seg with 2 pre-rolls + first ad with slice-point + last ad with cue-in → body emits 4 tags in the right order (pre-rolls at their VOD positions; slice-point and cue-in on their ads; cue-in before the post-ad VOD EXTINF). |

### 3.9 `go.mod` / `go.sum` — **NO CHANGE**

`Comcast/scte35-go` v1.8.0 is already in `go.mod`; no new
dependencies.

### 3.10 Post-task documentation (per
`.github/instructions/simlive-summary.instructions.md`)

| File | Change |
|---|---|
| `.github/instructions/simlive-summary.instructions.md` | Add the 4 segmentation constants to the env-var / constant block; mention the per-segment `#EXT-X-SCTE35` emission alongside the existing `EXT-X-DATERANGE` description. |
| `docs/specification.md` | §6.1a (SCTE-35 cue tags): add a sub-section "§6.1b Per-segment splice inserts" with the four events, the tag format, the constants, and the planner→engine field mapping. |
| `docs/memories.md` | Append a 2026-09-03 session entry summarizing: four-event stamping, the `#EXT-X-SCTE35` tag shape, the dedup invariant, the pre-roll-before-timelineBase known gap. |
| `/memories/repo/simlive-go.md` | Add a one-paragraph note about the new per-segment splice inserts and the four segmentation-type constants. |

---

## 4. Data Flow

```
                    Planner tick                          Engine tick
                   ──────────────                       ──────────────
ads-v3 m3u8   ──► loadAdSegments
                       │
                       ▼
               insertAdsIfNeeded
               breakStart, breakID, breakEventID
                       │
                       ├─► PreRollDelaysSec loop  (0x30 events on VOD segs)
                       │     preRollPts = breakStart - delay
                       │     findSegmentContainingPts(sp.planner.processingSegments, preRollPts)
                       │     appendSpliceInsertUnique(...)
                       │
                       ▼ (ad loop — unchanged: stamps IsAd/AdBreak* on each ad)
Redis ZSET   ◄─── addSegment
simlive:timeline:{ch}
  VOD members now have
  SliceInserts: [{0x30, …} ×2]    ──►  ZRANGEBYSCORE
  ad members have                       │
  SliceInserts: [{0x34, …},            ▼
                 {0x35, …}]   generateHLSPlaylist
                                          │
                                          ▼
                                 buildMediaPlaylistBody
                                   EXT-X-DATERANGE on VOD↔ad edge
                                   (existing — unchanged)
                                          │
                                  +  EXT-X-SCTE35 on each segment
                                     with non-empty SliceInserts
                                     (NEW — emitted before #EXTINF)
                                          │
                                          ▼
                                 {TMP_DIR}/{ch}/playlist_<rend>.m3u8
                                          │
                                          ▼
                                 simlive:hls:{ch}:rendition:{rend}
                                          │
                                          ▼
                                 GET /live/{ch}/playlist_<rend>.m3u8
```

---

## 5. Verification

### 5.1 Unit / static (run by the implementer)

```bash
go build ./...
go vet ./...
go test ./internal/...
```

- All pure tests pass (no Redis needed).
- The Redis-gated `insertAdsIfNeeded` tests skip cleanly when
  `REDIS_ADDR` is unreachable (mirrors
  `TestInsertAdsIfNeeded_StampsAllAdsWithBreakMetadata`).

### 5.2 End-to-end smoke (operator-run; planner/engine startup is
destructive — destructive steps are explicitly NOT in this plan as
implementer actions per skill conventions)

1. Start planner + engine + api (`make start-all` or
   `docker compose up`).
2. Wait for a tick to land on a window that contains an ad break.
3. `redis-cli -p 6371 ZRANGE simlive:timeline:{ch} 0 -1 | grep ads-v3`
   — ad members carry `splice_inserts` (one slice-point + one cue-in
   per ad).
4. `redis-cli -p 6371 ZRANGE simlive:timeline:{ch} 0 -1 | grep -v ads-v3`
   — VOD members at `breakStart-8` and `breakStart-4` carry
   `splice_inserts` (one pre-roll each).
5. `curl -s http://localhost:8080/live/{ch}/playlist_360p.m3u8 | tee /tmp/playlist_360p.m3u8`
   — 4 new `#EXT-X-SCTE35` lines bracket the ad break: 2 pre-rolls
   on the VOD segments at the right positions, 1 slice-point before
   the first ad EXTINF, 1 cue-in on the last ad.
6. Decode any `CUE=` base64 with
   `go install github.com/Comcast/scte35-go/cmd/scte35@latest && scte35 decode <b64>`
   and assert the matching `seg_type_id` (0x30 / 0x34 / 0x35) and
   `splice_time` value.
7. Probe via `probe/player-test.html` — confirm the player reports
   the 8s/4s pre-roll window (or at minimum, loads the playlist
   without rejecting the unknown tag).

### 5.3 Grep-able invariants

- Every ad break produces exactly **4** `#EXT-X-SCTE35` tags in
  the media playlist: 2 pre-rolls + 1 slice-point + 1 cue-in.
- Pre-rolls sit on the VOD segment whose `[MediaStart, MediaEnd)`
  contains `breakStart-8` (resp. `breakStart-4`).
- `TYPE=` matches the segmentation_type_id field inside the
  decoded base64 (i.e. the player sees the same value either way).
- `SpliceTimePTS` is **always** `int64(seconds * 90000)`, no
  rounding noise.
- `EventID == UniqueProgramID == adBreakEventIDFor(breakStart)`
  for all 4 events of one break.
- Existing `EXT-X-DATERANGE` summary tags still emit 1 CUE-OUT and
  1 CUE-IN per break (additive, not replaced).

---

## 6. Known Gaps / Follow-ups

- **Pre-roll for the first ad of a channel.** Pre-roll
  `(breakStart - 8)` lands before `timelineBase` (i.e. before any
  VOD segment exists on the channel's local timeline). The
  planner skips these pre-rolls with a one-line
  `if preRollPts < 0 { continue }`. A future change could
  synthesize a "channel-start" VOD segment to host the pre-roll
  event, but the value is minimal (the first ad of a channel is
  already edge-detected by the existing `EXT-X-DATERANGE` summary
  tag).
- **Per-rung visibility.** All three rungs share the same
  SegmentItem ZSET, so the same `SliceInserts` are emitted on all
  three playlists. M2 (true per-rung separate segments) might need
  to revisit this.
- **Bitstream-level SCTE-35 injection.** Tag-only emission; the
  `.ts` bitstream is unchanged. The
  `tmp/inject-scte35_splice_information-v2-plan.md` v2 plan covers
  the FFmpeg/TSDuck-side bitstream injection separately.
- **Verifier hook.** `cmd/verifier/main.go` could decode the
  served playlist's `CUE=` base64s and assert
  `OutOfNetworkIndicator` + `SegmentationTypeID` match the
  expected values per segment. Not in this plan.

---

## 7. Decisions

- **Pre-roll window: `{8s, 4s}`** — mirrors `tmp/PRE_ROLL_DELAYS`
  and the v2 plan. One-line change to `PreRollDelaysSec` if
  different delays are wanted.
- **Both pre-rolls share `SegmentationTypeID=0x30`** (Placement
  Opportunity Start) — matches the TS reference.
- **Pre-rolls skip silently** if the target VOD segment isn't
  scheduled yet — same as the TS `if (target) { ... }`. WARN-log
  once per `(channel, breakStart, delay)` so systematic issues
  are visible.
- **CUE-IN goes on the *last* ad segment, not the post-ad VOD** —
  mirrors the TS reference. The existing `EXT-X-DATERANGE` CUE-IN
  (on the post-ad VOD edge) stays; both are emitted.
- **Pre-roll lookups are over `sp.planner.processingSegments` filtered to
  `!IsAd`.** This is the pre-ad-insert VOD buffer; filtering
  keeps the lookup honest even if a previous tick left an ad in
  the buffer (defensive, not currently possible by construction).
  The buffer lives on the shared `Planner` (not the per-call
  `SegmentProcessor`) so the pre-roll loop and `addSegment` see
  the same slice without a copy. The per-channel in-flight guard
  in `cmd/planner/main.go runPlannerTick` ensures the buffer is
  only touched by one call at a time, so the shared ownership is
  safe.
- **Existing `EXT-X-DATERANGE` CUE-OUT/IN tags stay.** They use
  the `IsAd/AdBreak*` summary fields and remain the engine's edge
  detection. The per-segment `SliceInserts` are additive.
- **`SpliceInserts` stays `omitempty` JSON** — VOD-only members
  round-trip unchanged.
- **Out of scope:** `EXT-X-DISCONTINUITY` (contradicts the
  270826 PTS-rewrite), bitstream-level SCTE-35 injection
  (FFmpeg/TSDuck territory), verifier hooks, and pre-roll for the
  **first** ad of a channel.
