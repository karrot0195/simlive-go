# Implementation Plan: Running Channels Timeline

**Branch**: `004-running-channels-timeline` | **Date**: 2026-10-29 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/004-running-channels-timeline/spec.md`

## Summary

Add an API endpoint that returns upcoming timeline entries (from now to end) for every channel whose playout is enabled (`is_active = true`), grouped by channel, plus a Vue admin UI view that renders the per-channel timeline as a visual timeline/graph and flags gaps greater than 10 seconds between consecutive segments. Timeline data is read from the existing per-channel Redis sorted sets (`simlive:timeline:{channel_id}`); no planner changes.

## Technical Context

**Language/Version**: Go 1.26+ (backend), TypeScript/Vue 3 (frontend)
**Primary Dependencies**: Gin (HTTP), Gorm/Postgres, go-redis; Vue 3 + Vite + Vuetify (client)
**Storage**: PostgreSQL `playout_channels` table (channel identity + is_active + created_at); Redis sorted sets `simlive:timeline:{channel_id}` (score = running_time)
**Testing**: `go test ./...`; client lint/typecheck via pnpm
**Target Platform**: Linux server (API on :8080) + browser admin UI
**Project Type**: web-service + frontend (existing monorepo)
**Performance Goals**: N/A (v1 returns all upcoming entries in one response)
**Constraints**: No auth (matches existing /api surface); no pagination (out of scope); server clock is time reference
**Scale/Scope**: Fleet of playout channels; per-channel timeline may contain thousands of segments

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Streaming-First (III)**: Read-only access to existing timeline model; no changes to playout pipeline. No new integration tests required beyond API handler unit tests. PASS.
- **Simplicity & YAGNI (V)**: Reuses existing Redis/GORM patterns; no new dependencies. Trimmed projection avoids leaking internal PTS fields. PASS.
- **Observability (IV)**: Handler logs errors via existing logger conventions; uses gin error responses. PASS.
- **Data-Plane separation (II)**: No wiki/knowledge writes. PASS.

## Project Structure

### Documentation (this feature)

```text
specs/004-running-channels-timeline/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── timeline-api.md
└── tasks.md
```

### Source Code (repository root)

```text
internal/
├── api/
│   ├── handler.go          # add GetRunningChannelsTimeline handler
│   └── timeline_types.go   # new: response/projection types
├── models/
│   └── models.go           # SegmentItem (existing)
cmd/
└── api/
    └── main.go             # register GET /api/timeline/running (or /api/channels/timeline)
client/
└── src/
    ├── views/              # new RunningChannelsTimeline view
    ├── components/         # timeline graph component
    └── services/           # API client method
```

**Structure Decision**: Extend existing handler package and client; no new services. New route registered in cmd/api/main.go alongside /api/channels.

## Complexity Tracking

No constitution violations.