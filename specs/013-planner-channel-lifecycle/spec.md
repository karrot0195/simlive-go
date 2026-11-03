# Feature Specification: Planner & Engine Channel Lifecycle Reactivity

**Feature Branch**: `013-planner-channel-lifecycle`

**Created**: 2026-11-03

**Status**: Draft

**Input**: User description: "improve stable for planner script. Actually: if DB has 2 channels -> start planner -> start generate data for both. if stop channel A -> remove on DB -> add back. Take long time for planner trigger it (~5m). need improve it. Expect: channel change to inactive or remove from DB -> stop all process related to it. when add channel back or change status from inactive to LIVE (need verify) -> planner should start generate data for it as soon as possible. -> Should verify on engines script to see the same issue happen?"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Stop all work for a deactivated or removed channel (Priority: P1)

As an operator, when I stop or remove a channel, I want every part of the platform to stop working on that channel immediately, so that no stale stream, wasted processing, or ghost output survives the operation.

**Why this priority**: This is the core correctness and resource-safety issue. A channel that keeps preparing, downloading, or serving after being stopped is a visible defect and consumes shared capacity that active channels need.

**Independent Test**: Start with two active channels. Stop channel A and confirm that all preparation and playout work for A ceases within the target latency, while channel B continues without interruption.

**Acceptance Scenarios**:

1. **Given** two channels are generating output, **When** the operator deactivates channel A, **Then** all preparation and playout work for A stops within the target latency and A produces no further output.
2. **Given** channel A is active, **When** channel A is removed from the channel roster, **Then** all work for A stops within the target latency.
3. **Given** channel A is in the middle of preparation, **When** the operator deactivates A, **Then** the in-progress preparation is abandoned and cannot cause A to become active again.
4. **Given** channel A is deactivated and channel B remains active, **When** the normal operating cycle continues, **Then** channel B's output is not interrupted.

---

### User Story 2 - Start preparation promptly when a channel is activated (Priority: P1)

As an operator, when I add a channel back or move it back into an active state, I want preparation to begin almost immediately, so that I do not wait minutes for the channel to appear.

**Why this priority**: The reported defect is a roughly five-minute activation delay. Predictable, fast activation is required for routine operations such as stop/start maintenance and rapid channel changes.

**Independent Test**: With a channel in the inactive state, reactivate it and confirm preparation begins within the target latency, including reactivation through both supported paths (new/re-added channel and state change from inactive to active).

**Acceptance Scenarios**:

1. **Given** a channel roster with no channel X, **When** channel X is added, **Then** preparation for X begins within the target latency.
2. **Given** channel X is inactive, **When** the operator changes its state to active (LIVE or preparation state), **Then** preparation for X begins within the target latency.
3. **Given** other channels are busy preparing or playing, **When** channel X is activated, **Then** preparation for X still begins within the target latency instead of waiting for the other channels' work to finish.
4. **Given** channel X is stopped and started repeatedly, **When** each activation occurs, **Then** preparation begins each time and the platform never runs two concurrent work units for the same channel.

---

### User Story 3 - One consistent active/inactive definition across the platform (Priority: P2)

As an operator, I want the platform to agree on what "active" means, so that the channel list, the preparation service, and the playout service never disagree about whether a channel is running.

**Why this priority**: Conflicting signals (a channel marked stopped while it is still streaming, or automatically reactivated after a stop) make every other fix unreliable.

**Independent Test**: Deactivate a channel through each supported path and verify that every service reports and behaves as inactive within the target latency, and that nothing turns the channel back on without an operator action.

**Acceptance Scenarios**:

1. **Given** an operator deactivates a channel, **When** the current work for that channel finishes or is aborted, **Then** no component changes the channel back to an active state.
2. **Given** a channel is deactivated, **When** an operator queries its state, **Then** every service reports it as inactive and no output is produced.
3. **Given** a channel is automatically disabled after repeated preparation failures, **Then** it remains stopped until an operator explicitly reactivates it, with no repeating stop/start churn.
4. **Given** a channel is reactivated, **When** all services observe the change, **Then** they converge on the active state within the target latency.

---

### User Story 4 - Playout service parity (Priority: P2)

As an operator, I want the playout service to react to channel lifecycle changes as quickly and consistently as the preparation service, so that stopping or starting a channel has one predictable effect.

**Why this priority**: The request explicitly asks whether the same issue exists in the playout service. Behavior that is correct in one service but stale in the other would leave ghost streams running.

**Independent Test**: Stop and restart a channel and verify that the playout service stops serving that channel's stream within the target latency and resumes within the target latency after reactivation.

**Acceptance Scenarios**:

1. **Given** a channel is playing, **When** the channel is deactivated or removed, **Then** its stream stops and its playback resources are released within the target latency.
2. **Given** a channel is inactive, **When** it is reactivated and preparation succeeds, **Then** playout resumes within the target latency without requiring a service restart.
3. **Given** a channel is deactivated because of failures rather than an explicit stop, **Then** playout also stops and does not continue serving a channel the platform considers inactive.

---

### User Story 5 - Lifecycle transparency (Priority: P3)

As an operator, I want every start and stop decision to be explainable from logs, so that I can diagnose why a channel did not start or did not stop.

**Why this priority**: Without this, latency regressions and state disagreements are hard to detect during operations, but it is not required for the core fix.

**Independent Test**: Perform a stop and a start, then verify the logs identify the channel, the transition, the reason, and the timing for each decision.

**Acceptance Scenarios**:

1. **Given** a channel is activated or deactivated, **When** the operator reviews the logs, **Then** the lifecycle decision is recorded with the channel identifier, previous state, new state, reason, and timestamp.
2. **Given** a channel does not start within the expected time, **When** the operator reviews the logs, **Then** the blocking or failing step is identifiable.

### Edge Cases

- A channel is deactivated and then reactivated before the previous stop has fully completed.
- A channel is removed from the roster while it is being prepared.
- A channel's definition changes (for example, its timezone or program guide source) while it is active.
- A channel enters a failure state at the same time an operator stops it.
- Multiple lifecycle changes arrive for the same channel within one observation window; the final operator-chosen state must win.
- The platform restarts while channels are in mixed states, and must converge on the current roster without producing output for inactive channels.
- A reactivated channel's source content is unavailable, so preparation fails; retries must stay bounded and must not create a tight restart loop.
- The same channel activation is requested more than once in quick succession.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The platform MUST treat a channel as inactive immediately when it is deactivated through any supported path: explicit stop, state change to inactive, removal from the channel roster, or automatic disable after repeated failures.
- **FR-002**: Upon deactivation or removal, the platform MUST cease all work for that channel within the target latency, including queued preparation, in-progress preparation, downloads, and playout.
- **FR-003**: In-progress work for a deactivated channel MUST NOT change the channel's state back to active.
- **FR-004**: A deactivation MUST remain in effect until an operator explicitly reactivates the channel; no automatic process may revive it.
- **FR-005**: Upon activation (a new or re-added channel, or a state change from inactive to an active state such as LIVE or preparation), the platform MUST begin preparation for that channel within the target latency.
- **FR-006**: Activation of one channel MUST NOT be delayed by long-running work for other channels.
- **FR-007**: The preparation service and the playout service MUST apply the same active/inactive rules, so that both stop on deactivation and both resume on reactivation.
- **FR-008**: The platform MUST ensure at most one active work unit per channel at any time; duplicate activation requests MUST be harmless.
- **FR-009**: After reactivation, the platform MUST NOT serve output produced by the channel's previous session as if it were current.
- **FR-010**: Stopping, starting, or restarting one channel MUST NOT interrupt the output of unrelated active channels.
- **FR-011**: The platform MUST record every lifecycle decision with the channel identifier, previous state, new state, reason, and timestamp so it is visible to operators.
- **FR-012**: Automatic failure-driven deactivation MUST NOT produce repeating stop/start churn for the same channel.
- **FR-013**: On startup, the platform MUST converge to the channel roster's current state within the target latency without waiting for a full periodic refresh cycle.
- **FR-014**: The target latency MUST be configurable platform-wide with a sensible default, without requiring a code change.

### Key Entities

- **Channel Lifecycle State**: Whether a channel is eligible for work (active) or not (inactive), including the operator-visible status that drives eligibility.
- **Lifecycle Transition**: A recorded change from one channel lifecycle state to another, with reason and timestamp.
- **Work Unit**: A single preparation or playout session for a channel; at most one may be active per channel.
- **Channel Roster**: The authoritative set of channels and their lifecycle states from which all services derive their behavior.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: After a channel is deactivated or removed, 95% of stop operations cease all work for that channel within 10 seconds, and 100% within 30 seconds, using default settings.
- **SC-002**: After a channel is activated, 95% of activations begin preparation within 10 seconds, and 100% within 30 seconds, using default settings.
- **SC-003**: Across 20 stop/start cycles, zero deactivated channels return to an active state without an operator action.
- **SC-004**: Across 20 lifecycle operations on one channel, zero interruptions occur to unrelated active channels.
- **SC-005**: After any stop or removal, zero orphaned preparation or playout work units remain for that channel within 60 seconds.
- **SC-006**: In 10 trials where a channel is activated while other channels are preparing, preparation begins within the target latency in 100% of trials.
- **SC-007**: An operator can determine from logs, within one minute of review, why a channel did or did not start.
- **SC-008**: Changing the configured latency target changes the observed stop and start latency without a code change or redeployment of new logic.

## Assumptions

- "As soon as possible" is interpreted as a configurable target latency with a default of 10 seconds for typical operations (95th percentile), because the request did not specify a number.
- A channel is active when its operator-visible state is LIVE or preparing, and it has not been deactivated; INACTIVE and FAILURE states are inactive until an operator reactivates the channel.
- Reactivation can occur either by re-adding the channel or by changing its state from inactive to LIVE or preparing; both paths are in scope and must behave the same.
- Stopping a channel cleans up its prepared output, and reactivation rebuilds it; reusing cached source content is allowed but not required.
- The existing automatic disable after repeated failures (currently five consecutive failures) remains in force and is considered a deactivation path.
- All services already read a shared channel roster and shared working state; no new external infrastructure is required by this feature.
- The observed roughly five-minute delay is treated as the defect regardless of its cause; the specification fixes the outcome, not the mechanism.
- Multi-node/cluster coordination, API contract changes visible to partners, and client UI redesign are out of scope.
