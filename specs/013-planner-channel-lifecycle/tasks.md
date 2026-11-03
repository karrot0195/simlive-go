---

description: "Task list template for feature implementation"
---

# Tasks: Planner & Engine Channel Lifecycle Reactivity

**Input**: Design documents from `/specs/013-planner-channel-lifecycle/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/channel-lifecycle.md, quickstart.md

**Tests**: Unit test tasks are included because plan.md and research.md (R12) explicitly name test files as deliverables. Stack-level validation is covered by the final quickstart task.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- Single Go module `simlive` at repository root
- Binaries: `cmd/api/`, `cmd/planner/`, `cmd/engines/`
- Shared code: `internal/`
- No client (TypeScript) changes required except documentation alignment (T033)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Baseline verification and the shared configuration knob every later phase reads

- [x] T001 Run `make fmt && make vet && make test` from the repository root to confirm a clean baseline before changes
- [x] T002 [P] Add a `CHANNEL_SYNC_INTERVAL` accessor (default `2s`, invalid/≤0 falls back to default) in internal/utils/envs.go with a docs comment per contracts/channel-lifecycle.md §4

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared active predicate, model field, and roster query that ALL user stories depend on

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T003 [P] Add `PlayoutChannel.IsActive bool` with `gorm:"column:is_active;not null;default:true"` and `json:"is_active"`, plus `UpdateChannelReq.IsActive *bool`, in internal/models/models.go
- [x] T004 [P] Create the active predicate `is_active == true AND status NOT IN ('INACTIVE','FAILURE')`, status normalization (`is_active=true`+`INACTIVE` → `PREPARE`; `is_active=false`+`PREPARE|LIVE` → `INACTIVE`), contradiction validation (reject with 400), and definition equality (name, base_name, content_timezone, channel_epg_id, use_ad_epg_ads) in internal/models/lifecycle.go
- [x] T005 Write unit tests for the predicate, normalization, contradiction validation, and definition equality in internal/models/lifecycle_test.go (depends on T004)
- [x] T006 Create the `ActiveChannels` query (predicate-filtered, ordered by `id` for stable diffs) and roster diff/desired-state helpers (new / removed / unchanged / definition-changed / stopping-reactivation) in internal/lifecycle/reconcile.go (depends on T004)
- [x] T007 [P] Write unit tests for roster diff and desired-state helpers in internal/lifecycle/reconcile_test.go (depends on T006)

**Checkpoint**: Foundation ready — user story implementation can now begin

---

## Phase 3: User Story 1 - Stop all work for a deactivated or removed channel (Priority: P1) 🎯 MVP

**Goal**: Stopping or removing a channel cancels all preparation/playout work, purges output, and cannot be undone by in-flight work

**Independent Test**: Start with two active channels. Stop channel A and confirm that all preparation and playout work for A ceases within the target latency (p95 ≤10s, max ≤30s), while channel B continues without interruption (quickstart Scenarios A, D, G).

### Implementation for User Story 1

- [x] T008 [US1] Normalize stop/delete transitions (`status='INACTIVE'`, `is_active=false`, `updated_at` bump) and implement the best-effort immediate Redis purge (HLS master/renditions, rendition metadata, timeline, cursor, fallback; preserve `planner:failures:{id}`) plus segment-track clear in internal/api/handler.go (depends on T003, T004)
- [x] T009 [P] [US1] Write unit tests for the stop/delete/start/update transition mapping in internal/api/handler_lifecycle_test.go (depends on T008)
- [x] T010 [US1] Replace the global blocking tick + watcher in cmd/planner/main.go with a per-channel supervisor reconciler that cancels a channel's context when it leaves the active roster (deactivated or deleted) and waits for exit before cleanup (depends on T006)
- [x] T011 [US1] Make planner lifecycle writes conditional on the active predicate — `markChannelLive` becomes `UPDATE ... WHERE id=? AND is_active=true AND status NOT IN ('INACTIVE','FAILURE')` returning whether the transition happened; skip failure-counter/`MarkChannelFailed` writes when the supervisor context is cancelled — in internal/planner/planner.go (depends on T004)
- [x] T012 [P] [US1] Add `context.Context` parameters to `ESPXClient.GetEPGScheduleItems` and `GetChannelDetailWithEPG` using `http.NewRequestWithContext` in internal/api/espx_client.go
- [x] T013 [P] [US1] Make `RcloneService.Cat` accept a parent context and derive its 10s timeout from it in internal/storage/rclone.go
- [x] T014 [US1] Run `CleanChannelData` (timeline, cursor, failure counter) only after the supervisor's work unit has fully exited, never concurrently, in cmd/planner/main.go (depends on T010)
- [ ] T015 [P] [US1] Write supervisor state-machine unit tests (start, cancel, no-duplicate, cleanup-after-exit) in cmd/planner/reconcile_test.go (depends on T010)

**Checkpoint**: At this point, User Story 1 should be fully functional — stop/delete cancels, purges, and cannot resurrect

---

## Phase 4: User Story 2 - Start preparation promptly when a channel is activated (Priority: P1)

**Goal**: Reactivation (re-add or state change) begins preparation within the target latency, never blocked by other channels' work, with at most one work unit per channel

**Independent Test**: With a channel inactive, reactivate it via both paths (`POST /start` and `PATCH {"status":"LIVE"}`); preparation begins within the target latency even while other channels are preparing (quickstart Scenarios B, C, F).

### Implementation for User Story 2

- [x] T016 [US2] Start a supervisor immediately for each newly active channel in the reconcile pass (no waiting for `PLANNER_INTERVAL` or other channels' ticks), retaining the `PLANNER_WORKERS` semaphore so waiting supervisors never block discovery, in cmd/planner/main.go (depends on T010)
- [x] T017 [US2] Implement supervisor `stopping`/`restart_pending` handling so a reactivation during shutdown queues exactly one restart after `done` closes, and duplicate activation requests are harmless, in cmd/planner/main.go (depends on T016)
- [x] T018 [US2] Implement the API activation transitions — `POST /:id/start` writes `PREPARE`/`true` with best-effort failure-counter clear; PUT/PATCH with `status=PREPARE|LIVE` or `is_active=true` normalizes to active per contracts/channel-lifecycle.md §2 — in internal/api/handler.go (depends on T004; sequential with T008, same file)

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently — stop/start cycles meet the 10s/30s targets

---

## Phase 5: User Story 3 - One consistent active/inactive definition across the platform (Priority: P2)

**Goal**: Planner, API, and failure path agree on the single predicate; deactivation is durable with no resurrection and no failure churn

**Independent Test**: Deactivate a channel through each supported path; every service reports inactive within the target latency; failure-disabled channels stay stopped until explicit operator reactivation; 20 stop/start cycles show zero resurrections (quickstart Scenarios G, J).

### Implementation for User Story 3

- [x] T019 [US3] Apply the shared predicate to all API surfaces — honor the optional `is_active` query filter on `GET /api/channels` and use predicate-based purge eligibility — in internal/api/handler.go (depends on T004, T008)
- [x] T020 [US3] Make the failure-threshold path set `is_active=false` only (status unchanged), guard it against cancelled contexts, and ensure no automatic re-enable path exists so there is no stop/start churn, in internal/planner/planner.go (depends on T011)
- [x] T021 [US3] On startup, reconcile the roster immediately (first pass at boot, not after a full periodic cycle) so mixed-state platforms converge within the target latency, in cmd/planner/main.go (depends on T016)

**Checkpoint**: All activation/deactivation paths converge on one predicate; deactivation is durable

---

## Phase 6: User Story 4 - Playout service parity (Priority: P2)

**Goal**: Engines observe the same predicate and cadence as the planner, purge output on stop, and never serve stale session data

**Independent Test**: Stop and restart a channel; engine playout stops within the target latency, HLS/rendition keys and disk directory are purged, and playout resumes without restarting the engine process (quickstart Scenario E).

### Implementation for User Story 4

- [x] T022 [US4] Replace the hardcoded 5s sleep and status-only selection in cmd/engines/main.go with the shared `ActiveChannels` query/predicate and `CHANNEL_SYNC_INTERVAL` from T002, keeping the 500ms playout tick and `CancelCauseFunc` (depends on T002, T006)
- [x] T023 [US4] Implement `PurgeChannel` in internal/engine/engine.go: clear `rendMetaCache`, delete `simlive:hls:{id}`, `simlive:hls:{id}:rendition:*`, `simlive:renditions:{id}` Redis keys, and remove `TMP_DIR/{channel}` (depends on T004)
- [x] T024 [P] [US4] Extract/reuse the Redis key naming helpers for purge in internal/engine/prep_version.go (depends on T023)
- [x] T025 [US4] On definition change, cancel the old playout loop and spawn the replacement on the NEXT reconcile pass (never in the same pass) so no overlap uses shared directories/Redis keys; keep `isChannelChanged` limited to definition fields via the shared helper, in cmd/engines/main.go (depends on T022)
- [x] T026 [US4] Call `PurgeChannel` when a channel leaves the active roster (deactivated, removed, or failure-disabled with `status` still `LIVE`) in cmd/engines/main.go (depends on T022, T023)

**Checkpoint**: Engines stop and resume in lockstep with the planner; no ghost streams or stale HLS output

---

## Phase 7: User Story 5 - Lifecycle transparency (Priority: P3)

**Goal**: Every lifecycle decision is explainable from structured logs (channel, previous/new state, reason, latency)

**Independent Test**: Perform a stop and a start, then grep logs to confirm each decision shows channel id, previous state, new state, reason, and timing (quickstart Scenario H).

### Implementation for User Story 5

- [ ] T027 [P] [US5] Emit structured lifecycle logs (`event` ∈ lifecycle_start/stop/restart/cancel, `channel`, `previous_state`, `new_state`, `reason`, `latency_ms`, `cause`) in cmd/planner/main.go and internal/planner/planner.go, preserving per-channel loggers `PLANNER:{id}` (depends on T010)
- [ ] T028 [P] [US5] Emit the same structured lifecycle logs in cmd/engines/main.go and internal/engine/engine.go, preserving per-channel loggers `ENGINE:{id}` (depends on T022)
- [ ] T029 [P] [US5] Emit API lifecycle logs with the `[api] channel=...` prefix for start/stop/delete decisions in internal/api/handler.go (depends on T008, T018)

**Checkpoint**: All user stories independently functional and observable

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Documentation alignment, validation, and merge gates

- [ ] T030 [P] Align client/API-doc.md and client/swagger.yaml with the hard-delete DELETE semantics and the stop/purge behavior per research.md R9
- [x] T031 [P] Run `make fmt`, `make vet`, `make test` and fix any failures
- [ ] T032 Run the full quickstart.md validation (Scenarios A–J) against the running stack — `make build && make start-all`, two channels registered, measure stop/start latency, verify Redis/disk purge, 20-cycle no-resurrection check, engine parity, startup convergence, `CHANNEL_SYNC_INTERVAL` responsiveness
- [ ] T033 Re-check the constitution gate: Principle III (stack validation done in T032), Principle IV (structured logs from Phase 7), Principle V (no new dependencies)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Foundational — MVP
- **US2 (Phase 4)**: Depends on US1 (T010 supervisor reconciler is shared); API task T018 shares handler.go with T008
- **US3 (Phase 5)**: Depends on US1 (conditional writes T011) and US2 (reconciler start path T016)
- **US4 (Phase 6)**: Depends only on Foundational + T002 — can start in parallel with US1 once Phase 2 is done
- **US5 (Phase 7)**: Depends on the service tasks it instruments (T010, T022, T008/T018)
- **Polish (Phase 8)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Starts after Foundational — no dependencies on other stories
- **User Story 2 (P1)**: Starts after US1's T010 (shared supervisor file); otherwise independent
- **User Story 3 (P2)**: Integrates with US1/US2 components but is independently testable (durability scenarios)
- **User Story 4 (P2)**: Independent of planner stories after Foundational — parallelizable with US1/US2/US3
- **User Story 5 (P3)**: Thin logging layer over the other stories

### Within Each User Story

- Models/queries before services
- Services before endpoints/orchestration
- Core implementation before tests where tests depend on implementation shape (T005, T007, T009, T015 are written against their implementation tasks)
- Story complete before moving to next priority

### Parallel Opportunities

- T002 ∥ T003 ∥ T004 (different files, Setup/Foundational)
- T007 ∥ T009 once their implementations land
- T012 ∥ T013 (espx_client.go ∥ rclone.go) alongside T010–T011
- T015 ∥ T016–T018
- **US4 (T022–T026) runs fully in parallel with US1–US3** after Phase 2
- T027 ∥ T028 ∥ T029 (three different services)
- T030 ∥ T031

---

## Parallel Example: User Story 1

```bash
# After Phase 2 completes, launch together (different files):
Task: "Normalize stop/delete transitions + purge in internal/api/handler.go (T008)"
Task: "Add context.Context to EPG requests in internal/api/espx_client.go (T012)"
Task: "Make RcloneService.Cat context-aware in internal/storage/rclone.go (T013)"

# After T010 lands:
Task: "Transition mapping tests in internal/api/handler_lifecycle_test.go (T009)"
Task: "Supervisor state-machine tests in cmd/planner/reconcile_test.go (T015)"
```

---

## Parallel Example: User Story 4

```bash
# Independent of all planner stories — launch after Phase 2:
Task: "Shared predicate + CHANNEL_SYNC_INTERVAL in cmd/engines/main.go (T022)"
Task: "PurgeChannel in internal/engine/engine.go (T023)"
Task: "Key naming helpers in internal/engine/prep_version.go (T024)"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001–T002)
2. Complete Phase 2: Foundational (CRITICAL — blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: quickstart Scenarios A, D, G — stop latency ≤10s p95, purge complete, no resurrection
5. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. Add User Story 1 → test independently → deploy/demo (MVP!)
3. Add User Story 2 → test independently → deploy/demo (fixes the reported 5-minute delay)
4. Add User Story 3 → test independently → deploy/demo
5. Add User Story 4 → test independently → deploy/demo (engine parity)
6. Add User Story 5 → test independently → deploy/demo
7. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1 → US2 → US3 (shared planner/handler files, sequential)
   - Developer B: User Story 4 (engines, fully separate files)
3. Both merge before User Story 5 (logging touches all three services)
4. Final quickstart validation runs once against the integrated result

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story is independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same-file conflicts (T008/T018/T019/T029 all touch handler.go — keep sequential), cross-story dependencies that break independence
