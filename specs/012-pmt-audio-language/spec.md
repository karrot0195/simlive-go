# Feature Specification: Static PMT Audio Language

**Feature Branch**: `012-pmt-audio-language`

**Created**: 2026-11-03

**Status**: Draft

**Input**: User description: "We are still seeing a change in the PMT, but this time it looks like only the language descriptor (ISO 639) of the AAC audio stream is changing. In some PMTs it is set to 'eng' and in others to 'und'. The SCTE-35 stream remains constant. Please set the audio language to 'eng' in every PMT, on all variants, so that the PMT stays identical from segment to segment. Any change in the PMT, including the language descriptor, resets our ad state."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Audio Language Descriptor Never Drifts Between Segments (Priority: P1)

As a downstream partner consuming SimLive HLS streams, when I process consecutive segments of the same rendition, the audio language descriptor (ISO 639) in the PMT must always read the same value so that my demuxer does not interpret the PMT as changed and reset my ad state.

**Why this priority**: This is the defect reported by the partner. Alternating `eng` / `und` on the AAC audio stream causes a PMT content change at every transition, which resets the partner's SCTE-35 ad-handling state and breaks ad splicing and revenue tracking. The SCTE-35 elementary stream is already stable, so the language descriptor is the only remaining drift.

**Independent Test**: Can be fully tested by parsing the PMT from at least two consecutive segments of the same rendition, one whose source PMT carried `eng` and one whose source PMT carried `und`, and verifying both now declare `eng` and are otherwise identical.

**Acceptance Scenarios**:

1. **Given** a segment whose upstream PMT has no audio language descriptor, **When** the segment is served, **Then** its PMT declares the audio language as `eng`.
2. **Given** a segment whose upstream PMT declares the audio language as `und`, **When** the segment is served, **Then** its PMT declares the audio language as `eng`.
3. **Given** a segment whose upstream PMT already declares `eng`, **When** the segment is served, **Then** its PMT still declares exactly one `eng` (no duplicate descriptor is introduced).
4. **Given** two consecutive segments from different origins (VOD → ad → VOD) in the same rendition, **When** their PMTs are compared, **Then** the audio language descriptor is `eng` in both and no PMT content differs.

---

### User Story 2 - Normalization Covers Every Variant and Segment Type (Priority: P2)

As an operator, I need the audio language normalization applied uniformly to every rendition and every segment origin (plain VOD, ad, and splice-injected) so there is no path that can emit a differently labelled PMT.

**Why this priority**: Ad segments and VOD segments are produced from separate encodes with independent PMT descriptors, and each origin takes a different preparation path. Fixing only one path leaves the drift in place at ad boundaries — exactly where the partner observes it. Renditions are consumed independently by the player, so a single unnormalized rung breaks that viewer.

**Independent Test**: Can be tested by generating segments of each origin type (VOD, ad, splice-injected) on every active rendition and confirming every resulting PMT declares `eng`.

**Acceptance Scenarios**:

1. **Given** an ad segment (no splice events), **When** it is served, **Then** its PMT declares the audio language as `eng`.
2. **Given** a VOD segment (no splice events), **When** it is served, **Then** its PMT declares the audio language as `eng`.
3. **Given** a splice-injected segment, **When** it is served, **Then** its PMT declares the audio language as `eng`.
4. **Given** each active rendition (360p, 720p, 1080p), **When** segments are inspected, **Then** every rendition reports the same audio language value.

---

### User Story 3 - Language Drift Is Detectable by Verification Tooling (Priority: P3)

As an operator, I need the PMT verification tools to notice a language descriptor difference and report it, so that a future regression of this class is caught before a partner reports it.

**Why this priority**: The current verification fingerprint covers only elementary-stream PIDs and stream types, so an `eng` / `und` flip is invisible and every existing check reports "PMT matches baseline". Without closing this gap, the fix cannot be proven and the defect can silently return.

**Independent Test**: Can be tested by feeding the verifier two segments that differ only in their language descriptor and confirming it reports a mismatch.

**Acceptance Scenarios**:

1. **Given** two segments whose PMTs differ only in the audio language descriptor, **When** the PMT consistency check runs, **Then** it reports a PMT mismatch.
2. **Given** a sequence of consecutive segments all declaring `eng`, **When** the PMT consistency check runs, **Then** it reports a match for every segment after the baseline.
3. **Given** a mismatch is reported, **When** the operator reads the output, **Then** the differing element (elementary stream and descriptor) is identifiable without manual hex inspection.

---

### Edge Cases

- **Source declares a language other than `und` or the configured value** (for example `fra`): the served PMT must present the configured platform language, consistent with the normalization policy.
- **Source PMT contains duplicate or multiple language descriptors for the same audio stream**: the served PMT must contain exactly one descriptor with the configured language value on that stream.
- **Source segment contains more than one audio elementary stream**: each audio elementary stream is normalized independently; the PMT remains internally consistent across segments.
- **Segment with no audio elementary stream at all**: normalization is a no-op and must not add an audio stream entry.
- **Non-AAC audio codecs** (for example AC-3) present alongside or instead of AAC: normalization applies to the audio streams present and must not corrupt or remove their codec-specific descriptors.
- **Segment already present in the prepared-segment cache from before the fix**: it must be re-prepared (or invalidated) so the partner is never served an old-vintage PMT once the fix is live.
- **Configured language value is empty or malformed**: the system falls back to `eng` and logs a warning rather than emitting a PMT without a language descriptor.
- **Normalization cannot be applied to a segment**: the system follows the existing failure policy — retry once, then skip the segment and log the failure, rather than serving a PMT that may drift.
- **PMT version number**: normalizing the language must not cause spurious version increments on repeated preparation of otherwise identical content.

### Out of Scope

- SCTE-35 packet insertion and splice information table content (unchanged)
- Ad decisioning / ad state handling on the partner side (unchanged)
- HLS playlist generation and segment duration (unchanged)
- Rendition ladder or transcoding behavior (unchanged)
- Translating, re-encoding, or authenticating non-English audio content
- Changes to PMT version-number policy established by the static SCTE-35 PMT feature

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every served segment in every active rendition MUST have a PMT that declares the audio language of its AAC audio elementary stream(s) as the configured platform language, which defaults to ISO 639 `eng`.
- **FR-002**: Normalization MUST apply to all segment origins — plain VOD, ad, and splice-injected — so that no preparation path can emit a PMT with a different audio language value.
- **FR-003**: When the upstream PMT declares no language descriptor, the served PMT MUST gain a language descriptor carrying the configured platform language on the audio stream.
- **FR-004**: When the upstream PMT declares a language value other than the configured platform language (including `und`), the served PMT MUST present the configured value instead.
- **FR-005**: When the upstream PMT already declares the configured language, the served PMT MUST retain exactly one such descriptor, with no duplicate added.
- **FR-006**: Normalization MUST NOT change any other PMT content — elementary stream list, stream types, elementary stream PIDs, the SCTE-35 elementary stream, or the CUEI registration descriptor. The PMT must remain otherwise byte-identical to what the existing pipeline produces.
- **FR-007**: The PMT content (including all elementary stream descriptors) MUST be identical across consecutive segments of the same rendition, regardless of segment origin, except for `version_number` when a legitimate content change occurs.
- **FR-008**: The existing PMT version-number policy MUST be preserved: no increment when content is unchanged; increment by 1 modulo 32 when content genuinely changes.
- **FR-009**: PMT verification tooling MUST include elementary-stream descriptors in its consistency fingerprint and MUST report which element differs when a mismatch is found.
- **FR-010**: Segments already prepared and cached before this change MUST be re-prepared or invalidated when their PMT does not carry the configured language, so that no served segment retains a pre-fix PMT.
- **FR-011**: Language normalization failures MUST be logged using the project's structured logging conventions, retried at least once, and the segment skipped if normalization still fails.
- **FR-012**: The system MUST provide an operator-visible way to confirm the normalized language value across consecutive segments of a running channel.
- **FR-013**: The platform language value MUST be operator-configurable through the project's standard environment configuration, defaulting to `eng` when unset; an empty or invalid value MUST fall back to `eng` and emit a warning.

### Key Entities *(include if feature involves data)*

- **PMT (Program Map Table)**: The MPEG-TS table mapping elementary streams to PIDs; carries a `version_number`, the elementary stream list, and per-stream descriptors. The audio stream's ISO 639 language descriptor is one of those per-stream descriptors.
- **Audio Language Descriptor**: The ISO 639 language descriptor attached to the AAC audio elementary stream in the PMT. Its value is what currently alternates between `eng` and `und`.
- **Segment**: An HLS media segment containing MPEG-TS packets with its own PMT. Segments originate as plain VOD, ad, or splice-injected content.
- **Rendition**: One rung of the fixed ladder (360p, 720p, 1080p); each rendition is consumed and verified independently.
- **Prepared-Segment Cache**: The on-disk store of already-prepared segments that the engine reuses instead of re-preparing a source segment.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of served segments across all active renditions declare exactly one audio language descriptor with the configured platform value (`eng` by default).
- **SC-002**: PMT content is identical across 100% of consecutive segments of the same rendition over a continuous observation window that spans at least one ad break, verified by full-PMT comparison including descriptors.
- **SC-003**: The partner observes zero ad-state resets attributable to a PMT content change, including the language descriptor, per stream per day.
- **SC-004**: The verification tooling detects a deliberate `eng` / `und` flip in 100% of trials and produces zero false "matches baseline" results for descriptor-only differences.
- **SC-005**: All acceptance criteria of the static SCTE-35 PMT feature remain satisfied — the SCTE-35 elementary stream is still declared in every segment and the CUEI registration descriptor is unchanged.
- **SC-006**: No increase in segment preparation failures or skipped segments after rollout (within existing operational tolerance).

## Clarifications

### Session 2026-11-03

- Q: Is the audio language a fixed platform-wide value of `eng`, or should it be configurable per channel or per asset? → A: Platform-wide value, operator-configurable through environment configuration with default `eng`. No per-channel or per-asset override.
- Q: What should happen to segments already prepared and cached before the fix? → A: Invalidate/re-prepare cached segments whose PMT lacks the configured language, so the fix applies to the entire served playlist window immediately.

## Links

- Related: [001-static-scte35-pmt](../001-static-scte35-pmt/spec.md) (static PMT and version numbering)
- Verification Skill: [scte35-pmt-verify](../../.agents/skills/scte35-pmt-verify/SKILL.md)

## Assumptions

- The upstream VOD and ad encodes are independent and do not agree on the audio language descriptor; that disagreement is the source of the `eng` / `und` alternation.
- Each rendition carries a single AAC audio elementary stream, as observed in current streams; the feature still behaves correctly if more audio streams exist.
- The content catalog is English-language, so `eng` — the environment-configuration default — is the correct platform value for all current channels.
- The platform language applies uniformly to every channel; there is no per-channel or per-asset override.
- All PMT paths already declare the same SCTE-35 elementary stream and CUEI registration descriptor, so only the audio language descriptor needs normalization.
- Normalization can be performed during segment preparation without re-encoding audio.
- The partner's demuxer compares PMT content (including descriptors), not just `version_number`, when deciding to reset state.
- Prepared segments are reused from cache; re-preparing segments that lack the configured language is acceptable in cost and needed for immediate effect.
