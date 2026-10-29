# Tasks: Segment Re-download on Failure

**Input**: Design documents from `/specs/006-segment-redownload-on-failure/`

**Prerequisites**: plan.md (required), spec.md (required for user stories)

**Tests**: Optional; include only if requested.

**Organization**: Tasks grouped by user story.

## Phase 1: Setup

(No setup tasks required.)

## Phase 2: Foundational

(No foundational tasks required.)

## Phase 3: User Story 1 - Retry a failed segment download (Priority: P1)

**Goal**: Failed or undersized segments are not marked prepared and are retried.

**Independent Test**: Simulate a failed download or zero-byte file; verify segment is not appended and retried.

- [x] T001 [US1] In `internal/engine/engine.go`, update `prepareSegmentFromSource` to delete any existing stale/partial/undersized dest file before re-downloading, and only call `markPrepared` after download, PTS rewrite, and slice/PMT configuration all succeed.
- [x] T002 [P] [US1] In `internal/engine/engine.go`, update `generateRenditionPlaylist` to skip segments whose `.ts` file size is below 100KB in addition to missing `.pts` marker.
- [x] T003 [P] [US1] Ensure preparation failures are logged with channel and sequence identifiers (already via `logger.VError`/`logger.Error`).

## Phase 4: User Story 2 - Avoid marking prepared on partial failure (Priority: P2)

**Goal**: No prepared marker is created when any preparation step fails.

**Independent Test**: Force PTS rewrite or PMT failure; verify no marker and retry occurs.

- [x] T004 [US2] In `internal/engine/engine.go`, remove the `return e.markPrepared(marker)` call on the `rewritePtsOffset` error path so failures do not create a marker.
- [x] T005 [US2] In `internal/engine/engine.go`, ensure `applySliceEvents` failure returns before marker creation (marker created only after successful completion).

## Final Phase: Polish & Cross-Cutting Concerns

- [ ] T006 Add unit tests for `prepareSegmentFromSource` readiness and size-gate logic in `internal/engine/`.

## Dependencies & Execution Order

- T001 before T002/T004/T005.
- T004/T005 can run in parallel after T001.
- T006 after implementation tasks.