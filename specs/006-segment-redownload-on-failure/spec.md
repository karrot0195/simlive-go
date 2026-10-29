# Feature Specification: Segment Re-download on Failure

**Feature Branch**: `006-segment-redownload-on-failure`

**Created**: 2026-10-29

**Status**: Draft

**Input**: User description: "expect 1 segment can re-download if failed or no data by some reasons as (partner issue)"

## Clarifications

### Session 2026-10-29

- Q: When a previous download left a partial or zero-byte segment file, should the engine delete it before re-downloading? → A: Delete the stale partial/zero-byte file, then re-download.
- Q: Should the minimum usable segment size threshold be fixed or configurable? → A: Fixed ~100KB default.
- Q: Should a failing segment be retried indefinitely or stop after N attempts? → A: Retry until it leaves the download window.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Retry a failed segment download (Priority: P1)

When a segment download or preparation fails (or produces an empty/tiny file), the engine should not mark that segment as ready, and should retry downloading it on the next playout cycle instead of serving a broken/missing segment.

**Why this priority**: A single failed segment currently can be marked "prepared" even though preparation failed, causing it to be appended to the HLS playlist and served to viewers as a broken segment. Retrying keeps the stream continuous.

**Independent Test**: Simulate a failed rclone download or a zero-byte result for one segment; verify it is not appended to the rendition playlist and is retried on the next cycle.

**Acceptance Scenarios**:

1. **Given** a segment whose download fails, **When** the engine prepares segments, **Then** the segment is not marked prepared and is retried on the next playout cycle.
2. **Given** a segment file smaller than ~100KB after download, **When** the playlist is generated, **Then** that segment is skipped and not appended.
3. **Given** a segment that previously failed but succeeds on retry, **When** preparation completes, **Then** it is marked prepared and appended normally.

### User Story 2 - Avoid marking prepared on partial failure (Priority: P2)

Preparation steps (PTS rewrite, splice/PM T configuration) must fully succeed before a segment is marked prepared. A failure in any step must leave the segment unprepared so it can be retried.

**Why this priority**: Currently a marker can be created even when a preparation step fails, causing bad segments to be served.

**Independent Test**: Force a failure in PTS rewrite or PMT configuration; verify no prepared marker is created and the segment is retried.

**Acceptance Scenarios**:

1. **Given** PTS rewrite fails, **When** preparation runs, **Then** no prepared marker is created.
2. **Given** PMT configuration fails, **When** preparation runs, **Then** no prepared marker is created.

### Edge Cases

- What happens when a partial file remains from a failed download? The engine must not reuse it; it should re-download.
- What happens when a partner-side issue causes an empty manifest/segment? The size gate prevents serving it.
- What happens on repeated failures? The segment keeps being retried each cycle until success or it leaves the download window.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST NOT mark a segment as prepared unless all preparation steps (download, PTS rewrite, slice/PM T configuration) succeed.
- **FR-002**: System MUST retry downloading a segment on the next playout cycle if a previous attempt failed or produced no usable data.
- **FR-003**: System MUST skip appending a segment to a rendition playlist when its prepared marker is missing.
- **FR-004**: System MUST skip appending a segment whose resulting file size is below a minimum threshold (~100KB).
- **FR-005**: System MUST delete any stale partial or zero-byte file from a previous failed attempt, then re-download the segment (never reuse it).
- **FR-006**: System MUST log preparation failures with channel and sequence identifiers for operational visibility.

### Key Entities *(include if feature involves data)*

- **Segment file**: On-disk `.ts` file plus a `.pts` prepared marker; readiness is determined by marker presence and file size.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A failed segment download results in zero broken segments appended to the playlist on that cycle.
- **SC-002**: 100% of segments below the size threshold are excluded from playlists.
- **SC-003**: A segment that fails then succeeds on retry is served within two playout cycles.
- **SC-004**: Operators can identify failed segment preparations via structured logs.

## Assumptions

- Minimum usable segment size is approximately 100KB; smaller files are treated as failed/empty.
- Retry happens naturally through the existing playout loop; no separate retry queue is required.
- Partner-side issues (empty/partial remote files) are handled by the same size gate.