# Feature Specification: 24/7 Playout EPG Cache Fallback

**Feature Branch**: `010-epg-cache-fallback`

**Created**: 2026-10-30

**Status**: Draft

**Input**: User description: "recovery plan for 24/7 — @internal/planner sometime append logic for next day return empty data. Should improve it. current day -> GET data -> keep on disk for caching. append data run for next day. if empty, reuse caching data (previous day) or current days. if existing data, update caching. expect: simlive will have data 24/7, even data provider API return empty due to problem or something like that."

## Clarifications

### Session 2026-10-30

- Q: Should fallback apply only to the next-day append, or to any day that needs planning? → A: Any day that needs planning, as long as the channel has retained schedule data (covers today after a restart and multi-day outages).
- Q: How old may a retained schedule be before fallback is refused? → A: No limit — retained schedules are reused indefinitely; the channel is expected to always have a fallback.
- Q: Where should fallback usage be surfaced to operators? → A: A label on the timeline UI per channel, so admins can tell the channel is running on stand-in content.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Keep the channel on air across the day boundary when the provider has no next-day schedule (Priority: P1)

A channel is playing its current day's schedule. Near the end of the day the planner asks the schedule provider for the next day, but the provider returns no items (a known occurrence, an outage, or a delayed publish). Instead of the channel going silent at midnight, the system keeps the channel on air by standing in the most recent known-good schedule for the uncovered day, time-shifted so it covers the full next day.

**Why this priority**: This is the entire point of the request — channels must not go dark because an upstream provider is late or broken. Without it, nothing else matters.

**Independent Test**: Run a channel through a simulated day boundary where the provider returns an empty response for the next day, and verify the channel produces a continuous, playable timeline with no gap after midnight.

**Acceptance Scenarios**:

1. **Given** a channel has a successfully planned current day and the provider returns an empty schedule for the next day, **When** the planner prepares the next day, **Then** the channel continues with a stand-in timeline covering the full next day and playback does not stop at midnight.
2. **Given** the current day was itself planned from a stand-in schedule, **When** the next day is prepared and the provider is still empty, **Then** the most recent real schedule (not the stand-in) is used as the source, and the channel remains on air.
3. **Given** the provider returns a valid next-day schedule, **When** the planner prepares the next day, **Then** the real schedule is used and no stand-in content is inserted for that day.

---

### User Story 2 - Retain and refresh the last known-good schedule on persistent local storage (Priority: P1)

Every time the planner successfully retrieves a non-empty schedule for a channel, it keeps that schedule on the server's local disk so it remains available after a planner restart and can be reused later. When a newer non-empty schedule is retrieved, it replaces the retained copy.

**Why this priority**: The fallback in User Story 1 is only possible if a known-good schedule survives on disk. This is the enabling capability and is independently testable.

**Independent Test**: Retrieve a schedule for a channel, restart the planner, then force an empty provider response and confirm the retained schedule is still available and used.

**Acceptance Scenarios**:

1. **Given** a channel's schedule is retrieved successfully, **When** the retrieval completes, **Then** that schedule is retained on local disk with enough information to identify the channel and the day it belongs to.
2. **Given** a retained schedule already exists for a channel, **When** a newer non-empty schedule is retrieved, **Then** the retained copy is replaced with the newer content.
3. **Given** the provider returns an empty response, **When** a retained schedule exists, **Then** the retained copy is NOT overwritten by the empty response.
4. **Given** the planner restarts, **When** it later needs a fallback, **Then** the retained schedule is still available.

---

### User Story 3 - Return to real data automatically when the provider recovers (Priority: P2)

When the provider starts returning valid schedules again, the channel switches from stand-in content back to real content for newly planned coverage, without operator intervention, and the retained schedule is updated.

**Why this priority**: Fallback must be a safety net, not a permanent mode. Automatic recovery keeps programming correct once upstream recovers.

**Independent Test**: Force a stand-in for a day, then make the provider return real data on the next preparation cycle and verify the subsequent coverage uses real data and the retained copy updates.

**Acceptance Scenarios**:

1. **Given** the channel is running on stand-in content for a day, **When** the provider returns a valid schedule for that day, **Then** the real schedule is used for coverage that has not yet been produced and the retained copy is updated.
2. **Given** the provider recovers, **When** the next preparation cycle runs, **Then** no operator action is required for the channel to return to real data.

---

### User Story 4 - Operators can tell when a channel is running on stand-in content (Priority: P3)

An operator can see, per channel, that the current coverage is coming from a fallback rather than the provider, and can identify which retained day was used.

**Why this priority**: Operational trust. Without visibility, an unnoticed fallback can mask a prolonged provider outage. Lower priority than keeping the channel on air.

**Independent Test**: Force a fallback and confirm the channel's observable state and logs indicate fallback usage, including the source day; confirm normal channels are not marked.

**Acceptance Scenarios**:

1. **Given** a channel's coverage was produced from a fallback, **When** an admin views the timeline, **Then** a fallback label is displayed on that channel and the source day is visible.
2. **Given** a channel is using real provider data, **When** an operator inspects it, **Then** it is not marked as using fallback.

---

### Edge Cases

- **No retained schedule exists** (first run, or cache wiped) and the provider returns empty: the channel cannot be recovered; the system must report the failure clearly rather than silently marking the day as prepared.
- **Provider response is empty but reported as success**: an empty schedule must be treated as "no data" for fallback purposes, not as a successfully planned day.
- **Partial schedule** (some items, but not covering the full day): treated as valid provider data; existing gap-filling behavior remains responsible for covering the remainder.
- **Provider returns an error** (timeout, non-success response, malformed body): treated the same as empty for fallback purposes.
- **Retained schedule is several days old** because the provider has been down for a long time: the retained schedule is still used (no staleness expiry); only a missing, unreadable, or identity-mismatched retained schedule prevents fallback.
- **Retained schedule cannot be read** (missing, unreadable, corrupt): treated as "no retained schedule"; must not crash the planner.
- **Multiple day boundaries pass during an outage**: each day must still be covered without gaps.
- **Fallback content repeats**: the same retained day may be reused for more than one day; sequence numbers and running time must remain continuous and strictly increasing.
- **Channel's provider schedule identity changes** (EPG reassigned): retained data for the old identity must not be applied to the channel.
- **Stand-in day is partially played already**: fallback must only add coverage that does not already exist in the timeline; it must never duplicate or rewind already-played segments.
- **Planner restarts mid-day while on fallback**: the channel must continue without a gap and without duplicating coverage.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST retain, per channel, the most recent non-empty schedule successfully retrieved from the provider, on persistent local storage that survives planner restarts.
- **FR-002**: The retained schedule MUST record at least the channel identity, the day it was retrieved for, and when it was retrieved, so identity and recency can be reported.
- **FR-003**: A successful non-empty provider retrieval MUST replace the channel's retained schedule.
- **FR-004**: An empty provider response, or a provider error, MUST NOT overwrite or delete the channel's retained schedule.
- **FR-005**: When the planner needs to plan any day for a channel (today or a future day) and the provider returns no usable data, the system MUST fall back to the channel's retained schedule, provided one exists for that channel.
- **FR-006**: Fallback content MUST be time-shifted so it covers the entire target day (from the day's start through the day's end), regardless of which day the retained schedule originally belonged to.
- **FR-007**: Fallback MUST only add coverage that does not already exist; it MUST NOT duplicate, rewind, or replace already-planned or already-played segments.
- **FR-008**: Timeline continuity MUST be preserved across fallback transitions: segment sequence numbers and running time MUST remain strictly increasing with no gaps.
- **FR-009**: Fallback MUST NOT be sourced from content that was itself generated by a fallback; the system MUST use the most recent schedule that came from the provider.
- **FR-010**: When the provider returns usable data for a day currently covered by fallback, the system MUST automatically use the real data for coverage not yet produced and update the retained schedule, without operator action.
- **FR-011**: When no retained schedule exists and the provider returns no usable data, the system MUST report the failure distinctly (rather than treating the day as successfully prepared) so operators can act.
- **FR-012**: Fallback MUST NOT cause the channel to be marked inactive or failed solely because the provider returned empty data, provided the fallback successfully produced coverage.
- **FR-013**: Fallback usage MUST be observable per channel, including the source day of the retained schedule used, and MUST be distinguishable from normal provider-sourced planning. The timeline UI MUST display a fallback label on affected channels so admins can see that the channel is running on stand-in content.
- **FR-014**: Day-boundary and timezone handling for retained schedules and target-day coverage MUST follow the project's existing local-timezone convention, with the configured timezone only as a fallback.
- **FR-015**: Retained schedules MUST be scoped to the channel's current provider schedule identity; retained data from a previous identity MUST NOT be used.
- **FR-016**: Retained schedules MUST NOT expire based on age; a channel with a retained schedule MUST always have it available for fallback.

### Key Entities *(include if data involved)*

- **Retained schedule**: The most recent non-empty provider schedule for a channel. Attributes: channel identity, provider schedule identity, original day, retrieval time, schedule items (programmes and their start/end times, programme metadata used for segment resolution).
- **Fallback coverage**: Timeline coverage for a target day that was produced from a retained schedule rather than a live provider response. Attributes: target day, source retained day, source retrieval time, resulting timeline range.
- **Channel fallback state**: Operator-visible indication that a channel's current coverage is fallback-sourced, with source day and time.
- **Planner preparation outcome**: The result of preparing a day — real data, fallback data, or unrecoverable — used to drive channel status and reporting.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For a channel with a retained schedule, 100% of tested day boundaries where the provider returns empty continue to produce a continuous, playable timeline with no gap at the boundary.
- **SC-002**: A retained schedule survives a service restart and is available for fallback in 100% of tested restarts.
- **SC-003**: In 100% of successful provider retrievals, the retained schedule content matches what the provider returned.
- **SC-004**: Empty or failed provider responses leave the retained schedule unchanged in 100% of tested cases.
- **SC-005**: When the provider recovers, real data is used for not-yet-produced coverage within one planning cycle, with zero operator actions required.
- **SC-006**: Across fallback transitions, 100% of produced segments have strictly increasing sequence numbers and no running-time gaps in the tested scenarios.
- **SC-007**: Fallback usage is identifiable for 100% of affected channels within one planning cycle, and no unaffected channel is incorrectly marked.
- **SC-008**: Channels with a retained schedule keep producing coverage through 30 consecutive days of simulated provider outage without manual intervention and without a staleness cutoff.

## Assumptions

- "Empty provider data" means zero schedule items for the requested day; provider errors (timeout, non-success response, undecodable body) are treated equivalently for fallback purposes.
- The existing end-of-day gap-filling behavior remains in place and is not replaced by this feature; this feature adds a whole-day fallback on top of it.
- Only one retained schedule per channel is needed (the most recent non-empty one); no history of multiple prior days is required for fallback selection.
- Retained schedules are stored on the server's persistent local disk (under the existing temporary/storage directory convention), not in the timeline store.
- Repeated fallback days reuse the most recent provider-sourced schedule; content repetition is acceptable and preferred over going dark.
- Segment resolution (programme → stream → rendition) is unchanged; fallback simply supplies different schedule items to the existing planning path.
- Planning cadence, retry counts, and lookahead windows retain their current defaults; no new tuning knobs are introduced.
- The timeline UI adds a fallback label per affected channel; no new screens are required.
- Retained schedules never expire by age; they are removed or replaced only by a newer provider schedule or a channel identity change.
- Channels that have never successfully retrieved a schedule are out of scope for recovery; they fail visibly as they do today.
