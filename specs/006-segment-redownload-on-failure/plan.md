# Implementation Plan: Segment Re-download on Failure

**Branch**: `006-segment-redownload-on-failure` | **Date**: 2026-10-29 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/006-segment-redownload-on-failure/spec.md`

## Summary

Make segment preparation robust against failed downloads and empty/partial files. A segment must only be marked prepared when all preparation steps succeed and its resulting file meets a minimum size. Failed or undersized segments are not appended to playlists and are retried on subsequent playout cycles, with stale partial files removed first.

## Technical Context

**Language/Version**: Go 1.26+
**Primary Dependencies**: rclone (object storage), Redis (timelines), ffmpeg (PTS rewrite), tsduck (PMT)
**Storage**: On-disk segment cache under `TMPDir/<channel>/<rendition>/` plus Redis prepared markers
**Testing**: Go unit tests for prepare logic; integration tests against planner+engine+Redis+Postgres stack
**Target Platform**: Linux server
**Project Type**: Backend service (engine)
**Performance Goals**: Playout loop tick every 500ms; segment preparation must not block playlist generation
**Constraints**: No broken/partial segments in HLS playlists; retries bounded by download window
**Scale/Scope**: Per-channel engine instances; fixed rendition ladder (360p, 720p, 1080p)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Streaming-First (III)**: Changes are confined to segment preparation and playlist filtering; playout loop remains intact. PASS.
- **Observability (IV)**: Failures logged with channel and sequence identifiers. PASS.
- **Simplicity & YAGNI (V)**: Fixed 100KB threshold, no new dependencies, no new retry queue. PASS.

## Project Structure

### Documentation (this feature)

```text
specs/006-segment-redownload-on-failure/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
└── tasks.md
```

### Source Code (repository root)

```text
internal/engine/
└── engine.go   # prepareSegmentFromSource, downloadAndPrepareJobs, generateRenditionPlaylist
```

**Structure Decision**: Single backend project; changes localized to `internal/engine/engine.go`.

## Complexity Tracking

No constitution violations. No complexity tracking required.