# Tasks: Running Channels Timeline

**Input**: Design documents from `/specs/004-running-channels-timeline/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Not explicitly requested; no test tasks generated.

**Organization**: Tasks grouped by user story (US1 view, US2 refresh) plus FE visualization.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm project layout (no new init needed).

- [x] T001 Verify Go module and client dependencies are installed (`go.mod`, `client/package.json`)

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared types and helpers required by all stories.

- [x] T002 Create `TimelineEntry`, `ChannelTimeline`, `TimelineResponse` structs in `internal/api/timeline_types.go`
- [x] T003 [P] Add unit-testable helper to filter entries with running_time >= now in `internal/api/timeline_types.go`

**Checkpoint**: Types ready.

## Phase 3: User Story 1 - View upcoming timeline for all running channels (Priority: P1) 🎯 MVP

**Goal**: API returns grouped upcoming timelines; UI renders them.

**Independent Test**: With one enabled channel with timeline data, open UI view and confirm entries render.

### Implementation for User Story 1

- [x] T004 [US1] Implement `GetRunningChannelsTimeline` handler in `internal/api/handler.go` (query active channels ordered by created_at DESC; per channel ZRangeByScore now..+inf; project to TimelineEntry; empty array not null)
- [x] T005 [US1] Register route `GET /api/channels/timeline/running` in `cmd/api/main.go`
- [x] T006 [P] [US1] Add API client method `getRunningChannelsTimeline` in `client/src/services/api.ts` (or equivalent)
- [x] T007 [US1] Create `RunningChannelsTimeline` view component in `client/src/views/RunningChannelsTimeline.vue`
- [x] T008 [US1] Add route/menu entry for the new view in `client/src/router/index.ts` (or equivalent)

**Checkpoint**: US1 independently functional.

## Phase 4: User Story 2 - Refresh timeline on demand (Priority: P2)

**Goal**: Manual refresh re-requests API and updates view.

**Independent Test**: Change data, click Refresh, view updates.

### Implementation for User Story 2

- [x] T009 [US2] Add Refresh button and reload logic in `client/src/views/RunningChannelsTimeline.vue`

**Checkpoint**: US2 functional.

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: FE visualization and gap warnings.

- [x] T010 [P] Implement timeline/graph visualization component in `client/src/components/TimelineGraph.vue`
- [x] T011 Integrate TimelineGraph into `RunningChannelsTimeline.vue`
- [x] T012 [P] Implement gap detection (>10s) and warning display in `client/src/components/TimelineGraph.vue`
- [x] T013 Update `client/src/views/RunningChannelsTimeline.vue` to show empty state and error/retry
- [x] T014 Run quickstart.md validation

## Dependencies & Execution Order

- Phase 1 → Phase 2 → Phase 3 (US1) → Phase 4 (US2) → Phase 5 (Polish)
- T006 depends on T005 (route/contract known); T007 depends on T006.

## Implementation Strategy

### MVP First (User Story 1 Only)
Complete Phases 1-3; validate endpoint and basic UI.

### Incremental Delivery
Add refresh (US2), then visualization + gap warnings (Polish).