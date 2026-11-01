# Implementation Plan: 24/7 Playout EPG Cache Fallback

**Branch**: `010-epg-cache-fallback` | **Date**: 2026-10-30 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/010-epg-cache-fallback/spec.md`

## Summary

Make channels survive provider gaps: persist the most recent non-empty EPG schedule per channel to a JSON file on persistent local disk, and when a provider fetch returns empty or errors, time-shift the retained schedule to cover the target day and feed it through the existing planning path. Successful provider fetches replace the retained copy. When the provider recovers for a day that was fallback-filled, purge not-yet-played fallback coverage and re-plan from real data. Fallback usage is recorded in Redis and surfaced as a label on the admin timeline UI.

**Canonical day boundary**: the target day is the host-local day (`utils.StartOfDay`/`EndOfDay` in `utils.LocalLocation()`), per FR-014 and the project's timezone convention. Cached item times are formatted in `channel.ContentTimezone` only so the processor parses them correctly; no content-timezone day boundary is used.

## Technical Context

**Language/Version**: Go 1.26+ (planner, API); TypeScript / Vue 3 (client)

**Primary Dependencies**: Existing only — gin, gorm/postgres, redis (go-redis/v9), godotenv, rclone. No new dependencies.

**Storage**: Retained schedule JSON per channel under `EPG_CACHE_DIR` (default `{TMP_DIR}/epg-cache/<channel_id>.json`); fallback state in Redis hash `planner:fallback:{channel_id}`; existing timeline ZSET unchanged.

**Testing**: `go test ./internal/planner/...` for cache and fallback-builder unit tests; integration verification against the planner + engine + Redis + Postgres stack per constitution Principle III.

**Target Platform**: Linux server (systemd units under `config/system/`) + web browser.

**Project Type**: Web application (Go backend services + Vue admin client).

**Performance Goals**: Fallback adds at most one cache read and one cache write per channel per planner tick; each cache file is bounded to one day of schedule (≤ 1 MB); planner cadence is unchanged.

**Constraints**: No new dependencies; retained schedule must survive planner restarts including `ENABLE_CLEAN_UP=true`; no duplicate, rewind, or replacement of already-played segments; day boundaries are the host-local target day with `configs.TZ` only as a fallback, and fallback item timestamps are formatted in `channel.ContentTimezone` for correct parsing.

**Scale/Scope**: Per-channel cache file (one day of schedule, bounded by provider `$limit`); planner interval default 5m; admin timeline UI unchanged except a fallback chip.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Schema-First Knowledge Management**: Not applicable — no knowledge/wiki taxonomy changes.
- **II. Data-Plane vs Instruction-Plane Separation**: Not applicable — no change to `projects/simlive/knowledge/` or `CLAUDE.md` markers.
- **III. Streaming-First Architecture**: PASS with obligation — timeline continuity across fallback and recovery must be verified against the planner + engine + Redis + Postgres stack (see `quickstart.md`).
- **IV. Observability & Structured Logging**: PASS — fallback and recovery events logged through the per-channel `PLANNER:{channel_id}` logger with source day, target day, and item counts; fallback state exposed to the API/UI.
- **V. Simplicity & YAGNI**: PASS — file-based cache, reuse of the existing planning pipeline, no new dependencies, no new configuration beyond the cache directory.

## Project Structure

### Documentation (this feature)

```text
specs/010-epg-cache-fallback/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── epg-cache-file.md
│   ├── fallback-state-redis.md
│   └── timeline-api-delta.md
└── tasks.md             # /speckit-tasks output
```

### Source Code (repository root)

```text
internal/planner/
├── planner.go                 # MOD: ErrNoScheduleData, fallback orchestration, recovery
├── fallback_state.go          # NEW: MarkFallbackDay / HasFallbackDay / ClearFallbackDay (Redis hash)
├── schedule_cache.go          # NEW: retained-schedule load/save, atomic write, path resolution
├── schedule_cache_test.go     # NEW
├── schedule_fallback.go       # NEW: buildFallbackSchedule (repeat + time-shift to target day)
├── schedule_fallback_test.go  # NEW
├── planner_test.go            # NEW: recovery cutover + fallback-day decision tests
├── schedule_pad.go            # unchanged
└── schedule_pad_test.go       # unchanged

internal/utils/
└── envs.go                    # MOD: Settings.EpgCacheDir + EPG_CACHE_DIR

cmd/planner/
├── main.go                    # MOD: prepareDateWithRetry probes recovery for fallback days
└── utils.go                   # MOD: preserve the cache directory during ENABLE_CLEAN_UP

internal/api/
├── timeline_types.go          # MOD: ChannelTimeline fallback fields
└── monitor.go                 # MOD: read fallback state from Redis, populate payload

client/src/
├── types/api.ts               # MOD: ChannelTimeline fallback fields
└── views/RunningChannelsTimeline.vue  # MOD: fallback chip

config/system/
└── simlive-planner.service    # MOD: EPG_CACHE_DIR (optional override; default works)
```

**Structure Decision**: Backend change lives in `internal/planner` (planning behavior) plus a small read-only projection in `internal/api` and one label in the existing admin timeline view. No new packages or services; the cache is a sibling of existing `TMP_DIR` data.

## Phase 0 — Research

See [research.md](./research.md). All technical unknowns resolved: cache location/persistence, no-data semantics, time-shift algorithm, canonical day boundary, no-duplicate guard, recovery cutover, marker lifecycle, and operator visibility.

## Phase 1 — Design

- Data model: [data-model.md](./data-model.md)
- Contracts: [contracts/](./contracts/)
- Validation guide: [quickstart.md](./quickstart.md)

**Post-design re-check**: All constitution gates still pass. No new dependencies, no new services, timeline continuity explicitly covered by the integration scenarios, and fallback/recovery events are logged with the per-channel planner logger.

## Analysis Resolutions

Findings from `/speckit-analyze` (2026-10-30) and how the design resolves them.

| Finding | Resolution |
|---------|------------|
| I1 (HIGH) mixed day boundaries | Canonical target day is the host-local day per FR-014. `processDate`/`horizon` come from `utils.StartOfDay`/`EndOfDay` in `utils.LocalLocation()`; the fallback builder anchors at that absolute instant and formats timestamps in `channel.ContentTimezone` so `utils.ParseESPXTime` in the processor reads them correctly. Fallback markers store the same host-local `day_start`/`day_end`. |
| C1 (MEDIUM) SC-008 uncovered | Added quickstart scenario 7: 30 consecutive simulated outage days (clock shift) with no manual intervention. |
| U1 (MEDIUM) FR-007 implicit | Fallback planning gains an explicit guard: it runs only when the target day has no timeline coverage; sequence and running time are seeded from the last timeline member. A repeated call cannot duplicate coverage. |
| I2 (MEDIUM) stale marker | Mark the fallback day only AFTER coverage is produced; clear it on planning failure. The API label is driven by the marker, so a failed fallback cannot show a false label. |
| I5 (LOW) marker clear order | Clear the recovery marker only after the purge and replan succeed. |
| A1 (MEDIUM) unverifiable performance claim | Replaced with bounded-work goals (≤ 1 read + ≤ 1 write per channel per tick, file ≤ 1 MB, cadence unchanged). |
| I3 (LOW) structure drift | `internal/planner/fallback_state.go` and `planner_test.go` are now in the source tree. |
| C2 (MEDIUM) edge cases untested | Added quickstart scenarios 7 (multi-day outage) and 8 (planner restarts mid-day on fallback). |
| U2 (LOW) `dayStartInChannelTZ` undefined | Removed as a concept: `dayStart` is host-local midnight; only formatting uses the content timezone. |
| C3 (LOW) FR-011 indirect | Design requires `ErrNoScheduleData` to be logged with a distinct "no schedule data; cache empty" message and never to mark the day prepared. |
| I4 (LOW) test file naming | Recovery/cutover tests live in `internal/planner/planner_test.go`, not `schedule_fallback_test.go`. |
| D2 (MEDIUM) integration test automation | Integration validation remains the recorded quickstart run; the repository has no automated integration harness, and the constitution's stack-level requirement is satisfied by running those scenarios end-to-end. |
| D1 (LOW) duplicate wording | Spec edits are out of scope for this command; non-blocking. |

**Stale artifact note**: `tasks.md` predates these resolutions. Regenerate it with `/speckit-tasks` before implementation.

## Complexity Tracking

No constitution violations. No complexity justification required.

Delivery note: the recovery cutover (purge not-yet-played fallback coverage, then re-plan real data) is required by FR-010/SC-005 and is isolated in a single planner method so it can be reasoned about and tested independently.
