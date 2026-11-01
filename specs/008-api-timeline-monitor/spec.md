# Feature Specification: API-Level Timeline Monitor (SSE)

**Feature Branch**: `008-api-timeline-monitor`

**Created**: 2026-10-29

**Status**: Draft

**Input**: User description: "From specs/004-running-channels-timeline extends -> move checking logic to API level. Interval via TIME env. Monitored channels registered via FE watch/unwatch button. Server uses SSE to push updates to FE. FE no polling. IMPORTANT: do not impact engine/planner; monitor only."

## Clarifications

### Session 2026-10-29

- Q: Should the set of monitored channels be persisted in the database or kept in memory? → A: Persist in a `monitored_channels` table.
- Q: Should the monitor interval env var be named `TIME` or `MONITOR_INTERVAL`? → A: Use `MONITOR_INTERVAL` with a sensible default.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Register channels to monitor (Priority: P1)

An operator can watch/unwatch channels from the UI; only watched channels are monitored.

**Why this priority**: Limits monitoring to relevant channels.

**Independent Test**: Toggle watch on a channel; verify it appears in monitored set.

**Acceptance Scenarios**:

1. **Given** a channel not monitored, **When** operator clicks Watch, **Then** it becomes monitored.
2. **Given** a monitored channel, **When** operator clicks Unwatch, **Then** it is removed from monitoring.

### User Story 2 - Server pushes timeline updates via SSE (Priority: P1)

The frontend receives timeline updates over Server-Sent Events without polling.

**Why this priority**: Eliminates client polling and reduces load.

**Independent Test**: Open timeline screen; verify updates arrive via SSE.

**Acceptance Scenarios**:

1. **Given** monitored channels, **When** server interval elapses, **Then** an SSE event with updated timeline is pushed.
2. **Given** no monitored channels, **When** interval elapses, **Then** no timeline payload is pushed (or empty).

### User Story 3 - Configurable monitor interval (Priority: P2)

Monitor interval is configurable via env `TIME` (or similar) without code changes.

**Why this priority**: Allows tuning.

**Independent Test**: Change env interval; verify SSE cadence changes.

**Acceptance Scenarios**:

1. **Given** env `TIME=5s`, **When** server runs, **Then** updates occur every 5s.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST expose API endpoints to watch/unwatch channels for monitoring.
- **FR-002**: System MUST persist the set of monitored channels (per session or server-side).
- **FR-003**: System MUST run a monitor loop at an interval defined by env `MONITOR_INTERVAL` (default 3s).
- **FR-004**: System MUST push timeline updates to connected clients via Server-Sent Events.
- **FR-005**: Frontend MUST NOT poll; it consumes SSE events only.
- **FR-006**: Monitor logic MUST NOT modify engine or planner behavior.
- **FR-007**: Monitor logic MUST be read-only with respect to timelines (no writes to Redis timeline keys).
- **FR-008**: System MUST allow multiple concurrent SSE clients.

### Key Entities *(include if feature involves data)*

- **Monitored channel set**: Channel IDs registered for monitoring.
- **SSE client**: Connected frontend listener.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Frontend receives updates via SSE with no polling requests.
- **SC-002**: Watch/unwatch changes reflect in monitored set within one interval.
- **SC-003**: Monitor interval configurable via env without redeploy of code.
- **SC-004**: Engine and planner behavior unchanged (no regressions).

## Assumptions

- Env var name for interval is `MONITOR_INTERVAL` (default 3s).
- Monitored channel set persisted in `monitored_channels` table.
- SSE endpoint is unauthenticated or uses existing auth; security handled separately.