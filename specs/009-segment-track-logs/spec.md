# Feature Specification: Persistent Segment Track Logs

**Feature Branch**: `009-segment-track-logs`

**Created**: 2026-10-29

**Status**: Draft

**Input**: User description: "For monitor part (current spec) -> extend to keep segment track persistent in logs to check later if needed. Logs keep max 1000 items per channel; older items deleted. FE has a button on timeline screen per channel to view logs."

## Clarifications

### Session 2026-10-29

- Q: Should segment track entries be stored in Postgres, Redis, or a log file? → A: Append-only log file per channel.

### Session 2026-10-29 (improvement)
- Q: After calling DELETE track API, no further segments are logged because the in-memory seen set is kept. Should Clear also reset the seen set? → A: Yes. Clearing the track resets the per-channel seen set so currently-prepared and future segments are re-logged (refill accepted), restoring audit usefulness.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Persist segment track (Priority: P1)

Segment preparation/download events are persisted per channel for later inspection.

**Why this priority**: Operators need audit/history of segment handling.

**Independent Test**: Trigger segment processing; verify log entries persisted.

**Acceptance Scenarios**:

1. **Given** a segment is prepared, **When** processing completes, **Then** a track entry is persisted for that channel.
2. **Given** more than 1000 entries for a channel, **When** a new entry is added, **Then** the oldest is removed (max 1000).

### User Story 2 - View segment track from UI (Priority: P1)

Operator can view a channel's segment track via a button on the timeline screen.

**Why this priority**: Makes logs accessible without DB access.

**Independent Test**: Click button; view list of recent segment tracks.

**Acceptance Scenarios**:

1. **Given** a channel with track entries, **When** operator clicks the log button, **Then** a dialog/list shows entries.
2. **Given** no entries, **When** clicked, **Then** an empty state is shown.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST persist segment track entries per channel in an append-only log file (channel_id, sequence, status, timestamp, optional message).
- **FR-002**: System MUST retain at most 1000 entries per channel, deleting oldest when exceeded.
- **FR-003**: System MUST expose an API to fetch a channel's segment track (last N).
- **FR-004**: Frontend MUST provide a button on the timeline screen per channel to view its track.
- **FR-005**: Monitor/track logging MUST NOT impact engine or planner behavior.
- **FR-006**: Clearing a channel's track (DELETE) MUST also reset that channel's in-memory seen set so currently-prepared and future segments are re-logged.

### Key Entities *(include if feature involves data)*

- **Segment track entry**: channel_id, sequence, status, timestamp, message.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: At most 1000 track entries per channel at any time.
- **SC-002**: Operators can view track entries from the UI.
- **SC-003**: No impact on engine/planner performance or behavior.

## Assumptions

- Track entries stored in append-only log file per channel (e.g. under TMPDir/logs/<channel>.jsonl).
- Retention cap of 1000 is fixed (not configurable).
- Captured requests (T021-T025): track log is an audit of prepared segments and may legitimately diverge from the current timeline window; correctness improvements around staleness, recorder unification, run-reset, and real status values are tracked as follow-ups.