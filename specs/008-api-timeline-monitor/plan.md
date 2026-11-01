# Implementation Plan: API-Level Timeline Monitor (SSE)

**Branch**: `008-api-timeline-monitor` | **Date**: 2026-10-29 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/008-api-timeline-monitor/spec.md`

## Summary

Move running-channels timeline monitoring to the API layer. Persist monitored channels in a `monitored_channels` table, run a server-side monitor loop at `MONITOR_INTERVAL` (default 3s), and push updates to the frontend via Server-Sent Events. Frontend no longer polls. Engine and planner remain untouched.

## Technical Context

**Language/Version**: Go 1.26+ (API), TypeScript/Vue 3 (client)
**Primary Dependencies**: Gin, GORM/Postgres, Redis, Vue 3, EventSource (browser)
**Storage**: Postgres `monitored_channels` table; Redis timeline keys (read-only)
**Testing**: Go API tests; manual SSE verification
**Target Platform**: Linux server + web browser
**Project Type**: Web application (backend + frontend)
**Performance Goals**: SSE updates every 3s default; no client polling
**Constraints**: Read-only monitor; no engine/planner changes
**Scale/Scope**: Multiple SSE clients, multiple monitored channels

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Streaming-First (III)**: Monitor is read-only; engine/planner untouched. PASS.
- **Observability (IV)**: SSE/monitor logs via shared logger. PASS.
- **Simplicity & YAGNI (V)**: Single table, env interval, no new infra. PASS.

## Project Structure

### Documentation (this feature)

```text
specs/008-api-timeline-monitor/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
└── tasks.md
```

### Source Code (repository root)

```text
internal/api/
├── handler.go        # watch/unwatch + SSE endpoint
└── monitor.go        # monitor loop (new)
internal/models/
└── monitored_channel.go (new)
client/src/views/
└── RunningChannelsTimeline.vue
```

**Structure Decision**: Backend monitor + SSE in API; frontend consumes SSE and watch/unwatch buttons.

## API Endpoints

- `GET /api/channels/monitored` — list monitored channel ids.
- `POST /api/channels/:id/watch` — add to monitored set.
- `POST /api/channels/:id/unwatch` — remove from monitored set.
- `GET /api/channels/timeline/stream` — SSE updates for monitored channels.

## Complexity Tracking

No constitution violations. No complexity tracking required.