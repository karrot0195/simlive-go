# Feature Specification: Current Status Tracking

**Feature Branch**: `011-current-status-tracking`
**Created**: 2026-11-01
**Status**: Draft
**Input**: User description: "where are u now ?"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View Current Status of a Stream or Component (Priority: P1)

As an operator or admin, I want to query the current position/status of a running simulated-live stream or system component so that I can understand what is happening "right now" without inspecting raw logs.

**Why this priority**: Provides the foundational visibility needed for all other monitoring and debugging workflows.

**Independent Test**: Can be fully tested by issuing a status query for a known running stream and receiving a structured response describing its current position/state.

**Acceptance Scenarios**:

1. **Given** a running simulated-live stream, **When** an operator requests its current status, **Then** the system returns the stream's current position (e.g., current segment/time) and operational state.
2. **Given** a component that is not running or not found, **When** an operator requests its status, **Then** the system returns a clear "not found / not running" response rather than an error.

### User Story 2 - Real-Time Status Refresh (Priority: P2)

As an operator, I want the displayed status to refresh automatically so that I can track progress without manually re-querying.

**Why this priority**: Improves usability for live monitoring scenarios.

**Independent Test**: Can be tested by opening a status view and verifying it updates at a defined interval without manual action.

**Acceptance Scenarios**:

1. **Given** an active status view, **When** the refresh interval elapses, **Then** the displayed status updates to reflect the latest state.

### User Story 3 - Historical Status Context (Priority: P3)

As an operator, I want to see recent status history alongside the current status so that I can understand how the system arrived at its present state.

**Why this priority**: Useful for troubleshooting but not required for basic visibility.

**Independent Test**: Can be tested by requesting status with history enabled and verifying recent past states are included.

**Acceptance Scenarios**:

1. **Given** a running stream with prior state changes, **When** status is requested with history, **Then** the response includes the current state plus a bounded list of recent prior states.

### Edge Cases

- What happens when the queried stream/component has just started and has no prior state?
- How does the system handle a status request for an entity that exists but is in an error state?
- What happens when the status data source is temporarily unavailable?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide a way to query the current status/position of a running stream or component by identifier.
- **FR-002**: System MUST return a structured, human-readable status response including current position and operational state.
- **FR-003**: System MUST distinguish between "not found", "not running", and "error" states in its response.
- **FR-004**: System MUST support automatic refresh of status displays at a configurable interval.
- **FR-005**: System MUST optionally include a bounded history of recent states when requested.
- **FR-006**: System MUST handle status queries for non-existent identifiers gracefully without crashing.

### Key Entities

- **Status Report**: Represents the current state of a stream or component, including identifier, current position, operational state, and timestamp.
- **Status History Entry**: Represents a past state snapshot with timestamp and state value.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can obtain the current status of a running stream within 2 seconds of issuing a query.
- **SC-002**: 100% of status queries for non-existent identifiers return a clear "not found" response rather than an error.
- **SC-003**: Status refresh updates the displayed information within one refresh interval of a state change.
- **SC-004**: 90% of operators can successfully interpret the status response without additional documentation.

## Assumptions

- The feature scope is limited to status reporting/visibility; remediation or control actions are out of scope for v1.
- "Current position" refers to the stream's current playback/segment position within the simulated-live timeline. [NEEDS CLARIFICATION: exact definition of "position" (segment index vs. wall-clock time vs. media timestamp) not specified]
- The set of queryable entities (streams, engines, channels, etc.) is [NEEDS CLARIFICATION: which entity types should be status-queryable?]
- Authentication/authorization requirements for status queries follow existing admin/operator permissions. [NEEDS CLARIFICATION: should status be viewable by non-admin users?]
- Refresh interval defaults to a reasonable value (e.g., 5 seconds) and is configurable.

## Clarifications Needed

The following aspects of the request "where are u now ?" are ambiguous and significantly impact scope. Please confirm:

1. **Entity scope**: Which components should be status-queryable (streams, engines, channels, all of the above)?
2. **Position definition**: What does "current position" mean (segment index, wall-clock time, media PTS)?
3. **Access level**: Should status be visible to all users or only admins/operators?