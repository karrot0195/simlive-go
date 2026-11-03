# Implementation Plan: XML EPG Ad Pipeline

**Branch**: `014-xml-epg-scte35-pipeline` | **Date**: 2026-11-03 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/014-xml-epg-scte35-pipeline/spec.md`

## Summary

Restore the main branch's media signaling flow for ad breaks while keeping spec 013's schedule acquisition and fallback logic. EPG items continue to come from the partner XML source, and ad breaks now come only from XML ad markers. Each marker becomes one break anchored at its declared start; placement-opportunity-start markers are stamped at 8 s and 4 s before the break, cue-out on the first ad segment, placement-opportunity-end at 4 s before the break end, and cue-in on the first content segment after the break. Break IDs use a per-plan counter (masked to 16 bits for `unique_program_id`), PTS stays continuous across content and ads, and the static per-segment PMT from spec 001 is retained.

## Technical Context

**Language/Version**: Go 1.26.6 (module `simlive`)

**Primary Dependencies**: Existing only — Gin, GORM/PostgreSQL, go-redis/v9, TSDuck `tsp`, ffmpeg/ffprobe, rclone. No new external dependencies.

**Storage**: Partner XML EPG over HTTP; Redis timeline ZSET (`simlive:timeline:{channel}`) for segments; rclone storage for content and ad assets; retained schedule cache under `EPG_CACHE_DIR`.

**Testing**: `go test ./...`; planner unit tests for marker-driven breaks and boundary placement; model tests for playlist cue tags; partner client XML parsing tests; integration verification with Redis, rclone, TSDuck, and ffmpeg per constitution Principle IV.

**Target Platform**: Linux server (planner and engine systemd units).

**Project Type**: Web application backend — Go services in `cmd/api`, `cmd/planner`, `cmd/engines` with library packages under `internal/`.

**Performance Goals**: Each declared marker adds one ad-asset manifest fetch during planning; boundary scanning is linear in produced segments and small per-break marker counts. A one-day plan completes under the existing planner cadence.

**Constraints**: No new dependencies; static PMT contract from spec 001 (stream type 0x86, PID 0x1F00, registration 0x43554549) is unchanged; ad asset paths are bucket-inclusive and used verbatim; interval-based ad settings remain removed; break IDs need only be stable within a break.

**Scale/Scope**: One active partner profile per deployment; one day per plan; bounded markers per programme; changes confined to the planner, partner XML parser, segment model semantics, and playlist emission.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Library-First**: PASS — behavior lives in existing testable packages (`internal/planner`, `internal/models`, `internal/api`); binaries are unchanged entry points.
- **II. CLI Interface**: PASS (not directly applicable) — no new binary or CLI surface; existing services keep their interfaces.
- **III. Test-First (NON-NEGOTIABLE)**: PASS with obligation — implementation tasks start with failing tests for marker parsing, break insertion, pre-roll/post-roll/cue-in placement, PTS continuity, and playlist tags before code changes.
- **IV. Integration Testing**: PASS with obligation — HLS segment/SCTE-35 paths require integration checks: plan a day from a stub XML source, inspect the Redis timeline, run the engine, and verify PMT/SCTE-35 with the existing `pmt-verify`, `scte35-check`, and `hls-watch` tools.
- **V. Observability & Simplicity**: PASS — failures to load ad assets or stamp markers are logged with channel/sequence context; no new configuration knobs or abstractions beyond the required break context.

**Post-design re-check**: PASS — Phase 0/1 resolve the design without adding dependencies or violating a principle. No entries in Complexity Tracking.

## Project Structure

### Documentation (this feature)

```text
specs/014-xml-epg-scte35-pipeline/
├── plan.md              # This file
├── research.md          # Phase 0 decisions
├── data-model.md        # Phase 1 entities and field semantics
├── quickstart.md        # Phase 1 validation guide
├── contracts/
│   ├── epg-xml-input.md       # Upstream XML schedule contract consumed by the planner
│   ├── timeline-segment.md    # Timeline segment JSON contract
│   └── hls-cue-output.md      # Expected HLS cue and SCTE-35 playlist output
└── tasks.md             # /speckit-tasks output
```

### Source Code (repository root)

```text
internal/
├── planner/
│   ├── planner.go                # MOD: per-plan break counter; pass XML ads into processing
│   ├── segment_processor.go      # MOD: marker-driven break insertion, proactive pre/post-roll, pending cue-in
│   ├── segment_processor_test.go # MOD/NEW: break, boundary, PTS, and skip-path tests
│   └── schedule_pad.go           # MOD: shift inline ad marker starts when padding shifts an item
├── models/
│   ├── models.go                 # MOD: document cue-in-on-content semantics (no schema change)
│   └── scte35.go                 # MOD: emit CUE-IN DATERANGE for content segments carrying a cue-in role
├── api/
│   ├── partner_client.go         # MOD: parse inline ad markers and optional ad-programme flag
│   └── partner_client_test.go    # MOD: XML parsing coverage
├── engine/
│   └── engine.go                 # VERIFY: static PMT + splice injection paths unchanged
└── utils/
    └── tsduck.go                 # VERIFY: immediate vs pts_time signaling unchanged

config/
└── partner.yaml                  # unchanged; upstream endpoint already configured
```

**Structure Decision**: Single Go backend; planner behavior changes stay in `internal/planner`, segment/playlist semantics in `internal/models`, XML parsing in `internal/api`, and the engine's PMT/splice paths are verified rather than redesigned. No client or binary wiring changes.

## Implementation Outline

1. **XML input parsing** — extend `GetEPGScheduleItemsFromXML` to parse attribute-bearing `<ads path duration start/>` markers and to recognize a text-only `<ads>true</ads>` as a programme-level ad designation. Normalize all times to the server clock as today.
2. **Break model** — introduce a break context `{id, start, duration, path}` on the segment processor. Assign `id` from a per-plan counter; use the full value for the SCTE-35 event ID and mask to 16 bits for `unique_program_id`.
3. **Break insertion** — replace `processAdBreak`/`applyAdMarker` with a main-style flow: load the ad asset manifest (master → primary rendition → PTS ranges with `#EXTINF` fallback), append ad segments at the current timeline position, and compute the actual played duration from the asset segments.
4. **Boundary placement** — stamp the 8 s and 4 s placement-opportunity-start markers proactively while producing the content segments whose windows contain those positions; stamp the cue-out on the first ad segment; stamp the 4 s placement-opportunity-end while producing the ad segment containing it; carry a pending cue-in to the first content segment after the break, including across schedule-item boundaries. Pre-roll and placement-end `pts_time` values use the segment's timeline offset (`currentPtsOffset`), not the absolute wall clock, so they land in the rewritten PTS space the engine produces.
5. **Playlist emission** — emit the cue-in date-range from the content segment's cue-in role and break metadata, independent of the ad flag; keep per-segment SCTE-35 tags for every marker.
6. **Skip and failure paths** — filter markers already behind the plan start; skip markers with invalid times or zero/negative duration; skip a break whose asset cannot be loaded and continue content; stop at the day horizon.
7. **Padding alignment** — when schedule padding shifts a programme's start/end, shift its inline ad marker starts by the same delta so duplicated items keep breaks aligned with their windows.
8. **Validation** — `make fmt`, `make vet`, `make test`, `make build`; then run the quickstart scenarios with a stub XML source and the existing verification tools.

## Complexity Tracking

> No Constitution Check violations. No entries required.
