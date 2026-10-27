# Tasks: Improve Logging

**Input**: Design documents from `/specs/003-improve-logging/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, quickstart.md

**Tests**: Not explicitly requested; verification via build/test commands.

**Organization**: Tasks grouped by user story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2)

## Path Conventions

Single Go project: `internal/`, `cmd/` at repository root.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm baseline builds before changes.

- [X] T001 Run `make build` and `go test ./...` to establish a green baseline in the repository root.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: None blocking; proceed to user stories.

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel.

---

## Phase 3: User Story 1 - Use Native Logger Methods (Priority: P1) 🎯 MVP

**Goal**: Remove V* wrapper methods and migrate all call sites to native slog methods while preserving `[name]: ` prefix.

**Independent Test**: `grep` finds no V* references outside logging.go; `make build` and `make test` pass.

### Implementation for User Story 1

- [X] T002 [US1] Remove VInfo, VWarning, VError, VErrorWithError, VFatalf methods from `internal/utils/logging.go`; retain the `format` helper and expose a `Format(msg string, args ...interface{}) string` method for prefix preservation.
- [X] T003 [P] [US1] Migrate call sites in `cmd/planner/main.go` from V* methods to native `Info`/`Warn`/`Error` using `logger.Format(...)`.
- [X] T004 [P] [US1] Migrate call sites in `internal/engine/engine.go` from V* methods to native `Info`/`Warn`/`Error` using `logger.Format(...)`, reproducing any fatal-exit behavior via explicit `os.Exit(1)` where previously used.
- [X] T005 [US1] Verify no V* references remain outside `internal/utils/logging.go` via grep; run `make build` and `go test ./...`.

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently.

---

## Phase 4: User Story 2 - Surface rclone Download Timing and Provider Errors (Priority: P2)

**Goal**: Add elapsed-time reporting and verbose mode to rclone downloads, surfacing provider (B2) errors.

**Independent Test**: A failing CopyTo returns an error containing elapsed time and stderr; verbose mode includes provider detail.

### Implementation for User Story 2

- [X] T006 [US2] Add `verbose bool` field to `RcloneService` and update constructor to `NewRcloneService(verbose bool)` in `internal/storage/rclone.go`.
- [X] T007 [US2] Update `Cat` in `internal/storage/rclone.go` to measure elapsed time with `time.Since`, capture stderr, append `--verbose` when verbose, and include elapsed time in error messages.
- [X] T008 [US2] Update `CopyTo` in `internal/storage/rclone.go` to measure elapsed time with `time.Since`, capture stderr, append `--verbose` when verbose, and include elapsed time in error messages (and log on success).
- [X] T009 [US2] Update all `NewRcloneService()` call sites to pass the verbose flag (default false) across the codebase.
- [X] T010 [US2] Run `make build`, `go test ./...`, and `make vet`.

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently.

---

## Phase N: Polish & Cross-Cutting Concerns

**Purpose**: Final validation.

- [X] T011 [P] Run full quickstart validation from `specs/003-improve-logging/quickstart.md`.
- [X] T012 Update any documentation referencing V* logging methods or rclone verbose behavior.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately.
- **Foundational (Phase 2)**: None.
- **User Stories (Phase 3+)**: US1 and US2 are independent; can run in parallel.
- **Polish (Final Phase)**: Depends on all stories complete.

### Within Each User Story

- T002 before T003/T004 (logging API must change first).
- T006 before T007/T008.
- T009 after T006 (call sites updated to new constructor).

### Parallel Opportunities

- T003 and T004 can run in parallel (different files).
- T007 and T008 can run in parallel (different methods, same file — coordinate edits).
- US1 and US2 phases can run in parallel by different developers.

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup.
2. Complete Phase 3: User Story 1.
3. **STOP and VALIDATE**: Test User Story 1 independently.
4. Proceed to User Story 2.

### Incremental Delivery

1. Add User Story 1 → Test independently.
2. Add User Story 2 → Test independently.
3. Each story adds value without breaking previous stories.

---

## Notes

- [P] tasks = different files, no dependencies.
- [Story] label maps task to specific user story for traceability.
- Commit after each task or logical group.