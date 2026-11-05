# Tasks: Dynamic Partner Communication Data Downloader

**Input**: Design documents from `/specs/018-partner-data-downloader/`
**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: Tests are OPTIONAL - include where appropriate per TDD guidance.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and structure preparation

- [ ] T001 Create `internal/downloader/` package directory structure per implementation plan
- [ ] T002 [P] Review existing patterns in `internal/storage/rclone.go` and `internal/partner/config.go` for reuse

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core interfaces and config extensions that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [ ] T003 Define `Downloader` interface in `internal/downloader/downloader.go` with `Fetch(ctx context.Context, source string) ([]byte, error)`
- [ ] T004 Extend `internal/partner/config.go` with data provider configuration structures and validation (default `rclone`, unknown provider rejected, HTTP requires endpoint)
- [ ] T005 Add config validation logic in `internal/partner/config.go` `LoadConfigFile` to validate provider settings at load time
- [ ] T006 [P] Update `internal/partner/config_test.go` with tests for default provider, explicit providers, invalid provider, missing HTTP endpoint

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Choose a data provider per partner (Priority: P1) 🎯 MVP

**Goal**: Allow per-partner provider selection with default rclone; reject unknown providers.

**Independent Test**: Load partner profile with no provider specified → rclone selected. Load profile with HTTP provider → HTTP client selected. Load profile with unknown provider → error.

### Implementation for User Story 1

- [ ] T007 [P] [US1] Implement `RcloneDownload` in `internal/downloader/rclone.go` wrapping existing rclone usage (preserve behavior)
- [ ] T008 [P] [US1] Implement `ClientDownload` in `internal/downloader/http.go` reading endpoint/headers/timeout from config
- [ ] T009 [US1] Implement factory/resolver `New(p *partner.PartnerProfile) (Downloader, error)` in `internal/downloader/downloader.go` selecting provider based on profile
- [ ] T010 [US1] Add validation for HTTP endpoint presence when HTTP provider selected in config loading

### Verification for User Story 1

- [ ] T011 [US1] Verify provider selection behavior via skill `test`: run `make test` and ensure tests for default rclone, explicit HTTP selection, and unknown provider rejection pass

**Checkpoint**: User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Download partner data over HTTP (Priority: P1)

**Goal**: Support HTTP-based partners via ClientDownload with headers/timeout, proper error handling.

**Independent Test**: Point profile at HTTP endpoint serving payload → retrieves same bytes as rclone path. Non-2xx/timeout/unreachable → clear errors.

### Implementation for User Story 2

- [ ] T012 [P] [US2] Implement HTTP request logic in `internal/downloader/http.go` applying headers and timeout from profile
- [ ] T013 [US2] Add error handling for non-success status codes and network failures in `internal/downloader/http.go`
- [ ] T014 [US2] Ensure ClientDownload respects profile timeout and returns errors on timeout/unreachable
- [ ] T015 [US2] Add/update tests for HTTP download behavior (headers, timeout, errors)

### Verification for User Story 2

- [ ] T016 [US2] Verify HTTP download behavior via skill `test`: run `make test` and ensure HTTP provider tests pass

**Checkpoint**: User Stories 1 and 2 should both work independently

---

## Phase 5: User Story 3 - Unified Downloader contract (Priority: P2)

**Goal**: All data acquisition goes through Downloader interface; callers depend only on interface.

**Independent Test**: Use fake Downloader in tests; add new provider without changing callers.

### Implementation for User Story 3

- [ ] T017 [P] [US3] Ensure interface is generic and implementations are interchangeable
- [ ] T018 [US3] Update any existing call sites to use Downloader interface where appropriate (if they exist for "partner communication data" acquisition)
- [ ] T019 [US3] Add contract test with fake/test implementation to verify interchangeability

### Verification for User Story 3

- [ ] T020 [US3] Verify unified contract via skill `test`: run `make test` and ensure contract/interchangeability tests pass

**Checkpoint**: All user stories should now be independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T021 [P] Update `partner.example.yaml` to document data_provider configuration options
- [ ] T022 Run `make fmt` and `make vet` to ensure code quality
- [ ] T023 Run full test suite `make test` and fix any issues
- [ ] T024 Validate against quickstart scenarios

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Stories (Phase 3-5)**: All depend on Foundational completion
- **Polish (Phase 6)**: Depends on all user stories complete

### User Story Dependencies

- **US1 (P1)**: Can start after Foundational - No dependencies on other stories
- **US2 (P1)**: Can start after Foundational - Builds on US1 factory/implementations but independently testable
- **US3 (P2)**: Can start after Foundational - Depends on interface existing (from US1) but independently testable

### Parallel Opportunities

- T002 can run in parallel with T001
- T006 can run in parallel with other foundational tasks
- T007, T008 can run in parallel within US1
- T012 can run in parallel with other US2 tasks where possible

---

## Implementation Strategy

### MVP First (User Story 1 Only)
1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL)
3. Complete Phase 3: User Story 1
4. Validate independently
5. Deploy/demo if ready

### Incremental Delivery
1. Setup + Foundational → Foundation ready
2. Add US1 → Test independently → MVP
3. Add US2 → Test independently
4. Add US3 → Test independently
5. Polish
