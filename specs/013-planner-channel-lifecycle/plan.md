# Implementation Plan: Planner & Engine Channel Lifecycle Reactivity

**Branch**: `013-planner-channel-lifecycle` | **Date**: 2026-11-03 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/013-planner-channel-lifecycle/spec.md`

## Summary

Channel start/stop currently relies on a global, blocking planner tick plus two different notions of "active" (`status` vs `is_active`). The result is a multi-minute activation delay, uncancelled in-flight preparation, stale playout for disabled channels, and status resurrection after a stop. The plan replaces the global tick with a non-blocking per-channel reconciler (polling, default 2s), unifies the active predicate across planner, engines, and API (`is_active = true AND status NOT IN ('INACTIVE','FAILURE')`), makes lifecycle DB writes conditional so cancelled work cannot revive a channel, propagates cancellation into EPG/rclone I/O, and purges channel output (Redis HLS/timeline/cursor/rendition metadata, disk, in-memory caches) as soon as a channel stops. Engines receive the same predicate, configurable reconcile cadence, restart spacing, and purge behavior.

## Technical Context

**Language/Version**: Go 1.26.6 (module `simlive`)

**Primary Dependencies**: Gin (HTTP), Gorm + `gorm.io/driver/postgres`, `redis/go-redis/v9`, `scte35-go`, `godotenv`, `lumberjack`, `term`; external binaries `rclone`, `ffmpeg/ffprobe`, tsduck (verification only)

**Storage**: PostgreSQL (`playout_channels` and related tables), Redis (timeline ZSET, cursor, failure counter, HLS playlists/segments, rendition metadata, fallback state), local `TMP_DIR` for prepared segments

**Testing**: `make test` → `go test ./...`; unit tests for pure lifecycle predicate/diff/transition helpers; integration verification via the running planner + engines + API + Postgres + Redis stack (`quickstart.md`)

**Target Platform**: Linux server; three long-running Go binaries (`api`, `planner`, `engines`) started by the makefile/systemd/docker compose

**Project Type**: Multi-binary backend web service with a separate Vue admin client (client unchanged by this feature)

**Performance Goals**: 95% of channel stops and starts take effect within 10 seconds, 100% within 30 seconds; no interruption to unrelated active channels; roster reconciliation query every 2 seconds by default

**Constraints**: No new external infrastructure or dependency; cancellation must be cooperative but bounded by existing HTTP/rclone timeouts; at most one active work unit per channel; deactivations must be durable and cannot be undone by in-flight work; configuration via environment variables with sensible defaults

**Scale/Scope**: Small fleet (single-digit to tens of channels), `PLANNER_WORKERS` default 4 concurrent preparations, `MAX_WORKERS` default 4 engine download workers; three services share one Postgres and one Redis

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle / Constraint | Status | Notes |
|---|---|---|
| I. Schema-First Knowledge Management | N/A | No knowledge-taxonomy writes in this feature. |
| II. Data-Plane vs Instruction-Plane Separation | N/A | No wiki/knowledge pages are written. |
| III. Streaming-First Architecture | PASS | Lifecycle changes touch preparation and playout; validation explicitly exercises planner + engine + Redis + Postgres together (`quickstart.md`). |
| IV. Observability & Structured Logging | PASS | FR-011 is implemented as structured transition logs (`channel`, `previous_state`, `new_state`, `reason`, `latency_ms`) and per-channel loggers are preserved. |
| V. Simplicity & YAGNI | PASS | No new service, broker, or dependency. A polling reconciler at 2s meets the latency target using the existing Postgres control plane. |
| Architecture: Go 1.26 backend | PASS | All changes stay in Go; no client/TypeScript changes required. |
| Architecture: dependencies | PASS | No new dependency; existing Gin/Gorm/Redis/rclone only. |
| Architecture: deployment | PASS | Same three binaries and same environment-variable configuration surface. |
| Architecture: configuration | PASS | New `CHANNEL_SYNC_INTERVAL` (default `2s`) via `utils.GetEnv`; existing `PLANNER_INTERVAL`, `PLANNER_WORKERS`, `MAX_WORKERS` retained. |
| Architecture: time handling | PASS | Existing channel-local timezone helpers are reused; no new time semantics. |
| Development workflow: testing | PASS | Pure logic gets unit tests; end-to-end behavior gets the stack-level quickstart validation. |

**Gate result**: PASS — no violations, no entries in Complexity Tracking.

## Project Structure

### Documentation (this feature)

```text
specs/013-planner-channel-lifecycle/
├── plan.md                      # This file
├── research.md                  # Phase 0 output
├── data-model.md                # Phase 1 output
├── quickstart.md                # Phase 1 output
├── contracts/
│   └── channel-lifecycle.md     # Phase 1 output
├── checklists/
│   └── requirements.md          # From /speckit-specify
└── tasks.md                     # Phase 2 output (/speckit-tasks)
```

### Source Code (repository root)

```text
cmd/
├── planner/
│   ├── main.go                  # Replace global tick + watcher with a reconciler loop
│   ├── utils.go                 # Resolve CHANNEL_SYNC_INTERVAL; keep PLANNER_INTERVAL
│   └── reconcile_test.go        # Supervisor state-machine tests (NEW)
├── engines/
│   └── main.go                  # Shared predicate/query, configurable interval, purge, restart spacing
└── api/
    └── main.go                  # Routing unchanged

internal/
├── models/
│   ├── models.go                # Add PlayoutChannel.IsActive; UpdateChannelReq.IsActive
│   ├── enums.go                 # Existing status constants (unchanged)
│   ├── lifecycle.go             # NEW: active predicate, status normalization, definition equality
│   └── lifecycle_test.go        # NEW: predicate/definition tests
├── lifecycle/
│   ├── reconcile.go             # NEW: ActiveChannels query + roster diff/desired-state helpers
│   └── reconcile_test.go        # NEW: diff/desired-state tests
├── planner/
│   └── planner.go               # Conditional lifecycle writes; ctx-aware EPG/rclone calls
├── engine/
│   ├── engine.go                # PurgeChannel: Redis HLS keys, rendition metadata, caches, disk
│   └── prep_version.go          # Reuse key naming helpers for purge
├── api/
│   ├── handler.go               # Normalize start/stop/update/delete transitions + best-effort purge
│   ├── handler_lifecycle_test.go# NEW: transition mapping tests
│   └── espx_client.go           # Context-aware EPG/detail requests
├── storage/
│   └── rclone.go                # Context-aware Cat
└── utils/
    └── envs.go                  # CHANNEL_SYNC_INTERVAL accessor (and docs comments)
```

**Structure Decision**: Single Go module with three binaries. Shared lifecycle semantics live in `internal/models` (pure predicate/transition rules) and `internal/lifecycle` (query + roster diff). Service-specific orchestration stays in `cmd/planner` and `cmd/engines`; purge primitives stay in `internal/engine` and `internal/planner`. No client changes.

## Phase 0 Research Summary

All technical unknowns are resolved in [research.md](./research.md): polling vs broker notification, the authoritative active predicate, per-channel supervisor state machine, resurrection prevention, cancellation propagation, purge ownership, delete semantics, engine parity, and testing strategy.

## Phase 1 Design Summary

- [data-model.md](./data-model.md): `is_active` field, lifecycle state machine, supervisor state, Redis key lifecycle.
- [contracts/channel-lifecycle.md](./contracts/channel-lifecycle.md): active predicate, endpoint behavior matrix, environment and log contracts.
- [quickstart.md](./quickstart.md): runnable stop/start/delete/engine-parity validation with pass/fail measurements.

## Post-Design Constitution Check

Re-evaluated after Phase 1: still PASS. The design adds one small shared package and no dependencies; observability requirements are expressed as a log contract; the quickstart remains the integration gate required by Principle III.

## Complexity Tracking

> No Constitution Check violations. No entries required.
