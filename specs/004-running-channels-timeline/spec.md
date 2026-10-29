# Feature Specification: Running Channels Timeline

**Feature Branch**: `004-running-channels-timeline`

**Created**: 2026-10-28

**Status**: Implemented

**Input**: User description: "please a UI -> API to get timeline all running channels (playout.isEnable = true) from now -> end"

## Clarifications

### Session 2026-10-29

- Q: Should a channel that has no upcoming timeline entries be included in the API response with an empty entries array, or omitted entirely? → A: Included with an empty entries array (not omitted).
- Q: If Redis is unavailable for one channel but available for others, should the API fail the entire request with an error, or return partial results for the successful channels along with a per-channel error indicator? → A: Fail the entire request with an error response.
- Q: In what order should channels appear in the API response array? → A: By channel creation time descending (newest first), matching ListChannels default.
- Q: Should the API return the full SegmentItem (all fields as stored in Redis) for each timeline entry, or a trimmed projection of display-relevant fields? → A: Trimmed projection of display-relevant fields (sequence, running_time, duration, time_text, is_ad, ad-break info); internal PTS/media fields excluded.
- Q: Should the API expose which rendition segment files are prepared on disk so the UI can show readiness and enable playback? → A: Yes, `GET /api/channels/{id}/prepared` scans TMP_DIR and returns per-rendition segment file lists; UI polls on a configurable interval (default 2s, persisted).
- Q: Should the UI support playing a prepared segment directly? → A: Yes, clicking a prepared segment opens a player using the 360p segment URL.
- Note: UI must render timeline as a graph/timeline view and flag gaps >10s between consecutive segments as warnings.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View upcoming timeline for all running channels (Priority: P1)

An operator opens the admin UI and selects the "Running Channels Timeline" view. The UI calls the API, which returns the timeline entries for every channel whose playout is enabled, starting from the current time and extending to the end of the available timeline. The operator can see, per channel, the upcoming programs/ad breaks and their scheduled times without inspecting Redis via CLI tools.

**Why this priority**: This is the core value of the feature — a single consolidated view of what every enabled channel will play from now onward.

**Independent Test**: With at least one enabled channel that has a populated timeline in Redis, open the UI view and confirm it renders entries for that channel starting at or after the current time.

**Acceptance Scenarios**:

1. **Given** one or more channels with playout enabled and populated timelines, **When** the operator opens the Running Channels Timeline view, **Then** the UI displays timeline entries for each enabled channel starting from the current time to the end of the timeline.
2. **Given** a channel whose playout is disabled, **When** the timeline view is loaded, **Then** that channel's timeline is not included in the response.
3. **Given** a channel with no timeline data in Redis, **When** the timeline view is loaded, **Then** the channel is shown with an empty upcoming entries array (not omitted), and no error is shown to the operator.

---

### User Story 2 - Refresh timeline on demand (Priority: P2)

The operator can manually refresh the view to pull the latest timeline state from the API after planner updates.

**Why this priority**: Timelines change as the planner rewrites them; a manual refresh keeps the operator's view current without a page reload.

**Independent Test**: Change timeline data (via planner or test fixture) and trigger refresh; the view updates to reflect new entries.

**Acceptance Scenarios**:

1. **Given** the timeline view is already loaded, **When** the operator clicks Refresh, **Then** the UI re-requests the API and updates the displayed entries.

---

### Edge Cases

- What happens when no channels are enabled? The API returns an empty list and the UI shows an empty state.
- What happens when Redis is unavailable? The API returns an error and the UI shows a retry option.
- What happens when a channel's timeline contains entries only in the past? The channel is returned with an empty upcoming entries array (not omitted), and no past entries leak into the response.
- What happens when the current time falls between two timeline entries? Entries with running_time >= now are included.
- What happens when two consecutive entries have a gap greater than 10 seconds? The UI marks that gap as a warning.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST expose an API endpoint that returns timeline entries for all channels whose playout is enabled (is_active = true).
- **FR-002**: The API MUST filter timeline entries to those with running_time >= current time (from now to end), excluding past entries.
- **FR-003**: The API MUST read timeline data from the per-channel Redis sorted set (key pattern `simlive:timeline:{channel_id}`).
- **FR-004**: The API MUST return entries grouped by channel, including channel identity (id and name) alongside its filtered timeline entries.
- **FR-009**: The API MUST return channels ordered by creation time descending (newest first).
- **FR-010**: The API MUST return a trimmed projection per timeline entry containing display-relevant fields (sequence, running_time, duration, time_text, is_ad, and ad-break info); internal PTS/media fields are excluded.
- **FR-011**: The API MUST return `running_time` as a Unix timestamp (float), as stored in Redis.
- **FR-012**: The UI MUST render each channel's upcoming timeline as a visual timeline/graph so gaps and overlaps are easy to see.
- **FR-013**: The system MUST detect any gap greater than 10 seconds between two consecutive timeline entries (by running_time + duration vs next running_time) and mark it as a warning in the UI.
- **FR-005**: The system MUST provide a UI view that calls this API and renders the per-channel upcoming timeline entries.
- **FR-006**: The UI MUST display an empty state when no enabled channels or no upcoming entries exist.
- **FR-007**: The API MUST handle Redis or DB errors gracefully by returning an appropriate error response without crashing the service.
- **FR-014**: The system MUST expose `GET /api/channels/{id}/prepared` that scans TMP_DIR and returns per-rendition prepared segment file lists.
- **FR-015**: The UI MUST poll prepared segments on a configurable interval (default 2000ms) persisted in localStorage.
- **FR-016**: The UI MUST mark each segment READY with its prepared renditions and allow playback only when prepared.
- **FR-017**: The UI MUST display VOD badge for non-ad segments and AD badge for ad segments with consistent badge styling.
- **FR-018**: The API server MUST serve SPA routes (e.g. /timeline) via index.html fallback.

### Key Entities *(include if feature involves data)*

- **Running Channel**: A playout channel with is_active = true, identified by id and name.
- **Timeline Entry**: A scheduled segment entry with sequence, running_time, duration, media timing, and ad markers, as stored in Redis.
- **Timeline Response**: A collection of per-channel groups, each containing channel identity and its filtered list of upcoming timeline entries.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can view upcoming timeline entries for all enabled channels in a single UI interaction.
- **SC-002**: The API response contains no entries with running_time in the past relative to request time.
- **SC-003**: Disabled channels never appear in the timeline response.
- **SC-004**: The UI renders the timeline view without errors when at least one enabled channel has data.

## Assumptions

- "playout.isEnable = true" maps to the existing `is_active` column on the playout_channels table (channels started via the Start operation).
- Timeline data is already populated in Redis by the planner; this feature only reads and filters it.
- "from now -> end" means entries whose running_time is greater than or equal to the current server time; the server clock is the reference.
- The UI is the existing Vue 3 admin client; the new view integrates with the existing API client layer.
- Pagination is out of scope for v1; all upcoming entries for enabled channels are returned in one response.