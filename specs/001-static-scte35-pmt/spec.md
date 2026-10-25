# Feature Specification: Static SCTE-35 PMT

**Feature Branch**: `001-static-scte35-pmt`

**Created**: 2026-10-23

**Status**: Draft

**Input**: User description: "Partner feedback: Segments that contain SCTE-35 packets carry a PMT with the SCTE-35 elementary stream declared (stream_type 0x86). Segments without SCTE-35 packets carry a PMT without that stream. Both PMTs have version_number = 0 despite having different content. Impact: our demuxer detects the change in PMT content and triggers a reset of stream metadata, which discards any SCTE-35 ad-handling state. What we need: The SCTE-35 elementary stream (0x86) stays declared in the PMT even when a segment contains no SCTE-35 packets. The PMT remains unchanged across segments."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Consistent PMT Across Segments (Priority: P1)

As a downstream partner consuming SimLive HLS streams, when I process consecutive segments of the same rendition, the Program Map Table (PMT) must remain identical so that my demuxer does not interpret content changes as a stream reset.

**Why this priority**: This is the core issue reported by the partner. Any PMT content change between segments causes the partner's demuxer to discard SCTE-35 ad-handling state, breaking ad splicing and revenue tracking. Without a static PMT, the partner cannot reliably consume the stream.

**Independent Test**: Can be fully tested by parsing the PMT section from at least two consecutive segments — one containing SCTE-35 packets and one without — and verifying the PMT payload (excluding version_number) is byte-for-byte identical.

**Acceptance Scenarios**:

1. **Given** a segment that contains SCTE-35 packets, **When** the PMT is parsed, **Then** the SCTE-35 elementary stream (stream_type 0x86) is declared in the PMT.
2. **Given** a segment that contains no SCTE-35 packets, **When** the PMT is parsed, **Then** the SCTE-35 elementary stream (stream_type 0x86) is still declared in the PMT.
3. **Given** two consecutive segments (one with SCTE-35, one without), **When** their PMTs are compared, **Then** the PMT content is identical (same elementary streams, same stream_type, same ES PID).

---

### User Story 2 - PMT Version Number Compliance (Priority: P2)

As a downstream partner, when the PMT content does change for any reason, the version_number field must increment by 1 modulo 32 so that I can correctly detect the change per ISO/IEC 13818-1 §2.4.4.9.

**Why this priority**: If the PMT content ever changes (e.g., due to a legitimate stream reconfiguration), the version_number must reflect that change. Without proper versioning, the partner cannot distinguish between a static PMT and a silently changed one, leading to undetected stream inconsistencies.

**Independent Test**: Can be tested by introducing a deliberate PMT content change (e.g., adding or removing an elementary stream) and verifying that the version_number increments by exactly 1 modulo 32 from the previous value.

**Acceptance Scenarios**:

1. **Given** a PMT with version_number = N and content C, **When** the content changes to C' (a legitimate reconfiguration), **Then** the new PMT has version_number = (N + 1) mod 32.
2. **Given** a PMT with version_number = 31, **When** the content changes, **Then** the new PMT has version_number = 0 (wraps modulo 32).
3. **Given** a PMT with version_number = N and content C, **When** the content does not change, **Then** the version_number remains N.

---

### Edge Cases

- What happens when a segment contains SCTE-35 packets but the SCTE-35 stream was never declared in the PMT? The SCTE-35 stream must always be declared regardless of packet presence.
- How does the system handle a scenario where the SCTE-35 PID changes between segments? The PID must remain stable across segments; if it must change, the version_number must increment.
- What happens when the partner's demuxer encounters a PMT with version_number = 0 and identical content across segments? No stream reset should be triggered.
- What happens if PMT construction fails mid-stream? The system retries construction once; if it fails again, the segment is skipped and the failure is logged.

### Out of Scope

- SCTE-35 packet insertion logic (unchanged)
- Ad splicing / ad decision logic (unchanged)
- HLS playlist generation (unchanged)
- SCTE-35 splice information table (SIT) packet content (unchanged)
- Rendition ladder or segment duration (unchanged)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST declare the SCTE-35 elementary stream (stream_type 0x86) in the PMT for every segment, regardless of whether that segment contains SCTE-35 packets.
- **FR-002**: System MUST keep the PMT content identical across all segments of the same stream (same set of elementary streams, same stream_type values, same ES PIDs). Each segment is configured from its source through exactly one PMT-configuration path (VOD, ad, or splice-injected), all of which declare the same SCTE-35 stream (PID 0x1F00, stream_type 0x86, registration 0x43554549), producing byte-identical PMT content.
- **FR-003**: System MUST increment the PMT version_number by 1 modulo 32 whenever the PMT content changes for any reason (e.g., elementary stream added, removed, or PID reassigned).
- **FR-004**: System MUST NOT change the SCTE-35 elementary stream PID between segments unless the version_number is incremented.
- **FR-005**: System MUST log PMT anomalies — including unexpected content changes mid-stream, version_number mismatches, and PMT construction failures — using the project's structured logging conventions.
- **FR-006**: System MUST retry PMT construction at least once on failure; if construction still fails, the segment MUST be skipped (not served) and the failure MUST be logged per FR-005.

### Key Entities *(include if feature involves data)*

- **PMT (Program Map Table)**: The MPEG-TS table that maps elementary streams to their PIDs. Contains a version_number field and a list of elementary streams, each with a stream_type and PID.
- **SCTE-35 Elementary Stream**: An elementary stream declared with stream_type 0x86 in the PMT, carrying SCTE-35 splice information table (SIT) packets used for ad signaling.
- **Segment**: An HLS media segment containing MPEG-TS packets, including PMT packets and optionally SCTE-35 packets.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The PMT content (elementary stream list, stream_type values, ES PIDs) is identical across 100% of consecutive segments in a stream, verified by byte-level comparison of the PMT section payload. Validated across all rendition ladder rungs (360p, 720p, 1080p) and multiple channels.
- **SC-002**: The SCTE-35 elementary stream (stream_type 0x86) is declared in the PMT for 100% of segments, including segments that contain zero SCTE-35 packets.
- **SC-003**: The partner's demuxer does not trigger a stream metadata reset when processing consecutive segments, resulting in zero SCTE-35 ad-handling state losses per stream.
- **SC-004**: When PMT content changes due to a legitimate reconfiguration, the version_number increments by exactly 1 modulo 32 in 100% of cases.

## Clarifications

### Session 2026-10-23

- Q: When is the PMT for a segment constructed — once at stream initialization or freshly per segment? → A: Per segment, from source, via one of three idempotent `tsp -P pmt` paths (VOD, ad, splice-injected), all declaring the same SCTE-35 stream, so PMT content is identical across segments.
- Q: Should PMT construction and consistency events be logged with structured logging? → A: Log only anomalies — PMT content changes mid-stream, version_number mismatches, construction failures.
- Q: Which parts of the streaming pipeline are explicitly out of scope for this change? → A: Only the PMT generation in the segment builder — no changes to SCTE-35 packet insertion, ad logic, or playlist generation.
- Q: At what scale should the fix be validated during testing? → A: Multi-channel, multi-rendition (360p/720p/1080p) — full ladder as defined in the constitution.
- Q: What should happen if PMT construction fails for a segment? → A: Retry PMT construction; if still fails, skip the segment.

## Links

- Request: [231026-partner-feedback-number-version-pmt-static.md](../../requests/231026-partner-feedback-number-version-pmt-static.md)
- Plan: [plan.md](plan.md)
- Tasks: [tasks.md](tasks.md)
- Verification Skill: [scte35-pmt-verify/SKILL.md](../../.agents/skills/scte35-pmt-verify/SKILL.md)
- Verification Report: [verification-report.md](verification-report.md) (pending)

## Assumptions

- The streaming system generates PMTs as part of HLS segment generation for MPEG-TS streams.
- The PMT is configured per segment from source through an idempotent `tsp -P pmt` step, rather than being built once at stream initialization, so PMT content stays identical across segments.
- The partner's demuxer treats any change in PMT content (not just version_number) as a signal to reset stream metadata.
- The SCTE-35 elementary stream PID is known and stable at stream initialization time.
- ISO/IEC 13818-1 §2.4.4.9 is the authoritative standard for PMT version_number behavior.
