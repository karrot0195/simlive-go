# Tasks: Timeline UI Optimization

**Input**: Design documents from `/specs/014-timeline-ui-optimize/`

**Prerequisites**: plan.md (required), spec.md (required for user stories)

**Tests**: No automated tests requested; manual verification via quickstart.md.

**Organization**: Tasks grouped by user story to enable independent implementation.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)

## Path Conventions

- **Web app**: `client/src/views/RunningChannelsTimeline.vue`

---

## Phase 1: Setup

**Purpose**: No project scaffolding needed (existing component).

- [x] T001 Review existing component structure in client/src/views/RunningChannelsTimeline.vue

---

## Phase 2: Foundational

**Purpose**: Shared helpers/constants used by all user stories.

- [x] T002 Add `TIMELINE_PAGE_SIZE` constant (default 50) and `loadedCounts` ref keyed by channel_id in client/src/views/RunningChannelsTimeline.vue
- [x] T003 Add `dayKey(ts)` helper returning YYYY-MM-DD for a Unix timestamp in client/src/views/RunningChannelsTimeline.vue

**Checkpoint**: Foundation ready.

---

## Phase 3: User Story 1 - Show date (MM-dd) when timeline spans multiple days (Priority: P1) 🎯 MVP

**Goal**: Display MM-dd date components in summary and per-entry labels when range spans multiple days.

**Independent Test**: Load a timeline spanning midnight; summary and changed-day entries show MM-dd.

### Implementation for User Story 1

- [x] T004 [US1] Update `fmtTime` usage in `channelSummary` to include MM-dd for both endpoints when first and last entry fall on different calendar days in client/src/views/RunningChannelsTimeline.vue
- [x] T005 [US1] Add a `fmtEntryTime(e, prev)` helper that returns "MM-dd HH:MM:SS" when `e`'s calendar day differs from `prev`'s, else time-only, in client/src/views/RunningChannelsTimeline.vue
- [x] T006 [US1] Update entry time label rendering to use `fmtEntryTime` with previous entry in client/src/views/RunningChannelsTimeline.vue
- [x] T007 [US1] Ensure single-day ranges keep time-only format (no redundant date) in client/src/views/RunningChannelsTimeline.vue

**Checkpoint**: User Story 1 independently functional.

---

## Phase 4: User Story 2 - Lazy load / load more for large timelines (Priority: P1)

**Goal**: Render bounded pages with "Load more"; reset paging on refresh.

**Independent Test**: 500-entry channel renders 50 initially; "Load more" appends; hides when exhausted; SSE resets to 50.

### Implementation for User Story 2

- [x] T008 [US2] Compute `visibleEntries(ch)` returning `ch.entries.slice(0, loadedCounts[ch.channel_id] ?? TIMELINE_PAGE_SIZE)` in client/src/views/RunningChannelsTimeline.vue
- [x] T009 [US2] Update template timeline row to iterate `visibleEntries(ch)` instead of `ch.entries` in client/src/views/RunningChannelsTimeline.vue
- [x] T010 [US2] Add "Load more" button per channel shown when `loadedCount < ch.entries.length`, incrementing loadedCount by page size, in client/src/views/RunningChannelsTimeline.vue
- [x] T011 [US2] Reset `loadedCounts` to default page size for all channels in `load()` and SSE handler in client/src/views/RunningChannelsTimeline.vue
- [x] T012 [US2] Hide "Load more" when all entries loaded in client/src/views/RunningChannelsTimeline.vue

**Checkpoint**: User Story 2 independently functional.

---

## Phase 5: User Story 3 - Preserve gap detection and readiness across lazy-loaded entries (Priority: P2)

**Goal**: Gap flags and readiness badges remain correct for rendered and newly loaded entries.

**Independent Test**: Gap straddling page boundary flagged after "Load more"; badges update on SSE.

### Implementation for User Story 3

- [x] T013 [US3] Precompute a `gapStartSeqs(ch)` set over full `ch.entries` (gap >10s) in client/src/views/RunningChannelsTimeline.vue
- [x] T014 [US3] Update gap class binding to consult `gapStartSeqs` by sequence instead of index-based check on rendered subset in client/src/views/RunningChannelsTimeline.vue
- [x] T015 [US3] Ensure prepared/readiness badges recompute for rendered entries on SSE prepared updates in client/src/views/RunningChannelsTimeline.vue

**Checkpoint**: All user stories functional.

---

## Phase 6: Polish & Cross-Cutting

- [x] T016 Run client build (`cd client && pnpm build`) to verify no type errors
- [x] T017 Manual validation per quickstart.md

## Scope Sync (captured during implementation)

- [x] T018 Improve "Load more" UX/behavior: show loaded/total count, clearer button affordance, optional "Load all", preserve horizontal scroll position on append (captured request: "improve UX + behavior on UI side . For not not good when want to load more")

---

## Dependencies & Execution Order

- Phase 1 → Phase 2 → Phases 3-5 (US1/US2 share component; implement sequentially or carefully merge)
- US3 depends on US2's paging (gap boundary test)

## Notes

- Single file change; coordinate edits to avoid conflicts.
- No backend changes.