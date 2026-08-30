# SCTE-35 Cue Markers in m3u8 (EXT-X-DATERANGE)

**Plan version:** 0.1
**Date:** 2026-08-30
**Author:** (agent, via simlive-plan skill)
**Status:** Implementation in progress

> **Goal:** Emit SCTE-35 ad-break markers in the engine's media playlists
> (`playlist_360p.m3u8`, `playlist_720p.m3u8`, `playlist_1080p.m3u8`) so
> HLS players can resolve cue-out / cue-in against ad breaks. The
> markers are derived from the planner's existing ad-insertion pipeline
> (`insertAdsIfNeeded` in `internal/planner/segment_processor.go`) — no
> ESPX changes, no new env vars, no API changes. The base64 SCTE-35
> payload is built in Go using `github.com/Comcast/scte35-go@v1.8.0`
> (Apache-2.0); tag format mirrors `tmp/SCTE35Utils.ts`.

---

## 0. Progress

Track the work; tick as you go.

- [x] Read pre-flight context (`docs/specification.md`, summary
      instructions, `300826_0620-multi-rendition-abr-ladder.md`,
      `internal/engine/engine.go`, `internal/planner/{planner,
      segment_processor}.go`, `internal/models/models.go`, `tmp/scte35.ts`,
      `tmp/SCTE35Utils.ts`).
- [x] Drafted §1–§7 below; user approved `EXT-X-DATERANGE` tag style
      and `Comcast/scte35-go` payload library.
- [x] `go get github.com/Comcast/scte35-go@v1.8.0`.
- [ ] Add `IsAd / AdBreakID / AdBreakStart / AdBreakDuration` to
      `SegmentItem` (`internal/models/models.go`).
- [ ] Stamp ad breaks in `insertAdsIfNeeded`
      (`internal/planner/segment_processor.go`).
- [ ] New `internal/engine/scte35.go` — `BuildSpliceInsert`,
      `DecodeSpliceInsert`, `adBreakEventID`.
- [ ] `engine.generateHLSPlaylist`: factor body into pure
      `buildMediaPlaylistBody` + emit `EXT-X-DATERANGE` on
      VOD→ad / ad→VOD transitions.
- [ ] Unit tests: `internal/engine/scte35_test.go`,
      `internal/engine/cue_tag_test.go`,
      `internal/models/models_test.go` (omitempty round-trip).
- [ ] `go build ./...` + `go vet ./...` + `go test ./internal/...`.
- [ ] Post-task doc pass: `.github/instructions/simlive-summary.instructions.md`,
      `docs/specification.md` §6 / SegmentItem model, `docs/memories.md`.

---

## 1. Problem Statement

The engine today writes per-rung HLS media playlists with only the
minimum tags (`#EXTM3U`, `#EXT-X-VERSION:3`, `#EXT-X-TARGETDURATION`,
`#EXT-X-START`, `#EXT-X-MEDIA-SEQUENCE`, `#EXTINF`, `seg_N.ts`) — see
`engine.go#L513-L523`. The planner already inserts ad segments into
the timeline (via `insertAdsIfNeeded` at
`segment_processor.go#L176-L216`), but the engine has **no way to
distinguish an ad segment from a VOD segment**: `SegmentItem` carries
no `is_ad` flag, and the only ad trace in the data is
`StoragePath` containing the `ads-v3` bucket prefix (not used by the
engine).

Consequence: downstream players can't see where the ad break starts,
how long it lasts, or where content returns. There is no
`#EXT-X-CUE-OUT` / `#EXT-X-CUE-IN` and no `#EXT-X-DATERANGE` with a
SCTE-35 payload. The reference TypeScript code in `tmp/SCTE35Utils.ts`
documents the intended tag shape, but it shells out to an external
`/var/www/scte35-generator` binary that does not exist in this repo.

Reference: `tmp/scte35.ts` (constants only), `tmp/SCTE35Utils.ts`
(tag emitter with the trailing-unquoted-attribute gotcha — see
`tmp/SCTE35Utils.ts#L51-L59`).

---

## 2. Solution Overview

Three changes, layered so each step is testable in isolation:

1. **Model** — add four `omitempty` fields to `SegmentItem` so the
   planner can mark ad segments with break metadata that survives
   the Redis ZSET round-trip.
2. **Planner** — in `insertAdsIfNeeded`, capture `breakStart`,
   `breakDuration`, and a stable `breakID` from the same `globalRunningTime`
   the ads already consume, and stamp each ad `SegmentItem`. **No
   changes to ad-counting, scheduling, or PTS-rewrite.**
3. **Engine** — in `generateHLSPlaylist`, detect VOD→ad and ad→VOD
   transitions inside the prepared window and emit an
   `#EXT-X-DATERANGE` line on each transition, carrying a base64
   SCTE-35 `splice_insert` built with Comcast/scte35-go. The playlist
   body is factored into a pure `buildMediaPlaylistBody(...)` so the
   tags are unit-testable without Redis or the disk cache.

**Tag format** (mirrors `tmp/SCTE35Utils.ts`; see gotcha at
`tmp/SCTE35Utils.ts#L51-L59`):

```
#EXT-X-DATERANGE:ID="adbreak-<int64>",CLASS="com.apple.hls.avails",START-DATE="<RFC3339>",SCTE35-OUT="<base64>",DURATION=<n>,END-ON-NEXT=YES
```

(placed *before* the ad segment's `#EXTINF` / `seg_N.ts`)

```
#EXT-X-DATERANGE:ID="adbreak-<int64>",START-DATE="<RFC3339>",SCTE35-IN="<base64>",DURATION=<n>
```

(placed *before* the first VOD segment after the break)

- `START-DATE` is the break start in **UTC RFC3339**.
- `DURATION` is the break length in **seconds, unquoted, trailing**
  (the threefive `TagParser` parses a line as quoted-vs-unquoted by
  its last char; trailing unquoted is what works).
- `SCTE35-OUT/IN` is the base64 `splice_info_section` for
  `splice_insert` with `SpliceImmediateFlag=true`,
  `OutOfNetworkIndicator=<out>`, `BreakDuration.AutoReturn=true`,
  `BreakDuration.Duration=uint64(seconds*90000)`. The library's
  `Encode()` re-computes CRC32 automatically.

**Out of scope:** `EXT-X-DISCONTINUITY` at ad boundaries (the 270826
PTS-rewrite already keeps DTS continuous — adding it would
contradict that work); `SCTE35-OUT` injection into the actual `.ts`
bitstream (the player-side tag is sufficient for SSAI clients);
master-playlist / API changes (the master is unchanged; the API
serves the media playlist verbatim from Redis).

---

## 3. What Changed (by file)

### 3.1 `internal/models/models.go` (modified)

Add 4 `omitempty` fields to `SegmentItem` (`models.go#L23-L53`).
All `omitempty` so legacy members (VOD segments) round-trip to
JSON as before.

| Area | Change |
|---|---|
| SegmentItem | `IsAd bool \`json:"is_ad,omitempty"\`` |
| SegmentItem | `AdBreakID string \`json:"ad_break_id,omitempty"\`` (e.g. `"adbreak-1725014400"`) |
| SegmentItem | `AdBreakStart float64 \`json:"ad_break_start,omitempty"\`` (unix seconds; → `START-DATE`) |
| SegmentItem | `AdBreakDuration float64 \`json:"ad_break_duration,omitempty"\`` (seconds; → `DURATION` + SCTE-35 `BreakDuration.Duration`) |

### 3.2 `internal/planner/segment_processor.go` (modified)

In `insertAdsIfNeeded` (`segment_processor.go#L176-L216`), before
the ad loop, capture:

```go
breakStart := sp.planner.globalRunningTime
breakDuration := 0.0
for _, a := range sp.adSegments { breakDuration += a.Duration }
breakID := fmt.Sprintf("adbreak-%d", int64(breakStart))
```

Then, in the ad loop, stamp each ad `SegmentItem` before
`addSegmentToRedis`:

```go
adItem.IsAd = true
adItem.AdBreakID = breakID
adItem.AdBreakStart = breakStart
adItem.AdBreakDuration = breakDuration
```

No change to sequence numbering, PTS offset anchoring, ad-pool
trimming, or `lastAdTime` advance.

### 3.3 `internal/engine/scte35.go` (new)

Pure helpers over `github.com/Comcast/scte35-go/pkg/scte35`. No
external state. Import path: `github.com/Comcast/scte35-go/pkg/scte35`.

| Function | Lines | Role |
|---|---|---|
| `BuildSpliceInsert(eventID uint32, durSec float64, out bool) (string, error)` | TBD | Returns base64 `splice_info_section`. `SpliceInsert{SpliceEventID, SpliceImmediateFlag:true, OutOfNetworkIndicator:out, Program: NewSpliceInsertProgram(0), BreakDuration{AutoReturn:true, Duration: uint64(durSec*90000)} if out}`. `Tier:4095, SAPType:3`. `EncryptedPacket{EncryptionAlgorithm: None, CWIndex:255}`. Calls `sis.Base64()`. |
| `DecodeSpliceInsert(b64 string) (*scte35.SpliceInfoSection, error)` | TBD | Wrapper over `scte35.DecodeBase64`. Returns the parsed `SpliceInfoSection` (used by tests + the future verifier hook). |
| `adBreakEventID(breakStart float64) uint32` | TBD | Stable event id derived from the break's start: `uint32(int64(breakStart) & 0x7fffffff)` (positive 31-bit; avoids the 0xFFFFFFFF reserved/edge cases). |

All helpers log via `log`; all errors return through the second
return value (never panic).

### 3.4 `internal/engine/engine.go` (modified)

In `generateHLSPlaylist` (`engine.go#L472-L537`):

1. Factor the body-building loop (`engine.go#L513-L523`) into a pure
   helper `buildMediaPlaylistBody(segments []models.SegmentItem,
   targetDuration int) (string, error)` — same contract as
   `buildMasterPlaylist` (rendition.go#L86-L105) so it can be
   unit-tested without Redis/disk.
2. Inside the body loop, detect VOD→ad and ad→VOD transitions
   against the *prepared* (`available`) segments:
   - When `!prev.IsAd && seg.IsAd`: emit the
     `EXT-X-DATERANGE` CUE-OUT line **before** the `#EXTINF` for
     `seg`. `START-DATE = time.Unix(int64(seg.AdBreakStart), 0).UTC().Format(time.RFC3339)`.
     `eventID = adBreakEventID(seg.AdBreakStart)`,
     `durSec = seg.AdBreakDuration`.
   - When `prev.IsAd && !seg.IsAd`: emit the
     `EXT-X-DATERANGE` CUE-IN line **before** `#EXTINF` for `seg`.
     Use `prev.AdBreakID`, `prev.AdBreakStart`, `prev.AdBreakDuration`.
3. On `BuildSpliceInsert` error: log + emit the `EXT-X-DATERANGE`
   with the `SCTE35-OUT`/`SCTE35-IN` attribute omitted (keep `ID`,
   `START-DATE`, `DURATION`, `END-ON-NEXT=YES` so the player still
   gets the metadata — just no base64 payload). Avoids
   double-failures degrading the entire playlist.
4. Run the detection only on the prepared `available` slice (after
   the `.pts` marker filter at `engine.go#L484-L495`) so a CUE-OUT
   never references a segment not in the playlist.
5. Continue to write the result via the existing atomic-rename
   path and Redis `SET` at `engine.go#L525-L527`.

No change to `generateMasterPlaylist`, the API, the on-disk
download/GC window, or the rclone pipeline.

### 3.5 `internal/engine/scte35_test.go` (new, pure)

Tests for `BuildSpliceInsert` / `DecodeSpliceInsert` /
`adBreakEventID` using only the library + the helpers.

| Test | Asserts |
|---|---|
| `TestBuildSpliceInsert_Out` | `DecodeBase64` round-trip: `SpliceCommand` is `*SpliceInsert`, `OutOfNetworkIndicator=true`, `SpliceImmediateFlag=true`, `BreakDuration.AutoReturn=true`, `BreakDuration.Duration == uint64(30*90000)`, `SpliceEventID` matches. CRC valid. |
| `TestBuildSpliceInsert_In` | Same, with `OutOfNetworkIndicator=false`, `DurationFlag()` returns false (no `BreakDuration`). |
| `TestBuildSpliceInsert_Duration` | `durSec=12.345` → ticks `int64(12.345*90000) == 1111050`; encoded section length matches `scte35` library. |
| `TestAdBreakEventID_Stable` | Same input → same output across calls; distinct starts → distinct IDs. |
| `TestDecodeSpliceInsert_BadInput` | `DecodeSpliceInsert("not-base64!")` returns non-nil error. |

### 3.6 `internal/engine/cue_tag_test.go` (new, pure)

Tests for `buildMediaPlaylistBody`.

| Test | Asserts |
|---|---|
| `TestBuildMediaPlaylistBody_NoAds` | Output is byte-identical to today's `generateHLSPlaylist` body for an ad-free input. |
| `TestBuildMediaPlaylistBody_CueOutBeforeAd` | For a sequence `[VOD, VOD, AD, AD, VOD]`, the output contains an `EXT-X-DATERANGE` line **before** the first `AD` segment's `#EXTINF`. The line has `ID="adbreak-<n>"`, `CLASS="com.apple.hls.avails"`, `START-DATE="<RFC3339>"`, `SCTE35-OUT="<b64>"`, `DURATION=<n>`, `END-ON-NEXT=YES`. The `<b64>` decodes to a valid `splice_insert` with `OutOfNetworkIndicator=true`. |
| `TestBuildMediaPlaylistBody_CueInAfterAd` | Same sequence, also assert a CUE-IN `EXT-X-DATERANGE` line **before** the post-break `VOD`'s `#EXTINF`, carrying `SCTE35-IN="<b64>"` whose decoded `OutOfNetworkIndicator=false`. |
| `TestBuildMediaPlaylistBody_TrailingAttrUnquoted` | The CUE-OUT line ends with `,END-ON-NEXT=YES\n` (verified via `strings.HasSuffix`); the CUE-IN line ends with `,DURATION=<n>\n`. (This is the tmp/ gotcha.) |
| `TestBuildMediaPlaylistBody_BuildErrorDropsAttr` | When `BuildSpliceInsert` returns an error (e.g. NaN duration), the DATERANGE line is still emitted **without** the `SCTE35-OUT/IN` attribute, and the rest of the playlist is unaffected. |

### 3.7 `internal/models/models_test.go` (modified)

Append tests for the new fields (in the existing test file).

| Test | Asserts |
|---|---|
| `TestSegmentItem_AdFields_OmitEmpty` | A `SegmentItem` with `IsAd=false` (zero), empty `AdBreakID`, zero `MediaStart` round-trips through `json.Marshal → json.Unmarshal` with all four new fields absent in the JSON output. |
| `TestSegmentItem_AdFields_RoundTrip` | A populated ad `SegmentItem` round-trips, preserving `IsAd`, `AdBreakID`, `AdBreakStart`, `AdBreakDuration`. |

### 3.8 `go.mod` (auto-updated)

`go get github.com/Comcast/scte35-go@v1.8.0` was already run;
`go.mod` / `go.sum` will reflect `Comcast/scte35-go`, its
transitive `bamiaux/iobit`, and the `spf13/{cobra,pflag}` (CLI sub-
deps, unused in our code path) and `inconshreveable/mousetrap`
(brought in by cobra). `go mod tidy` should be run after
implementation to drop unused CLI deps if possible.

### 3.9 Post-task documentation (per `.github/instructions/simlive-summary.instructions.md`)

| File | Change |
|---|---|
| `.github/instructions/simlive-summary.instructions.md` | Add the 4 new `SegmentItem` fields, the new `internal/engine/scte35.go`, the `Comcast/scte35-go` dep, the new tag line, the `internal/engine/{scte35,cue_tag}_test.go` test files. |
| `docs/specification.md` | §6 (Ad Insertion): note that ad breaks now carry an `AdBreakID/AdBreakStart/AdBreakDuration` and the playlist emits `EXT-X-DATERANGE` with `SCTE35-OUT/IN`. SegmentItem model block (§6 / summary §528): add the 4 fields. |
| `docs/memories.md` | Append a 2026-08-30 session entry summarizing: planner stamping, engine tag emission, scte35-go choice, the trailing-unquoted-attr gotcha, ad-stability of `AdBreakID`. |

---

## 4. Data Flow

1. **Planner tick** (`cmd/planner/main.go` cron → `planner.ProcessEPGToTimelineWithRetry`).
2. **`insertAdsIfNeeded`** captures `breakStart = globalRunningTime`
   and `breakDuration = Σ ads[i].Duration`. Builds
   `breakID = "adbreak-<int64(breakStart)>"`. For each ad:
   - Stamps the 4 ad fields.
   - Calls `addSegmentToRedis(pipe, ctx, adItem.MediaStart, adItem)`.
   - Advances `globalRunningTime` (unchanged).
3. **Redis ZSET** `simlive:timeline:{ch}` now carries ad members
   with `is_ad=true, ad_break_id=adbreak-<n>, ad_break_start, ad_break_duration`.
4. **Engine tick** (1s ticker) — `generateHLSPlaylist` filters to
   the prepared `available` slice.
5. **`buildMediaPlaylistBody`** walks the prepared segments; on
   `!prev.IsAd && seg.IsAd` (CUE-OUT) and
   `prev.IsAd && !seg.IsAd` (CUE-IN) it appends the
   `EXT-X-DATERANGE` line before the `#EXTINF`.
6. **Atomic write** of `playlist_<rend>.m3u8` + Redis `SET`
   `simlive:hls:{ch}:rendition:{rend}` (24h TTL) — unchanged.
7. **API** serves the playlist verbatim at
   `GET /live/{ch}/playlist_<rend>.m3u8`. The master playlist
   (`GET /live/{ch}/playlist.m3u8`) is unchanged.

```
                       Planner tick                Engine tick
                      ──────────────              ──────────────
ads-v3 m3u8     ─►  loadAdSegments
                          │
                          ▼
                  insertAdsIfNeeded
                  breakStart, breakID, Σdur
                          │
                          ▼ (adItem.IsAd = true …)
Redis ZSET      ◄───  addSegmentToRedis
simlive:timeline:{ch}
  members now have
  is_ad, ad_break_*                ─►  ZRANGEBYSCORE
                                              │
                                              ▼
                                     generateHLSPlaylist
                                              │
                                              ▼
                                     buildMediaPlaylistBody
                                       (EXT-X-DATERANGE
                                        on VOD↔ad edge)
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
go test ./internal/models/... ./internal/engine/...
```

- All pure tests pass (no Redis needed).
- The Redis-gated `engine.GenerateHLSPlaylist` integration test (if
  any is added) skips cleanly when `REDIS_ADDR` is unreachable
  (mirrors `master_playlist_test.go`).

### 5.2 End-to-end smoke (operator-run; planner/engine startup is
destructive — destructive steps are explicitly NOT in this plan as
implementer actions per skill conventions)

1. Start planner + engine + api (`make start-all` or
   `docker compose up`).
2. Wait for a tick to land on a window that contains an ad break.
3. `curl -s http://localhost:8080/live/{ch}/playlist_360p.m3u8 |
   tee /tmp/playlist_360p.m3u8` — confirm two new
   `EXT-X-DATERANGE` lines bracket the ad segments.
4. Decode the `SCTE35-OUT` base64 with the lib's CLI
   (`go install github.com/Comcast/scte35-go/cmd/scte35@latest &&
   scte35 decode <b64>`) and assert `OutOfNetworkIndicator=true`,
   `SpliceImmediateFlag=true`, `BreakDuration` non-zero.
5. `redis-cli -p 6371 ZRANGE simlive:timeline:{ch} 0 -1 | grep ads-v3`
   — ad members carry `is_ad`, `ad_break_id`, `ad_break_start`,
   `ad_break_duration`.
6. Probe via `probe/player-test.html` — confirm the player reports
   the ad break boundaries (or at minimum, the player loads the
   playlist without rejecting the unknown tag).

### 5.3 Grep-able invariants

- `EXT-X-DATERANGE` lines appear **only** in playlists where
  `len(ads) >= 1` in the prepared window.
- `START-DATE` is **always** UTC RFC3339 (`Z` suffix or `+00:00`).
- `DURATION` is **always** unquoted and trailing.
- CUE-OUT/CUE-IN always have **matching IDs** (same `adbreak-<n>`).
- The number of CUE-OUT and CUE-IN tags in a window are equal
  (well-formed breaks).

---

## 6. Known Gaps / Follow-ups

- **Per-rung visibility.** The current implementation emits the same
  CUE-OUT/CUE-IN on all three rungs (`playlist_360p.m3u8` etc.)
  because they all reference the same shared segment cache. M2
  ("true per-rung separate segments" — see
  `docs/300826_0620-multi-rendition-abr-ladder.md`) would need to
  re-evaluate whether the same break metadata is broadcast across
  variants or only the active one.
- **Pre-roll warnings.** `tmp/scte35.ts` defines
  `SEG_TYPE_PLACEMENT_OPPORTUNITY_START` (0x30) and
  `PRE_ROLL_DELAYS = [8, 4]`. This plan emits only
  `splice_insert` (the actual cue-out point), not a separate
  `time_signal` with a pre-roll `segmentation_descriptor`. Adding
  pre-rolls is a strict superset of this work and can ship in a
  follow-up.
- **Verifier hook.** `cmd/verifier/main.go` could be extended with
  a "decode the served playlist's `SCTE35-OUT` and assert it parses
  + `OutOfNetworkIndicator=true`" check; not in this plan.
- **SCTE-35 in the .ts bitstream.** Tag-only emission; no
  splice-info-section injection into the ad `.ts` packets. Players
  that rely on the `.ts` PSI for ad-insertion (rare for HLS SSAI)
  will need a separate integration.
- **Ad-break ID collision.** `int64(breakStart)` is used directly
  in the ID. Two breaks that start at the same unix second across
  the same channel would collide. This is naturally rare (the
  `AdInsertionInterval` of 30s + the 5-ad pool caps the per-tick
  cadence), but if a future change shortens the guard, switch to
  `(breakStart << 8) | tickSequence` to keep IDs unique.

---

## 7. References

- [Comcast/scte35-go v1.8.0](https://github.com/Comcast/scte35-go)
  — `SpliceInfoSection.Encode() / Base64()`, `DecodeBase64`,
  `SpliceInsert{…, BreakDuration{…}}`, `SpliceImmediateFlag`.
- `tmp/scte35.ts` — constants
  `SEG_TYPE_*` and `PRE_ROLL_DELAYS` (not used in this plan; left
  as future pre-roll work).
- `tmp/SCTE35Utils.ts` — tag string format
  (`generateCUE_OUT` / `generateCUE_IN`) + the
  trailing-unquoted-attribute gotcha.
- `docs/290826_1448-implement-pts-sliding-window-logic.md` — local
  `PtsOffset` timeline; this plan does not touch it.
- `docs/300826_0214-planner-24h-next-day-rollover.md` — ad-insertion
  cadence and `globalRunningTime` semantics; this plan does not
  change them.
- `docs/300826_0620-multi-rendition-abr-ladder.md` — M1.5 shared
  segment cache + 3 advertised rungs; the M2 follow-up is called
  out as out-of-scope here.
