# Tasks: 24/7 Playout EPG Cache Fallback

**Input**: Design documents from `/specs/010-epg-cache-fallback/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Included — unit tests for pure logic plus recorded integration validation against planner + engine + Redis + Postgres (Constitution Principle III).

**Organization**: Tasks are grouped by user story. Every task has an exact file path and is written to be executable without further context.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US4)
- Exact file paths are included in every task

## Path Conventions

Web application at the repository root.

- Go services: `cmd/{api,planner,engines}/`, `internal/{planner,api,models,utils}/`
- Client: `client/src/`
- Deployment config: `config/`

**Canonical day boundary (applies to all tasks)**: the target day is the host-local day (`utils.StartOfDay`/`EndOfDay` in `utils.LocalLocation()`, with `configs.TZ` only as a fallback). Channel content timezone is used only to format item timestamp strings.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Configuration surface for the on-disk cache.

- [X] T001 Add `EpgCacheDir string` to `Settings` in `internal/utils/envs.go`, resolved as `GetEnv("EPG_CACHE_DIR", filepath.Join(GetEnv("TMP_DIR", os.TempDir()), "epg-cache"))` (add `path/filepath` import)
- [X] T002 [P] Add `Environment=EPG_CACHE_DIR=/root/simlive-go/outputs/epg-cache` to `config/system/simlive-planner.service`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared cache reader, no-data error, and fallback-state store required by every story.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T003 Implement `RetainedSchedule` and the read path in `internal/planner/schedule_cache.go`: resolve `{EPG_CACHE_DIR}/<channel_id>.json`, unmarshal, and validate — `version` MUST be `1`; `channel_id` MUST equal the requesting channel; `channel_epg_id` MUST equal `channel.ChannelEpgID`; `items` MUST be non-empty; unreadable/malformed JSON MUST be treated as "no retained schedule" and MUST NOT fail the planner; there is NO age-based expiry
- [X] T004 Add `ErrNoScheduleData` sentinel (`errors.New("no schedule data from provider or retained schedule")`) in `internal/planner/planner.go`
- [X] T005 Implement fallback-state helpers in `internal/planner/fallback_state.go`: `MarkFallbackDay`, `HasFallbackDay`, `ClearFallbackDay` over Redis hash `planner:fallback:{channel_id}`, field = target day `YYYY-MM-DD`, value with `target_date`, `source_date`, `source_epg_id`, `day_start`, `day_end`, `created_at` (unix seconds), where `day_start`/`day_end` are the host-local target-day boundaries; TTL refreshed to `RedisExpiryHours` (default 26h) on every write
- [X] T006 [P] Add unit tests in `internal/planner/schedule_cache_test.go` covering version mismatch, channel mismatch, EPG-id mismatch, empty `items`, and corrupt JSON all resolving to "no retained schedule"

**Checkpoint**: Cache read + no-data error + fallback marker available — user stories can proceed.

---

## Phase 3: User Story 1 - Keep the channel on air across the day boundary (Priority: P1) 🎯 MVP

**Goal**: When the provider returns empty/error for a day targeted for planning, cover that host-local day from the retained schedule so playback never stops at midnight.

**Independent Test**: Seed a retained-schedule file for a channel, point the provider at an empty stub, trigger planning for the next day, and verify continuous coverage through the end of that day with no gap and no duplicate entries.

### Implementation for User Story 1

- [X] T007 [P] [US1] Implement `buildFallbackSchedule(items []models.PARTNERScheduleItem, targetDayStart, horizon time.Time, tz string) []models.PARTNERScheduleItem` in `internal/planner/schedule_fallback.go`: parse item start/end in `tz` (channel content timezone) for durations, skip items with duration ≤ 0, deep-copy `Programme.ProgrammeStreams`, repeat the sequence anchored at `targetDayStart` (host-local midnight as an absolute instant), assign `start = cursor` / `end = start + duration`, format with `configs.DATE_FORMAT_YYYY_MM_DD_HHMMSS` in `tz`, stop at `horizon`, and break when a cycle makes no progress
- [X] T008 [P] [US1] Add unit tests in `internal/planner/schedule_fallback_test.go` for: full-day coverage from a mid-day anchor, multi-cycle repetition, zero/negative-duration skip, target-day shift across months, and no-progress termination
- [X] T009 [US1] Update `ProcessEPGToTimeline` in `internal/planner/planner.go`: return `ctx.Err()` on cancellation; derive `processDate`/`horizon` from `utils.StartOfDay`/`EndOfDay` in `utils.LocalLocation()`; when the provider errors OR returns zero items, apply the no-duplicate guard (return without generating if the target day already has timeline entries via `HasDateInTimeline`), load the retained schedule, build fallback with `buildFallbackSchedule(cached.Items, utils.StartOfDay(processDate), horizon, channel.ContentTimezone)`, and seed sequence/running time from `getLastSegmentState`; when no valid retained schedule exists return `ErrNoScheduleData` instead of `nil`
- [X] T010 [US1] Implement marker lifecycle in `internal/planner/planner.go`: call `MarkFallbackDay` only after the fallback plan produces coverage and returns nil; never write a marker on failure, and never clear a pre-existing marker for another day (so a failed regeneration cannot create a false label)
- [X] T011 [US1] Add distinct structured logs in `internal/planner/planner.go` via `PLANNER:{channel_id}`: "fallback used" with channel, `source_date`, `target_date`, item count; and an error-level "no schedule data (provider empty, cache empty)" before returning `ErrNoScheduleData` (FR-011 — the day must not be marked prepared)
- [X] T012 [P] [US1] Add a unit test in `internal/planner/planner_test.go` asserting fallback is NOT generated when the target day already has timeline entries (no-duplicate guard)
- [ ] T013 [US1] Run integration validation from `specs/010-epg-cache-fallback/quickstart.md` (scenario 2): seed cache fixture, stub provider returns `data: []`, verify timeline covers the full target day, `sequence`/`running_time` strictly increase, and playback crosses the boundary without a gap
- [ ] T014 [US1] Run integration validation from `specs/010-epg-cache-fallback/quickstart.md` (scenario 5): clear the cache, stub returns empty, verify `ErrNoScheduleData` is logged distinctly, the day is not marked prepared, and the failure counter advances

**Checkpoint**: US1 works with a seeded cache.

---

## Phase 4: User Story 2 - Retain and refresh the last known-good schedule on disk (Priority: P1)

**Goal**: Persist every successful non-empty provider schedule to disk, refresh it on newer data, and never let empty responses or restarts destroy it.

**Independent Test**: Fetch a real schedule, confirm the file exists with the returned items; restart the planner with `ENABLE_CLEAN_UP=true` and confirm it survives; send an empty response and confirm the file is unchanged.

### Implementation for User Story 2

- [X] T015 [US2] Implement `SaveRetainedSchedule` in `internal/planner/schedule_cache.go` writing the `RetainedSchedule` schema from `data-model.md` atomically (temp file in the same directory, then rename) with `version` (`1`), `channel_id`, `channel_epg_id`, `source_date`, `fetched_at`, `timezone` (channel content timezone, informational only), and non-empty `items`
- [X] T016 [US2] In `ProcessEPGToTimeline` (`internal/planner/planner.go`), call `SaveRetainedSchedule` only after a successful non-empty provider response; never save on empty/error and never save fallback-generated items; log save failures without failing the planning run
- [X] T017 [US2] Update `prepareEnv` in `cmd/planner/utils.go` so the `ENABLE_CLEAN_UP=true` wipe removes `TMP_DIR` children except the resolved `EPG_CACHE_DIR` (works whether the cache dir is inside or outside `TMP_DIR`)
- [X] T018 [P] [US2] Add unit tests in `internal/planner/schedule_cache_test.go` for save/load round-trip, atomic overwrite, unchanged file after an empty response, and ignoring a partially written temp file
- [ ] T019 [US2] Run integration validation from `specs/010-epg-cache-fallback/quickstart.md` (scenarios 1 and 3, plus restart): real fetch writes the file, `ENABLE_CLEAN_UP=true` restart preserves it, empty fetch leaves the checksum unchanged
- [ ] T020 [US2] Run integration validation from `specs/010-epg-cache-fallback/quickstart.md` (scenarios 7 and 8): 30 consecutive simulated outage days retain coverage with no age cutoff (SC-008), and a mid-day planner restart on fallback continues without gaps or duplicates

**Checkpoint**: US1 + US2 deliver a self-populating fallback cache.

---

## Phase 5: User Story 3 - Return to real data automatically when the provider recovers (Priority: P2)

**Goal**: Replace not-yet-played fallback coverage with real provider data without operator action, preserving played segments and timeline continuity.

**Independent Test**: Put a channel on a fallback day, switch the provider to a different valid schedule, run one cycle, and verify played entries remain, unplayed entries are replaced, the marker clears only after success, and the cache updates.

### Implementation for User Story 3

- [X] T021 [US3] Extract the schedule-item planning loop from `ProcessEPGToTimeline` into `planScheduleItems(ctx, ch, processDate, horizon, items, useFallback)` in `internal/planner/planner.go` so pre-fetched real items can be replanned after a purge
- [X] T022 [US3] Implement `RecoverFallbackDay(ctx, ch, day time.Time) (bool, error)` in `internal/planner/planner.go`: return false when no fallback field exists for `day`; fetch the provider once; return false on empty/error; on non-empty save the retained schedule, `ZREMRANGEBYSCORE` `simlive:timeline:{channel_id}` for scores in `[max(now - WindowBufferSeconds, day_start), +inf]`, call `planScheduleItems` with the real items, and call `ClearFallbackDay` ONLY after that replan succeeds (leave the marker intact on failure)
- [X] T023 [US3] Update `prepareDateWithRetry` in `cmd/planner/main.go`: when `HasDateInTimeline` is true, call `RecoverFallbackDay` for that day and log the outcome before the skip path; keep the existing reset/mark-LIVE behavior
- [X] T024 [P] [US3] Add unit tests in `internal/planner/planner_test.go` for the cutover computation (`max(now - WindowBufferSeconds, day_start)`) and the fallback-day recovery decision
- [ ] T025 [US3] Run integration validation from `specs/010-epg-cache-fallback/quickstart.md` (scenario 4): fallback day → provider real; verify played entries retained, unplayed replaced from the new schedule, sequence/running time continuous, Redis marker cleared, and cache file updated

**Checkpoint**: The safety net is temporary, not permanent.

---

## Phase 6: User Story 4 - Operators can tell when a channel is running on stand-in content (Priority: P3)

**Goal**: Show a fallback label with the source day on the admin timeline for channels currently covered by stand-in content.

**Independent Test**: Put a channel on a fallback day covering now; the timeline shows the label with the source date. Channels on real data show no label.

### Implementation for User Story 4

- [X] T026 [P] [US4] Add `FallbackActive bool \`json:"fallback_active,omitempty"\`` and `FallbackSourceDate string \`json:"fallback_source_date,omitempty"\`` to `ChannelTimeline` in `internal/api/timeline_types.go`
- [X] T027 [US4] In `buildTimelinePayload` (`internal/api/monitor.go`), read `HGETALL planner:fallback:{channel_id}` and select the entry whose stored host-local `[day_start, day_end]` contains `now`; skip malformed/other-day entries; populate `fallback_active`/`fallback_source_date` (do not recompute the boundary from `content_timezone`)
- [X] T028 [P] [US4] Mirror the fields in `client/src/types/api.ts` as `fallback_active?: boolean` and `fallback_source_date?: string`
- [X] T029 [US4] Render a fallback label (Vuetify chip) in the channel card in `client/src/views/RunningChannelsTimeline.vue` showing `FALLBACK: <source_date>` when `fallback_active`; keep a stable min size so the header does not shift when the label appears
- [ ] T030 [US4] Run integration/UI validation from `specs/010-epg-cache-fallback/quickstart.md` (scenario 6): label appears during the fallback day, shows the source date, and is absent for real-data channels

**Checkpoint**: All four user stories are independently functional.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, verification, and log quality.

- [X] T031 [P] Document `EPG_CACHE_DIR`, the cache file contract, the `planner:fallback:{channel_id}` key, and the new timeline fields in `config/specification-server.md` and the Redis key table in `client/API-doc.md`
- [X] T032 Run `make fmt`, `make vet`, `go test ./internal/planner/...`, and `make build-client` from the repository root; fix all failures
- [ ] T033 Run all scenarios in `specs/010-epg-cache-fallback/quickstart.md` (1–8) end-to-end against planner + engine + Redis + Postgres and record the results (Constitution Principle III)
- [X] T034 [P] Review planner logs in `internal/planner/planner.go` to confirm fallback, recovery, and no-cache outcomes are distinguishable and include channel, source day, target day, and error context; adjust messages if ambiguous
- [X] T035 [P] Verify `internal/planner/schedule_cache.go` cannot crash the planner when `EPG_CACHE_DIR` is missing, unwritable, or contains partial files

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Setup — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Foundational
- **US2 (Phase 4)**: Depends on Foundational; supplies the write path that keeps US1 populated in production
- **US3 (Phase 5)**: Depends on US1 (markers) and US2 (cache save)
- **US4 (Phase 6)**: Depends on US1 (markers exist); can run in parallel with US3
- **Polish (Phase 7)**: Depends on all desired stories

### User Story Dependencies

- **US1**: No story dependencies — testable with a seeded cache file
- **US2**: No story dependencies — testable by observing disk writes
- **US3**: Requires US1 + US2
- **US4**: Requires US1 (marker write); independent of US3

### Within Each User Story

- Builders/cache primitives before orchestration
- Unit tests alongside (or before) their implementation
- Integration validation last
- Complete a story before moving to the next priority

### Parallel Opportunities

- T002 runs alongside T001 (different files)
- T006 depends on the `RetainedSchedule` type from T003 — write it after T003 lands
- T007/T008 (builder + tests) are separate files and can run together
- T012 (planner_test.go) exercises the guard added by T009 — run it after T009
- T018 (cache tests) is independent of T017 (cleanup logic)
- T024 (planner_test.go) can run in parallel with T021/T022 (planner.go — different files)
- T026/T028 (types) and T029 (UI) touch different files; T027 depends on T026
- T031/T034/T035 run alongside T032/T033

---

## Parallel Example: User Story 1

```bash
Task: "Implement buildFallbackSchedule in internal/planner/schedule_fallback.go"
Task: "Add unit tests in internal/planner/schedule_fallback_test.go"

# Then orchestration, followed by the guard test:
Task: "Update ProcessEPGToTimeline fallback path in internal/planner/planner.go"
Task: "Add no-duplicate guard test in internal/planner/planner_test.go"
```

## Parallel Example: User Story 4

```bash
Task: "Add fallback fields to ChannelTimeline in internal/api/timeline_types.go"
Task: "Mirror fallback fields in client/src/types/api.ts"
Task: "Render fallback label in client/src/views/RunningChannelsTimeline.vue"
```

---

## Implementation Strategy

### MVP First

1. Complete Phase 1 (Setup) and Phase 2 (Foundational)
2. Complete US1 — demonstrable with a seeded cache file
3. Complete US2 — makes the cache self-populating, which is the minimum production-usable increment
4. **STOP and VALIDATE** the day-boundary scenario before recovery work

### Incremental Delivery

1. Setup + Foundational → shared primitives ready
2. US1 → fallback coverage works with seeded data (demo)
3. US2 → cache auto-populates and survives restarts (deployable MVP)
4. US3 → automatic recovery when the provider returns
5. US4 → operator label
6. Polish → docs, format/vet/build, full quickstart run

---

## Analysis Resolution Traceability

| Finding | Task(s) |
|---------|---------|
| I1 mixed day boundaries | T009, T027 |
| C1 SC-008 uncovered | T020, T033 |
| U1 FR-007 implicit | T009, T012 |
| I2 stale fallback marker | T010, T027 |
| I5 marker clear order | T022 |
| A1 unverifiable performance goal | plan.md only |
| I3 structure drift | T005, T024 |
| C2 edge cases untested | T020 |
| U2 `dayStartInChannelTZ` undefined | T009 |
| C3 FR-011 indirect | T011, T014 |
| I4 test file naming | T024 |
| D2 integration automation | T013, T014, T019, T020, T025, T030, T033 |
| D1 duplicate wording | no task (spec-level, non-blocking) |

---

## Notes

- `[P]` tasks operate on different files with no incomplete dependencies
- Unit tests target pure logic; day-boundary and recovery behavior MUST be validated against the full stack
- Commit after each task or logical group; stop at checkpoints to validate independently
- Delete `EPG_CACHE_DIR` to reset the feature — there is deliberately no age-based expiry
