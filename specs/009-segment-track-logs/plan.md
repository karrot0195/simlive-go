# Implementation Plan: Persistent Segment Track Logs

**Branch**: `009-segment-track-logs` | **Date**: 2026-10-29 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/009-segment-track-logs/spec.md`

## Summary

Persist segment track entries in append-only log files per channel (max 1000 entries, oldest removed), expose an API to fetch last N, and add a FE button on the timeline screen to view logs.

## Technical Context

**Language/Version**: Go 1.26+ (API), TypeScript/Vue 3 (client)
**Primary Dependencies**: Gin, Vue 3, Vite
**Storage**: Append-only log files per channel (under TMPDir/logs/<channel>.jsonl)
**Testing**: Manual verification
**Target Platform**: Linux server + web browser
**Project Type**: Web application (backend + frontend)
**Performance Goals**: N/A
**Constraints**: Read-only monitor; no engine/planner impact; max 1000 entries
**Scale/Scope**: Per-channel log files

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Streaming-First (III)**: Monitor only; no pipeline changes. PASS.
- **Observability (IV)**: Adds persistent track logs. PASS.
- **Simplicity & YAGNI (V)**: File-based, fixed cap, no new deps. PASS.

## Project Structure

### Documentation (this feature)

```text
specs/009-segment-track-logs/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
└── tasks.md
```

### Source Code (repository root)

```text
internal/api/
└── segment_track.go (new)
client/src/views/
└── RunningChannelsTimeline.vue
```

**Structure Decision**: Backend log file + API endpoint; FE dialog to view.

## API Endpoints

- `GET /api/channels/:id/segment-track?limit=N` — fetch last N entries.
- Append-only log file per channel (max 1000).

## Track Recording Mechanism

- Recording is performed by the **API monitor loop** (`buildTimelinePayload` in `internal/api/monitor.go`), which is read-only with respect to the playout pipeline — engine and planner are untouched.
- For each monitored channel, the loop scans the channel's on-disk prepared segment files (reuse of the `GetPreparedSegments` disk scan) and appends a `SegmentTrackEntry{Status:"prepared"}` for each sequence not present in a process-local last-seen set (guarded by a mutex).
- Append failures and scan errors are logged via `utils.Logger` only; they never affect the SSE payload or playout behavior (FR-005).
- The last-seen set is process-local and resets on API restart (re-scanning will re-record currently-prepared segments; acceptable, idempotent-ish audit behavior).
- **Improvement**: `ClearSegmentTrack` (DELETE) now also resets the per-channel seen set, so clearing the log re-enables recording of currently-prepared and future segments (refill accepted). This resolves the stall where clearing caused no further entries to be logged.

## Complexity Tracking

No constitution violations. No complexity tracking required.