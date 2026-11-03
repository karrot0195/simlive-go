# Feature Specification: Timeline UI Optimization

**Feature Branch**: `014-timeline-ui-optimize`

**Created**: 2026-11-03

**Status**: Draft

**Input**: User description: "optimize for timeline UI - Data fills from 4:26:51 PM to 12:00:02 AM -> expect show more MM-dd because it can multiple days - Ui become slow -> it looks due to too much data. Can lazy load or support load more btn ?"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Show date (MM-dd) when timeline spans multiple days (Priority: P1)

An operator views a channel timeline whose entries run from late afternoon (e.g. 4:26:51 PM) past midnight (e.g. 12:00:02 AM). Because the entries span more than one calendar day, the summary line and per-entry time labels must include the date (MM-dd) so the operator can tell which day each segment belongs to.

**Why this priority**: Without dates, a multi-day window is ambiguous and the operator cannot reason about scheduling across day boundaries.

**Independent Test**: Load a timeline whose entries span two calendar days; confirm the "Data fills from ... to ..." summary and entry time labels include MM-dd.

**Acceptance Scenarios**:

1. **Given** a timeline whose first and last entry fall on different calendar days, **When** the operator views the channel card, **Then** the summary reads "Data fills from MM-dd HH:MM:SS to MM-dd HH:MM:SS" (date shown for both endpoints).
2. **Given** a timeline whose entries all fall on a single calendar day, **When** the operator views the channel card, **Then** the summary shows time only (no redundant date), preserving the existing compact format.
3. **Given** an entry whose calendar day differs from the previous entry, **When** the timeline row is rendered, **Then** that entry's time label includes MM-dd.

---

### User Story 2 - Lazy load / load more for large timelines (Priority: P1)

An operator opens the Running Channels Timeline view for a channel that has a large number of upcoming entries. Rendering all entries at once makes the UI slow. The view should initially render a bounded window of entries and offer a "Load more" action (or automatic lazy loading on scroll) to reveal additional entries without blocking the UI.

**Why this priority**: UI responsiveness is required for operators to monitor channels; unbounded rendering degrades usability.

**Independent Test**: Load a channel with more than the initial page size of entries; confirm only the first page renders initially and "Load more" reveals the rest without full re-render stall.

**Acceptance Scenarios**:

1. **Given** a channel with more upcoming entries than the initial page size, **When** the view renders, **Then** only the first page of entries is rendered and a "Load more" control is visible.
2. **Given** the "Load more" control is visible, **When** the operator activates it, **Then** the next page of entries is appended and the control remains if more entries exist.
3. **Given** all entries have been loaded, **When** the view renders, **Then** no "Load more" control is shown.
4. **Given** new SSE updates arrive, **When** entries are refreshed, **Then** the loaded page count resets to the initial page so stale appended entries do not accumulate.

---

### User Story 3 - Preserve gap detection and readiness across lazy-loaded entries (Priority: P2)

Gap warnings (>10s) and prepared/ready state must continue to be computed and displayed for every rendered entry, including those revealed via "Load more", without recomputing over the entire (possibly huge) dataset on each render.

**Why this priority**: Lazy loading must not regress existing monitoring capabilities.

**Independent Test**: With a multi-page timeline containing a >10s gap straddling the page boundary, load more and confirm the gap is flagged.

**Acceptance Scenarios**:

1. **Given** a gap >10s between two consecutive entries where the second entry is on a later page, **When** that page is loaded, **Then** the gap is flagged on the boundary entry.
2. **Given** entries revealed via "Load more", **When** their prepared status changes via SSE, **Then** their ready/not-ready badges update.

---

### Edge Cases

- What happens when a channel has zero upcoming entries? The empty state is shown and no "Load more" control appears.
- What happens when the page size is larger than the total entries? All entries render and no "Load more" control appears.
- What happens on SSE refresh while the operator has loaded multiple pages? The loaded page count resets to the initial page to avoid showing stale appended entries; the operator can load more again.
- What happens when entries span midnight but all on adjacent days? Date (MM-dd) is shown only where the calendar day changes, keeping labels compact.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST format timestamps as "MM-dd HH:MM:SS" (or equivalent locale date+time) whenever the displayed range spans more than one calendar day; otherwise it MUST use the existing time-only format.
- **FR-002**: The "Data fills from ... to ..." summary MUST include the date (MM-dd) for both endpoints when the first and last entry fall on different calendar days.
- **FR-003**: The system MUST render only an initial bounded page of timeline entries per channel (default page size 50) on first render.
- **FR-004**: The system MUST provide a "Load more" control that appends the next page of entries for a channel when more entries exist.
- **FR-005**: The system MUST hide the "Load more" control when all entries for a channel have been loaded.
- **FR-006**: On SSE or manual refresh, the system MUST reset the loaded page count to the initial page for each channel.
- **FR-007**: Gap detection (>10s) MUST be computed against the full ordered entry list, not only the rendered subset, so boundary gaps are not lost.
- **FR-008**: Prepared/readiness badges MUST update for all rendered entries when prepared-segment data changes.
- **FR-009**: The system MUST keep the page size configurable (default 50) without code changes where feasible, or at least as a single named constant.

### Key Entities *(include if feature involves data)*

- **Timeline Entry**: A scheduled segment entry with sequence, running_time, duration, time_text, is_ad, and ad-break info, as stored in Redis.
- **Loaded Page State**: Per-channel count of how many entries have been revealed in the UI.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A channel with 500 upcoming entries renders its first paint in under 1 second with only 50 entries rendered.
- **SC-002**: The "Data fills from ... to ..." summary is unambiguous for any range spanning multiple calendar days (date shown for both endpoints).
- **SC-003**: Activating "Load more" reveals the next page without a full re-render stall perceptible to the operator.
- **SC-004**: Gap warnings and readiness badges remain correct after lazy loading and SSE refresh.

## Assumptions

- Page size default of 50 entries balances responsiveness and usefulness; configurable via a named constant.
- Date format uses MM-dd (month-day) as requested; full year omitted for compactness.
- Lazy loading is client-side pagination over already-fetched data; no API pagination changes required for v1. API-side pagination is a future enhancement if payload size becomes a bottleneck.
- The "Load more" interaction is preferred over infinite scroll for predictability, but infinite scroll may be added later.
- Captured request (during implementation): improve UX/behavior of "Load more" (loaded/total count, clearer affordance, optional "Load all", preserve scroll position).