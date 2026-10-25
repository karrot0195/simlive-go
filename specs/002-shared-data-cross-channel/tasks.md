# Tasks: Shared Data Cross-Channel

## Phase 1: Setup
- [ ] T001 Read planner.go and segment_processor.go to confirm shared fields.

## Phase 2: Foundational
- [ ] T002 Move `isBlockingProcessing`, `processingSegments`, `adsId` fields from Planner struct to SegmentProcessor struct.

## Phase 3: User Story 1 - Multi-channel playout isolation (P1)
- [ ] T003 [US1] Update all references in segment_processor.go to use sp instead of sp.planner for the three fields.
- [ ] T004 [US1] Remove the three fields from Planner struct in planner.go.

## Phase 4: User Story 2 - Scoped planner state (P2)
- [ ] T005 [US2] Verify go build ./... passes.
- [ ] T006 [US2] Verify no other shared mutable buffers across channels.

## Final Phase: Polish
- [ ] T007 Verification against partner request: build + scoping inspection.