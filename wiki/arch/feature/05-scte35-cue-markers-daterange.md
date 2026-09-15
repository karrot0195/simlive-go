# SCTE-35 — Cue Markers (Feature Detail)

> Feature: `scte35-cue-markers-daterange`
> Source: `docs/300826_0616-implement-scte35-cue-markers.md`, `docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md`, `.github/instructions/simlive-summary.instructions.md`, `internal/engine/scte35.go`, `internal/planner/segment_processor.go`, `docs/memories.md`.

## Overview
SCTE-35 splice events are emitted in two forms: (1) `#EXT-X-DATERANGE` summary tags (CUE-OUT / CUE-IN) on VOD↔ad transitions, and (2) per-segment `#EXT-X-SCTE35` lines for each `SpliceInsert` stamped by the planner. The `Comcast/scte35-go` library (`v1.8.0`) builds the base64 `SpliceInfoSection` payload.

## Four splice events per ad break
Stamped by planner (`insertAdsIfNeeded`) on correct segments:
- `0x30` — pre-roll 8s (`SpliceTimePTS = breakStart - 8`)
- `0x30` — pre-roll 4s (`SpliceTimePTS = breakStart - 4`)
- `0x34` — slice-point / cue-out (`SpliceTimePTS = breakStart`) — attached to first ad segment
- `0x35` — cue-in (`SpliceTimePTS = breakEnd`) — attached to last ad segment

All four share same `EventID` (`uint32(int64(breakStart) & 0x7FFFFFFF)`) and `Duration` (`totalBreakDuration`).

## Engine emission (`buildMediaPlaylistBody`)
- `#EXT-X-DATERANGE` — before first ad EXTINF (CUE-OUT) and before first post-ad VOD EXTINF (CUE-IN). `DURATION` unquoted, trailing, capped at 3 decimal places (`20.02` not `20.019999999999996`).
- `#EXT-X-SCTE35` — before segment's `#EXTINF`, after any DATERANGE. Format: `CUE="<base64>",TYPE=<segType>,ID="<adbreak-id>"`. `TYPE` = decimal `segmentation_type_id` (`0x30` pre-roll, `0x34` slice-point, `0x35` cue-in).

## Key helpers (`internal/engine/scte35.go`)
- `buildSpliceInsert(eventID, durSec, out)` — builds `SpliceInfoSection` (OON bit, `SpliceImmediateFlag`, `BreakDuration` with `AutoReturn`, 90kHz ticks, CRC32, `Tier:4095`).
- `buildCueOutLine` / `buildCueInLine` — full DATERANGE line shape.
- `adBreakEventID(breakStart)` — stable positive 31-bit ID.
- `formatBreakDuration` — 3-decimal cap, strips trailing zeros / lone dot.

## Related docs
- `docs/300826_0616-implement-scte35-cue-markers.md`
- `docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md`
- `docs/memories.md` (SCTE-35 cue markers session; 15 new tests passing)
