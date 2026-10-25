# Implementation Plan: Static SCTE-35 PMT

**Branch**: `001-static-scte35-pmt` | **Date**: 2026-10-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-static-scte35-pmt/spec.md`

**Request**: [231026-partner-feedback-number-version-pmt-static.md](../../requests/231026-partner-feedback-number-version-pmt-static.md)

## Summary

Ensure the SCTE-35 elementary stream (stream_type 0x86) is declared in the PMT for **every** segment regardless of whether it carries SCTE-35 packets. Currently, the engine only runs `tsp -P pmt --add-pid` for ad segments (via `ConfigurePmtForAdsTs`) and segments with splice events (via `InjectSpliceEvent`). Regular VOD segments without splice events leave the PMT unchanged — meaning the SCTE-35 stream is absent from those segments' PMTs. The fix extends PMT configuration to all segments: every segment is processed from its source through exactly one of three `tsp -P pmt --add-pid 0x1F00/0x86` paths (VOD, ad, or splice-injected), so the SCTE-35 stream is declared in every PMT and the resulting PMT content is byte-identical across segments. Each path is idempotent on the elementary stream entry, so repeated runs do not duplicate the ES entry.

## Technical Context

**Language/Version**: Go 1.26+ (Go+ language, custom Go-based transpiled language)

**Primary Dependencies**: TSDuck (`tsp` binary) for MPEG-TS manipulation including PMT modification; ffmpeg for PTS offset rewriting; rclone for object storage; Redis for timeline state; SCTE35-Go for splice insert encoding

**Storage**: Redis (timeline sorted sets), on-disk segment cache, object storage (rclone)

**Testing**: Unit tests for pure logic (PTS math, m3u8 parsing, segment item construction). Integration tests for planner→engine→API flow require full stack (Redis + Postgres + rclone + TSDuck + ffmpeg)

**Target Platform**: Linux server (Docker Compose deployment)

**Project Type**: Streaming server (simulated-live HLS playout engine)

**Performance Goals**: PMT configuration must not add measurable latency to segment preparation (< 100ms per segment). The `tsp` pipeline already runs for PTS rewriting and splice injection; PMT-only runs should be fast (< 1s).

**Constraints**: Must not change SCTE-35 packet content, ad insertion logic, HLS playlist generation, or rendition ladder. Must use existing TSDuck `tsp` binary — no new dependencies.

**Scale/Scope**: Multi-channel, multi-rendition (360p/720p/1080p). Validated across all ladder rungs.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Compliance | Notes |
|-----------|------------|-------|
| **III. Streaming-First Architecture** | ✅ Pass | Change is within the playout pipeline (segment preparation). Requires integration tests against running planner+engine+Redis+Postgres stack. |
| **IV. Observability & Structured Logging** | ✅ Pass | FR-005 mandates logging PMT anomalies using existing `utils.Logger` structured logging. |
| **V. Simplicity & YAGNI** | ✅ Pass | Fix is minimal: ensure SCTE-35 stream is declared in PMT at stream init time. No new dependencies, no new services, no dynamic transcoding. Complexity justified by failing partner test. |
| **Architecture Constraints** | ✅ Pass | Uses existing TSDuck `tsp` binary. No new dependencies. No config file changes. |

**Gate Result**: ✅ All gates pass. No violations requiring complexity justification.

**Post-Design Re-check**: ✅ Still passes. Design is minimal (one new function mirroring existing `ConfigurePmtForAdsTs`), uses existing TSDuck dependency, no new services or config changes. Integration tests cover the full stack per Principle III.

## Project Structure

### Documentation (this feature)

```text
specs/001-static-scte35-pmt/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
└── tasks.md             # (created by /speckit-tasks)
```

### Source Code (repository root)

```text
internal/
├── engine/
│   ├── engine.go        # Main engine loop, segment preparation pipeline
│   ├── rendition.go     # Rendition ladder, playlist builders
│   └── scte35.go        # SCTE-35 DATERANGE / splice insert line builders
├── utils/
│   └── tsduck.go        # TSDuck tsp wrappers: InjectSpliceEvent, ConfigurePmtForAdsTs
├── planner/
│   ├── planner.go       # Main planner loop
│   └── segment_processor.go  # Timeline construction, ad insertion
└── models/
    └── models.go        # SegmentItem, SpliceInsert, etc.
```

**Structure Decision**: Single Go project. The PMT fix primarily touches `internal/utils/tsduck.go` (add a new function or modify existing PMT configuration) and `internal/engine/engine.go` (call site in segment preparation pipeline).

## Links

- Request: [231026-partner-feedback-number-version-pmt-static.md](../../requests/231026-partner-feedback-number-version-pmt-static.md)
- Spec: [spec.md](spec.md)
- Tasks: [tasks.md](tasks.md)
- Verification Skill: [scte35-pmt-verify/SKILL.md](../../.agents/skills/scte35-pmt-verify/SKILL.md)

## Complexity Tracking

> No Constitution Check violations — section not required.