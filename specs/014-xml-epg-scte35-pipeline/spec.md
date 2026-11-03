# Feature Specification: XML EPG Ad Pipeline

**Feature Branch**: `014-xml-epg-scte35-pipeline`

**Created**: 2026-11-03

**Status**: Draft

**Input**: User description: "Based on the investigation summary, follow the same logic as the main branch for how PMT, SCTE-35, pre-roll, cue-out, cue-in, and PTS are prepared. Change only how EPG item data is obtained from the API and replace default interval ads with ads declared in the XML. Detect ads as they approach and attach pre-roll, cue-out, and cue-in around them."

## Context

Spec 013 moved the planner to an EPG-driven ad model but also rewrote the media signaling path. The result diverged from the main branch's proven segment/splice behavior: ad break identity is derived from programme timestamps, pre-roll and cue-in placement changed, and PMT handling moved in a different direction. This feature restores the main branch's media pipeline behavior for PMT, SCTE-35, pre-roll, cue-out, cue-in, and PTS computation, while keeping the two intentional 013 changes:

1. EPG items come from the partner XML API.
2. Ads come only from ad markers declared in that XML — never from a fixed interval.

## Clarifications

### Session 2026-11-03

- Q: Should PMT preparation keep spec 001's static per-segment PMT or strictly copy main branch's ad-only PMT handling? → A: Keep the static PMT from spec 001. Every segment — content and ad — declares the same SCTE-35 elementary stream, PID, and registration; only the SCTE-35/pre-roll/cue/PTS flow follows the main branch.
- Q: Where is CUE-IN attached when a break ends? → A: On the first content segment after the break, matching the main branch behavior and correlating with that break's CUE-OUT.
- Q: How do several XML ad markers inside one programme map to breaks? → A: One break per marker. Each marker is anchored at its own declared start, uses its own declared duration and asset path, and emits its own pre-roll, cue-out, and cue-in boundaries.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Ads come only from the XML EPG (Priority: P1)

An operator schedules a channel whose XML EPG declares ad markers. The system plays those breaks at their declared times and never inserts an ad merely because a fixed interval elapsed.

**Why this priority**: This is the core behavior change. Unrequested interval ads break the partner's schedule and make ad reconciliation impossible.

**Independent Test**: Prepare an EPG day with one declared ad marker and one long content programme, then inspect the generated timeline: exactly one break appears, at the marker's declared start, with the marker's asset and duration. A day with no markers produces zero breaks no matter how long it runs.

**Acceptance Scenarios**:

1. **Given** an XML EPG item carrying an ad marker with a path, duration, and start, **When** the planner builds the timeline, **Then** the marker becomes one ad break anchored at that start and using that path and duration.
2. **Given** an XML EPG day with no ad markers, **When** the timeline is built, **Then** no segment is marked as an ad and no break is generated.
3. **Given** a channel that has been running for longer than any previously configured ad interval, **When** its timeline is inspected, **Then** no break appears unless the XML declared one.
4. **Given** a programme containing several ad markers, **When** the timeline is built, **Then** each marker produces its own independent break in the order declared.

---

### User Story 2 - Boundaries are attached as ads approach (Priority: P1)

A downstream partner receives pre-roll warnings before an ad break, a cue-out when the break starts, a pre-return warning before the break ends, and a cue-in when content resumes. All markers reference the same break identity.

**Why this priority**: Without correctly placed boundaries the partner cannot prepare its ad decision or return cleanly to content, so the break is unusable even if the ad segments exist.

**Independent Test**: Build a timeline containing one known break and inspect the SCTE-35 markers and HLS tags around it: two pre-roll markers at 8 s and 4 s before the break, one cue-out on the first ad segment, one pre-return marker 4 s before the break ends, and one cue-in on the first content segment after the break — all sharing the break's identity.

**Acceptance Scenarios**:

1. **Given** a break starting at a known timeline position, **When** the preceding content segments exist, **Then** placement-opportunity-start markers are attached to the segments containing 8 s and 4 s before the break start.
2. **Given** the first ad segment of a break, **When** it is prepared, **Then** it carries the cue-out marker at the break start with out-of-network signaling and the break duration.
3. **Given** a break about to end, **When** the segment containing 4 s before the break end exists, **Then** it carries a placement-opportunity-end marker.
4. **Given** the first content segment after a break, **When** it is prepared, **Then** it carries the cue-in marker at the break end, and the marker's break identity matches the cue-out.
5. **Given** a pre-roll position that falls before the available timeline, **When** the break is built, **Then** that missing pre-roll marker is skipped but the cue-out and cue-in are still emitted.

---

### User Story 3 - PTS stays continuous through content and ads (Priority: P1)

A player consuming the stream sees one continuous presentation timeline across content-to-ad and ad-to-content transitions. Segment durations and splice times line up with the channel clock, not with the source asset's original timestamps.

**Why this priority**: A discontinuity at a break causes visible glitches, rejected splice points, and mis-timed ad starts.

**Independent Test**: Walk a timeline containing at least two breaks and confirm that each segment's playback offset continues from the previous segment, that presentation timestamps rise monotonically, and that each splice time matches the break's position on the channel clock.

**Acceptance Scenarios**:

1. **Given** a content segment followed by an ad segment, **When** both are prepared, **Then** the ad segment's playback offset continues from the content segment without a gap or rewind.
2. **Given** an ad segment followed by resumed content, **When** both are prepared, **Then** the resumed content continues after the break on the same monotonic timeline.
3. **Given** a break at a known channel-clock position, **When** its cue-out is emitted, **Then** the splice time corresponds to that position and not to the ad asset's original timestamp.
4. **Given** segments prepared from different source assets, **When** their timestamps are rewritten, **Then** their presentation timestamps remain continuous across the boundary.

---

### User Story 4 - Static PMT is preserved (Priority: P2)

A downstream partner demuxes consecutive segments without seeing a PMT content change, so its ad-handling state survives every content/ad transition.

**Why this priority**: Spec 001 established this partner requirement. Restoring the main branch's signaling must not regress it.

**Independent Test**: Parse the PMT of a content segment, an ad segment, and a splice-bearing segment from the same rendition, and confirm the SCTE-35 elementary stream declaration is identical in all of them.

**Acceptance Scenarios**:

1. **Given** a content segment with no SCTE-35 packets, **When** its PMT is read, **Then** the SCTE-35 elementary stream is declared.
2. **Given** an ad segment and a content segment from the same rendition, **When** their PMTs are compared, **Then** the stream list, stream type, PID, and registration descriptor are identical.
3. **Given** a splice-bearing segment, **When** its PMT is read, **Then** it declares the same SCTE-35 stream as every other segment in that rendition.

---

### User Story 5 - EPG items come from the partner XML source (Priority: P1)

The planner obtains the day's schedule from the partner XML API using the playout channel identifier, and each item yields its start, stop, primary stream, and any ad markers.

**Why this priority**: The XML EPG is the authoritative input; the old JSON schedule source and its interval ads are no longer used.

**Independent Test**: Point the planner at a stub partner XML endpoint and confirm the resulting timeline uses exactly the items and ad markers returned by that endpoint, with no second schedule source consulted.

**Acceptance Scenarios**:

1. **Given** a channel with a guide in the partner XML source, **When** the planner prepares a day, **Then** it uses that guide's items and ignores the legacy JSON schedule source.
2. **Given** an item whose primary stream is missing, **When** the guide is normalized, **Then** the item is omitted rather than producing a segment with no source.
3. **Given** the partner source is unavailable and retained schedule fallback exists, **When** the day is prepared, **Then** the existing fallback behavior is used unchanged.

---

### Edge Cases

- An ad marker's declared start is already behind the current timeline position: skip that break rather than rewinding or replaying it.
- An ad marker's asset cannot be read or has no renditions: skip that break, log the failure, and continue content without leaving a hole.
- An ad break's declared duration extends past its programme's end: the break runs to its declared end and resumed content starts after it.
- The content asset ends before a declared marker's start: play the remaining break at the content end rather than leaving a gap, and log the timing adjustment.
- Two markers declare the same start: process them sequentially in declaration order, each as its own break.
- Consecutive breaks occur with no content between them: each break still emits its cue-out, and a cue-in is emitted only when content resumes; earlier cue-ins that cannot be placed are logged.
- A programme is itself marked as an ad programme with no inline marker: treat the whole programme as one break spanning its declared window.
- An ad marker has zero or negative duration: ignore the marker.
- The break starts at the very beginning of the available timeline: cue-out and cue-in still emit; missing pre-roll positions are skipped.
- The break ends at the end of the day's horizon: cue-out still emits; cue-in may be absent if no content segment follows.
- A programme crosses a day boundary or a daylight-saving transition: its times remain ordered and its break positions remain on the channel clock.
- A content programme has no primary stream or uses an unsupported schema: omit it and continue with the remaining items.
- Replanning the same day: breaks are not duplicated in the timeline and previously emitted boundaries remain consistent for the same break.
- A segment carries both a pre-roll marker and a cue-out marker: both are preserved in chronological order.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST obtain EPG items for a channel and day from the partner XML source using the playout channel identifier.
- **FR-002**: Each EPG item MUST provide a start, a stop, a primary playout stream, and zero or more ad markers; each ad marker MUST provide an asset path, a duration, and a start.
- **FR-003**: The system MUST derive ad breaks only from ad markers declared in the XML EPG (or a programme-level ad designation); it MUST NOT insert ads based on elapsed time or a fixed interval.
- **FR-004**: Each XML ad marker MUST become exactly one independent break anchored at its declared start, with its declared path and duration.
- **FR-005**: When several ad markers appear in one programme, each MUST produce its own break in declaration order.
- **FR-006**: For each break, the system MUST attach placement-opportunity-start markers at 8 seconds and 4 seconds before the break start to the segments containing those positions, when those positions exist.
- **FR-007**: The first ad segment of a break MUST carry a cue-out marker at the break start with out-of-network signaling and the break's duration.
- **FR-008**: For each break, the system MUST attach a placement-opportunity-end marker at 4 seconds before the break end to the segment containing that position, when it exists.
- **FR-009**: The first content segment after a break MUST carry a cue-in marker at the break end with in-network signaling.
- **FR-010**: Pre-roll, cue-out, placement-opportunity-end, and cue-in markers belonging to one break MUST share one break identity that is distinct from other breaks.
- **FR-011**: Cue-out and cue-in markers MUST use immediate-splice signaling without a presentation-time attribute; pre-roll and placement-opportunity-end markers MUST carry a presentation-time attribute.
- **FR-012**: Ad segments MUST be identifiable as advertising and MUST carry the break identity shared with their boundaries.
- **FR-013**: Segment playback offsets MUST continue monotonically across content-to-ad and ad-to-content transitions, with no gap, rewind, or reliance on the source asset's original timestamps.
- **FR-014**: Splice times for each break MUST be computed from the break's position on the channel timeline, and ad segment durations MUST come from the ad asset's own segment ranges.
- **FR-015**: Every segment — content, ad, and splice-bearing — MUST declare the same SCTE-35 elementary stream in its PMT: stream type 0x86, PID 0x1F00, registration descriptor 0x43554549.
- **FR-016**: PMT content MUST remain identical across all segments of the same rendition, preserving spec 001.
- **FR-017**: PMT and SCTE-35 construction failures MUST be retried at least once, and a segment that still fails MUST be skipped and logged rather than served corrupt.
- **FR-018**: The HLS media playlist MUST emit a date-range cue-out tag on the first ad segment, a date-range cue-in tag on the first content segment after the break, and a per-segment SCTE-35 tag for each splice marker.
- **FR-019**: An ad marker whose start is behind the current timeline position MUST be skipped, not replayed or used to rewind the clock.
- **FR-020**: An unreadable ad asset MUST cause that break to be skipped with a logged failure while content processing continues.
- **FR-021**: When the partner XML source is unavailable, the existing retained-schedule fallback MUST continue to apply.
- **FR-022**: The legacy JSON schedule source and all interval-based ad settings MUST NOT influence the produced timeline.

### Key Entities *(include if data involved)*

- **EPG Item**: One XML schedule entry with a start, stop, primary stream reference, and zero or more ad markers.
- **Ad Marker**: An XML declaration carrying an asset path, a duration, and a start that defines one advertising break.
- **Ad Break**: The planned placement of one marker on the channel timeline, with a stable identity shared by all of its boundaries.
- **Segment**: One HLS media unit with a source path, duration, presentation range, playback offset, rendition set, and optional SCTE-35 markers.
- **Splice Marker**: A signaling event attached to a segment: pre-roll start, cue-out, placement-opportunity end, or cue-in.
- **PMT**: The program map that declares the SCTE-35 elementary stream consistently for every segment.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of XML ad markers produce one break each, and 0% of breaks are generated without a declared marker.
- **SC-002**: 100% of breaks that have surrounding segments produce one cue-out, one cue-in, two pre-roll markers where both positions exist, and one pre-return marker.
- **SC-003**: 100% of cue-in markers share the break identity of their matching cue-out.
- **SC-004**: Presentation timestamps are monotonically increasing across 100% of content-to-ad and ad-to-content boundaries, with zero gaps or rewinds.
- **SC-005**: PMT content is identical across 100% of consecutive segments in each rendition, and the SCTE-35 stream is declared in 100% of segments.
- **SC-006**: A channel whose XML EPG declares no ads produces zero ad segments over a full day of playback.
- **SC-007**: A test day with a known set of markers produces exactly that set of breaks, and each break starts within one segment duration of its declared start.
- **SC-008**: A downstream demuxer observing the resulting stream records zero stream-metadata resets caused by PMT changes across breaks.

## Assumptions

- The partner XML EPG is the single authoritative schedule source for planning; the legacy JSON schedule endpoint is no longer used.
- Ad asset paths are complete storage locations used verbatim, as established by spec 013.
- Each XML ad marker is a separate break; markers are not merged per programme.
- Cue-in belongs on the first content segment after the break, not on the last ad segment.
- Spec 001's static per-segment PMT supersedes the main branch's ad-only PMT handling; only the SCTE-35, pre-roll, cue, and PTS flow follows the main branch.
- "Follow the main branch logic" is a behavioral reference for boundary placement and timing, not a requirement to reintroduce inconsistent SCTE-35 PIDs.
- Break identity need not be globally unique, but it must be stable within a break and shared by all of that break's markers and ad segments.
- Times are normalized to the server's single timeline frame before planning, as already done for XML EPG items.
- A programme-level ad designation, if exposed by the XML source, is equivalent to one marker spanning the programme window.
- Existing retained-schedule fallback, day-horizon behavior, and stream-schema validation continue unchanged.
- SCTE-35 marker content, advertisement handling in the player, and the partner-facing XML output contract are unchanged by this feature.
- Admin JSON endpoints and the admin client are out of scope.
