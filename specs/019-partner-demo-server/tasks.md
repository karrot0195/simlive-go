# Tasks: Partner Demo Server

**Input**: Design documents from `/specs/019-partner-demo-server/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Tests are OPTIONAL - not explicitly requested in the feature specification.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Single project**: `cmd/`, `internal/`, etc. at repository root

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [x] T001 Create `cmd/partner/` directory structure
- [x] T002 Create demo data directory structure (e.g., `data/partner/` or as configured)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T003 Create basic HTTP server skeleton in `cmd/partner/main.go`
- [x] T004 Add configuration loading for simple partner config in `cmd/partner/main.go`

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Run a simple partner demo server (Priority: P1) 🎯 MVP

**Goal**: Start a lightweight partner demo server that serves minimal API surface via minimal fixed paths

**Independent Test**: Start the demo server and call its endpoints with sample data from files; verify it returns expected responses.

### Implementation for User Story 1

- [x] T005 [P] [US1] Implement minimal fixed-path endpoint for channel list in `cmd/partner/main.go` (load from files)
- [x] T006 [P] [US1] Implement minimal fixed-path endpoint for EPG/schedules in `cmd/partner/main.go` (load from files)
- [x] T007 [US1] Add file loading logic for demo data in `cmd/partner/main.go`

### Verification for User Story 1

- [x] T008 [US1] Verify demo server serves channels and EPG via minimal fixed paths using skill `start all` or manual test; expected: endpoints return valid sample data from files

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Simple auth support (Priority: P1)

**Goal**: Provide simple token-based auth flows (login/token issuance and verification)

**Independent Test**: Exercise auth endpoints and verify token issuance/verification behave as expected.

### Implementation for User Story 2

- [x] T009 [P] [US2] Implement simple token-based login endpoint in `cmd/partner/main.go`
- [x] T010 [P] [US2] Implement token verification endpoint in `cmd/partner/main.go`
- [x] T011 [US2] Add simple token storage/validation logic in `cmd/partner/main.go`

### Verification for User Story 2

- [x] T012 [US2] Verify simple token auth flow works end-to-end using skill `start all` or manual test; expected: login returns token, verify accepts valid token

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - HTTP download support for partner data (Priority: P1)

**Goal**: Serve partner communication data files over HTTP for use with `ClientDownload`

**Independent Test**: Configure a partner profile to use HTTP data provider pointing to the demo server; verify data is downloaded successfully.

### Implementation for User Story 3

- [x] T013 [P] [US3] Implement HTTP file serving endpoint for communication data in `cmd/partner/main.go`
- [x] T014 [US3] Add file serving logic with proper headers for data downloads

### Verification for User Story 3

- [x] T015 [US3] Verify HTTP data download works with `ClientDownload` pattern using skill `start all` or manual test; expected: files served correctly from disk

**Checkpoint**: All user stories should now be independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [x] T016 [P] Add simple configuration support from `partner.yaml` in `cmd/partner/main.go`
- [x] T017 Add example demo data files and update documentation
- [x] T018 Run quickstart.md validation

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - User stories can proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P1 → P1)
- **Polish (Final Phase)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) - Should be independently testable
- **User Story 3 (P1)**: Can start after Foundational (Phase 2) - Should be independently testable

### Within Each User Story

- Implementation tasks before verification
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks marked [P] can run in parallel
- All Foundational tasks marked [P] can run in parallel (within Phase 2)
- Once Foundational phase completes, all user stories can start in parallel
- Tasks marked [P] within a story can run in parallel

---

## Parallel Example: User Story 1

```bash
# Launch parallel implementation tasks for User Story 1:
Task: "T005 [P] [US1] Implement minimal fixed-path endpoint for channel list in cmd/partner/main.go"
Task: "T006 [P] [US1] Implement minimal fixed-path endpoint for EPG/schedules in cmd/partner/main.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo
4. Add User Story 3 → Test independently → Deploy/Demo
5. Each story adds value without breaking previous stories

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- All user stories are P1 priority
- Verification tasks use skills from `.specific/project.yaml` where applicable
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
