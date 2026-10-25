# Feature Specification: Shared Data Cross-Channel

**Feature Branch**: `bugfix/release-v1-implement-version-number`

**Created**: 2026-10-25

**Status**: Draft

**Input**: User description: "Shared data cross-channel: when running simlive for 2 channels, data of channel 2 leaks into channel 1, happening after ads injected finish and next video becomes wrong. Suspected shared mutable state."

## Clarifications

### Session 2026-10-25

- Q: Are there other shared mutable fields beyond planner causing cross-channel leak? → A: No; only planner held shared mutable state (processingSegments, isBlockingProcessing, adsId); engine state already keyed by channelID; utils has no globals.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Multi-channel playout isolation (Priority: P1)

As a partner running two simlive channels concurrently, I want each channel's playout to remain isolated so that channel 1 never shows channel 2's content, especially after ad breaks.

**Why this priority**: Core correctness; cross-channel contamination breaks broadcast.

**Independent Test**: Run two channels; inspect channel 1 playlist after an ad break on channel 2; verify only channel 1 segments present.

**Acceptance Scenarios**:

1. **Given** two channels running concurrently, **When** ads are injected and finish on channel 2, **Then** channel 1's next segments remain channel 1's own content.
2. **Given** planner processing two channels in the same tick, **When** ad breaks are inserted, **Then** splice events attach to the correct channel's segments only.

---

### User Story 2 - Scoped planner state (Priority: P2)

As a developer, I want per-channel mutable planner state (processingSegments, isBlockingProcessing, adsId) scoped to the SegmentProcessor instance so concurrent channels cannot overwrite each other.

**Why this priority**: Root cause fix.

**Independent Test**: Inspect code; run go build; verify fields only on SegmentProcessor.

**Acceptance Scenarios**:

1. **Given** Planner shared across channels, **When** two ProcessEPGToTimeline calls run concurrently, **Then** each uses its own processingSegments slice.

---

### Edge Cases

- What happens when a channel is deactivated mid-processing? CleanChannelData clears Redis; in-memory state is per-call so no leak.
- How does system handle ad injection at same media time across channels? Each has own adsId counter.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST scope `processingSegments`, `isBlockingProcessing`, and `adsId` to the SegmentProcessor instance, not the shared Planner.
- **FR-002**: System MUST keep engine mutable state (inFlight, downloadingChannels, rendMetaCache) keyed by channelID.
- **FR-003**: System MUST NOT use package-level mutable buffers shared across channels.
- **FR-004**: System MUST build successfully (`go build ./...`).

### Key Entities

- **SegmentProcessor**: per-channel/per-process planning state.
- **Planner**: shared read-only services (db, redis, rclone).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Two channels run concurrently without cross-channel segment leakage.
- **SC-002**: `go build ./...` exits 0.
- **SC-003**: Planner shared mutable fields removed.

## Assumptions

- Engine state already keyed by channelID; only planner shared state is the leak source.
- Concurrent processing uses worker pool; per-instance state avoids races.