# Tasks: API-Level Timeline Monitor (SSE)

**Input**: Design documents from `/specs/008-api-timeline-monitor/`

**Prerequisites**: plan.md (required), spec.md (required for user stories)

**Tests**: Optional.

**Organization**: Tasks grouped by user story.

## Phase 1: Setup

(No setup tasks required.)

## Phase 2: Foundational

- [x] T001 Create `monitored_channels` model/migration in `internal/models` and DB.
- [x] T002 Add env `MONITOR_INTERVAL` parsing (default 3s) in API config.

## Phase 3: User Story 1 - Register channels to monitor (Priority: P1)

**Goal**: Watch/unwatch channels persisted.

**Independent Test**: Toggle watch; verify DB row.

- [x] T003 [US1] Add watch/unwatch handlers in `internal/api/handler.go`.
- [x] T004 [P] [US1] Register routes for watch/unwatch.
- [x] T005 [P] [US1] Add FE watch/unwatch button in `client/src/views/RunningChannelsTimeline.vue`.

## Phase 4: User Story 2 - Server pushes timeline updates via SSE (Priority: P1)

**Goal**: SSE stream to FE.

**Independent Test**: Connect EventSource; receive updates.

- [x] T006 [US2] Implement SSE endpoint in `internal/api/handler.go`.
- [x] T007 [US2] Implement monitor loop (`internal/api/monitor.go`) broadcasting to clients.
- [x] T008 [P] [US2] Update FE to consume SSE instead of polling.

## Phase 5: User Story 3 - Configurable interval (Priority: P2)

**Goal**: Interval configurable via env.

**Independent Test**: Change env; verify cadence.

- [x] T009 [US3] Use `MONITOR_INTERVAL` in monitor loop (default 3s).

## Final Phase: Polish & Cross-Cutting Concerns

- [x] T010 Ensure monitor is read-only (no engine/planner impact).
- [ ] T011 Update swagger/docs for new endpoints.

## Dependencies & Execution Order

- T001/T002 first.
- T003/T004 after T001.
- T006/T007 after T002.
- T005/T008 after backend endpoints.
- T009 after T007.
- T010/T011 last.