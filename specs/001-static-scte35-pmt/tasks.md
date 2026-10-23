# Tasks: Static SCTE-35 PMT

**Input**: Design documents from `specs/001-static-scte35-pmt/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: No explicit test tasks — the spec does not request TDD. Validation scenarios are documented in `quickstart.md`.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2)
- Include exact file paths in descriptions

## Path Conventions

- **Single Go project**: `internal/` at repository root
- Paths shown below reflect the actual project structure from plan.md

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

No setup tasks required — the project already exists with all dependencies (TSDuck `tsp`, ffmpeg, rclone, Redis, Postgres). No new dependencies are introduced.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

No foundational tasks required — the change is a small addition to the existing segment preparation pipeline. No new models, database migrations, or infrastructure changes are needed.

---

## Phase 3: User Story 1 - Consistent PMT Across Segments (Priority: P1) 🎯 MVP

**Goal**: Ensure the SCTE-35 elementary stream (stream_type 0x86) is declared in the PMT for every segment, regardless of whether that segment contains SCTE-35 packets. The PMT content must be identical across all segments of the same stream.

**Independent Test**: Parse the PMT section from two consecutive segments — one containing SCTE-35 packets and one without — and verify the PMT payload (excluding version_number) is byte-for-byte identical. See `quickstart.md` Scenario 1 and 2 for detailed steps.

### Implementation for User Story 1

- [ ] T001 [P] [US1] Add `ConfigurePmtForVodTs` function in `internal/utils/tsduck.go` that runs `tsp -a 1/10 -I file "<input>" -P pmt --add-pid 0x1F00/0x86 --add-registration 0x43554549 -O file "<output>"` to configure the PMT for VOD segments without splice events. Mirror the existing `ConfigurePmtForAdsTs` function (lines 172-189) — same structure, same error handling, same 5-second timeout. The function signature must be: `func ConfigurePmtForVodTs(parentCtx context.Context, seg models.SegmentItem, inputFile string, outputFile string) error`

- [ ] T002 [P] [US1] Normalize SCTE-35 PID to `0x1F00` in `internal/utils/tsduck.go` by changing the `SCTE35_PID` constant from `"0x30"` to `"0x1F00"` (line 16). This ensures `InjectSpliceEvent` uses the same PID as `ConfigurePmtForAdsTs` and the new `ConfigurePmtForVodTs`, making PMT content identical across all segments per FR-002. **Risk**: Downstream systems expecting SCTE-35 on PID `0x30` will need to update to `0x1F00`.

- [ ] T003 [US1] Modify `applySliceEvents` in `internal/engine/engine.go` (lines 473-516) to add an `else` branch that calls `ConfigurePmtForVodTs` for VOD segments that have no splice events and are not ads. The decision logic should be:
  ```
  if segment has splice events:
      for each event: InjectSpliceEvent()  // existing
  else if segment is ad:
      ConfigurePmtForAdsTs()  // existing
  else:
      ConfigurePmtForVodTs()  // NEW — covers plain VOD segments
  ```
  Use a `.pmt.tmp` temp file suffix and `os.Rename` on success (matching the existing pattern in `applySliceEvents`). Clean up the temp file on failure.

- [ ] T004 [US1] Add retry logic and structured logging for PMT configuration failures in `internal/engine/engine.go`. On `ConfigurePmtForVodTs` failure: retry once, then if still fails, log the error using `logger.VError` with channelID and segment sequence (matching existing patterns at lines 482-491), clean up temp file, and return the error so the segment is skipped upstream per FR-006.

**Checkpoint**: At this point, User Story 1 should be fully functional. All segments should have SCTE-35 declared in their PMT with identical content. Run quickstart.md Scenarios 1 and 2 to validate.

---

## Phase 4: User Story 2 - PMT Version Number Compliance (Priority: P2)

**Goal**: When PMT content changes for any legitimate reason, the version_number field must increment by 1 modulo 32 per ISO/IEC 13818-1 §2.4.4.9.

**Independent Test**: Introduce a deliberate PMT content change (e.g., adding or removing an elementary stream) and verify that the version_number increments by exactly 1 modulo 32 from the previous value. See `quickstart.md` Scenario 3 for detailed steps.

### Implementation for User Story 2

- [ ] T005 [US2] Verify that `tsp -P pmt --add-pid` handles version_number correctly. Run a manual test: take a segment, run `tsp -P pmt --add-pid 0x1F00/0x86` twice, and inspect the PMT version_number before and after using `tsp -I file "<segment>" -P psi --pid 0x10`. Confirm that:
  1. Adding the same PID twice is idempotent (version_number unchanged)
  2. Adding a different PID increments version_number by 1
  3. The version_number wraps correctly at 31 → 0
  Document findings in `internal/utils/tsduck.go` as a comment above `SCTE35_PID`.

- [ ] T006 [US2] If `tsp` does NOT automatically increment version_number on PMT content change, implement a post-configuration version_number fix in `internal/utils/tsduck.go` or `internal/engine/engine.go`. Use `tsp -P pmt --set-version <N>` or equivalent to ensure the version_number is correct after any PMT modification. If `tsp` already handles this correctly (expected), add a defensive comment and move on.

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently. Run quickstart.md Scenarios 1-3 to validate.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T007 Run all quickstart.md validation scenarios from `specs/001-static-scte35-pmt/quickstart.md`:
  - Scenario 1: PMT contains SCTE-35 on VOD segments without splice events
  - Scenario 2: PMT is identical across consecutive segments
  - Scenario 3: PMT version_number increments on content change
  - Scenario 4: PMT configuration failure is logged and retried
  - Scenario 5: Full integration — partner demuxer does not reset
  Document results and any failures.

- [ ] T008 Update `internal/utils/tsduck.go` comments to document the PID normalization decision (why `0x1F00` was chosen over `0x30`) and the idempotency guarantee. Reference `specs/001-static-scte35-pmt/research.md` Decision 2 for rationale.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — no tasks required
- **Foundational (Phase 2)**: No dependencies — no tasks required
- **User Story 1 (Phase 3)**: Can start immediately — no blocking prerequisites
- **User Story 2 (Phase 4)**: Depends on User Story 1 completion (version_number compliance builds on the PMT configuration from US1)
- **Polish (Phase 5)**: Depends on both user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: No dependencies on other stories — can be implemented first
- **User Story 2 (P2)**: Depends on US1 — version_number compliance requires the PMT configuration to be in place first

### Within Each User Story

- T001 and T002 can run in parallel (different concerns in the same file, but T002 is a simple constant change)
- T003 depends on T001 (needs `ConfigurePmtForVodTs` to exist)
- T004 depends on T003 (retry/logic wraps the call added in T003)

### Parallel Opportunities

- T001 and T002 can be implemented in parallel (different functions, same file but no conflicts)
- T003 and T004 must be sequential (T003 adds the call, T004 wraps it with retry/logging)

---

## Parallel Example: User Story 1

```bash
# Launch T001 and T002 together (independent changes to tsduck.go):
Task: "Add ConfigurePmtForVodTs function in internal/utils/tsduck.go"
Task: "Normalize SCTE35_PID constant in internal/utils/tsduck.go"

# Then T003 and T004 sequentially:
Task: "Modify applySliceEvents in internal/engine/engine.go"
Task: "Add retry logic and logging in internal/engine/engine.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 3: User Story 1 (T001-T004)
2. **STOP and VALIDATE**: Run quickstart.md Scenarios 1 and 2
3. Deploy/demo if ready — User Story 1 alone fixes the partner's core issue

### Incremental Delivery

1. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
2. Add User Story 2 → Test independently → Deploy/Demo
3. Each story adds value without breaking previous stories

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
- The `SCTE35_PID` constant change (T002) is marked [P] but touches the same file as T001 — they are independent functions so no conflict