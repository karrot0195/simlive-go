# Research: XML EPG Ad Pipeline

## D1. Scope of the main-branch restoration

**Decision**: Restore the main branch's ad signaling flow only: one break per XML marker, pre-roll warnings, cue-out, placement-opportunity-end, cue-in on the following content segment, counter-based break IDs, and timeline-relative PTS. Keep spec 013's partner XML acquisition, retained-schedule fallback, live-edge anchoring, schedule padding, and programme-end stop.

**Rationale**: The investigation summary identified the media signaling rewrite — not the schedule source — as the source of the divergence. The live-edge anchor and programme-end stop fix real scheduling holes and are orthogonal to ad boundaries. Keeping them avoids reintroducing known regressions while restoring the proven splice behavior.

**Alternatives considered**:
- Revert the entire planner to main — rejected: it would remove the XML source, fallback, and padding that this feature explicitly keeps.
- Keep 013's signalling and patch individual bugs — rejected: the cue-in placement, ID derivation, and pre-roll handling are structurally different, so patching would leave two competing flows.

## D2. PMT ownership and SCTE-35 PID

**Decision**: Retain spec 001's static per-segment PMT. Every segment is configured through a PMT path that declares stream type 0x86, PID 0x1F00, and registration 0x43554549. SCTE-35 injection also targets PID 0x1F00.

**Rationale**: The partner requirement that motivated spec 001 is still active. Main's ad-only PMT handling and its 0x30 injection PID produce different PMT content between segments, which triggers the partner demuxer reset that spec 001 fixed.

**Alternatives considered**:
- Copy main literally (ad-only PMT, 0x30) — rejected: known partner regression.
- Configure PMT only on ad segments while keeping 0x1F00 — rejected: content segments would still lack the SCTE-35 declaration.

## D3. When to stamp pre-roll and post-roll markers

**Decision**: Stamp boundaries while each segment is being constructed, before it is flushed to Redis. The content segment whose `[media_start, media_end)` window contains `break_start - 8` or `break_start - 4` receives that placement-opportunity-start marker. The ad segment whose window contains `break_end - 4` receives the placement-opportunity-end marker. Track emitted positions per break to avoid duplicates.

**Rationale**: Main's `findSegmentContainingPts` lookup depends on the in-memory segment buffer retaining the target segment. The planner flushes to Redis at the live edge and retains only a small tail, so a post-hoc lookup can either miss the segment or mutate an already-flushed member, leaving a duplicate without the marker. Stamping before flush guarantees the marker reaches the timeline exactly once and preserves the visible timing main intended.

**Alternatives considered**:
- Keep the post-hoc lookup and enlarge the buffer — rejected: still fails for already-flushed segments and can produce duplicate ZSET members.
- Stamp only at break time and re-flush earlier segments — rejected: duplicates timeline entries and complicates engine selection.

## D4. Break identity

**Decision**: Use a per-plan monotonically increasing counter for break identity. The counter value is the SCTE-35 event ID and the model's break ID; `unique_program_id` uses the value masked to 16 bits.

**Rationale**: The main branch used a counter and avoided the overflow problem described in the investigation summary. Epoch-derived IDs exceeded the 16-bit `unique_program_id` and produced invalid SCTE-35. A counter is stable within a break, shared by all of that break's markers and ad segments, and needs no global uniqueness guarantee.

**Alternatives considered**:
- Derive from marker start Unix time — rejected: overflows the 16-bit field and can collide after masking.
- Persist IDs in Redis for cross-run stability — rejected: unnecessary for SCTE-35 correlation and adds state and failure modes.

## D5. Cue-in placement and playlist emission

**Decision**: Attach the cue-in marker to the first content segment after the break. Populate that segment's break ID, break start, and break duration without setting the ad flag. Emit the cue-in date-range based on the cue-in role and break metadata, not on the ad flag. Carry a pending cue-in across schedule-item boundaries when a break ends at the end of one item's content.

**Rationale**: This matches the main branch's boundary intent while fixing two gaps: main did not mark the content segment as eligible for date-range cue-in emission, and it lost a cue-in when the break ended exactly at a stream boundary. The spec requires a date-range cue-in on the first content segment after the break.

**Alternatives considered**:
- Keep cue-in on the last ad segment (013 behavior) — rejected by the clarification and the spec.
- Emit cue-in only as a per-segment SCTE-35 tag — rejected: the date-range tag is part of the expected HLS contract.

## D6. Break duration source

**Decision**: Use the actual total duration of the ad asset's segments as the played break duration for splice times, break-end calculation, and the date-range duration. Use the marker's declared duration for scheduling and horizon checks. Log a warning when the two differ by more than one segment duration.

**Rationale**: PTS continuity depends on the duration actually inserted. Using a declared duration that disagrees with the asset would make the cue-in position drift from the last ad segment and break the monotonic timeline.

**Alternatives considered**:
- Trust the declared duration — rejected: a short asset would leave a gap before cue-in; a long asset would overlap resumed content.
- Trust the asset and ignore the marker duration — rejected: the marker duration is still needed for scheduling and for detecting malformed input.

## D7. Marker ordering and past markers

**Decision**: Sort markers by declared start. Before walking a programme's content, discard markers already behind the current timeline position, markers with invalid timestamps, and markers with zero or negative duration. During the walk, a marker becomes due when its start is at or before the current timeline position; the break starts at the current segment boundary.

**Rationale**: This preserves forward-only time and avoids replaying ads from earlier in the day. Starting at the current segment boundary matches the main branch and keeps the timeline free of gaps; the resulting start is within one segment duration of the declared start.

**Alternatives considered**:
- Split the content segment at the exact marker start — rejected: requires partial-segment handling the pipeline does not support.
- Jump the clock forward to the marker start — rejected: leaves a hole in coverage.
- When the content asset ends before a future marker, play that break at the content end — chosen to avoid dead air; the adjustment is logged so operators can see the break did not land at its declared start.
- When consecutive breaks have no content between them, drop the earlier unplaceable cue-in and log it — chosen because the timeline cannot represent two overlapping cue-in points at the same position.

## D8. Ad asset loading

**Decision**: For each marker, read the ad master playlist at the marker path, resolve renditions, read the primary rendition's media playlist, and derive per-segment durations from `#EXT-X-PTS-RANGE`, falling back to `#EXTINF` when the range is absent. If no rendition resolves, skip the break. Build ad segments with the marker path as the bucket-inclusive storage base.

**Rationale**: This mirrors the main branch's ad segment loading and the current XML ad path convention. It produces correct per-segment durations and reuses the existing rendition resolution helper.

**Alternatives considered**:
- Reuse the removed `ADS_STREAM_ID` setting — rejected: spec 013 removed it; markers carry their own paths.
- Cache ad segments per path in Redis — deferred: optional optimization, not required for correctness; a per-plan in-memory cache is sufficient if needed.

## D9. Schedule padding and ad markers

**Decision**: When padding shifts a schedule item's start and end, shift every inline ad marker start by the same delta. Drop markers whose timestamps cannot be parsed after the shift.

**Rationale**: Padding duplicates head/tail items to cover the day. Without shifting, duplicated items would carry markers at their original wall-clock times and either skip them as past or insert breaks outside their programme window.

**Alternatives considered**:
- Drop ad markers from padded duplicates — rejected: the padded copy represents a real playback window and should retain its ad pattern.
- Leave markers untouched — rejected: breaks would not align with the shifted programme.

## D10. Programme-level ad designation

**Decision**: If the XML source marks a programme itself as advertising (for example a text-only `<ads>true</ads>` or an equivalent attribute) and it has no inline markers, treat the programme window as one break and play the programme's primary stream as the ad asset.

**Rationale**: The spec allows a programme-level designation as an equivalent of one marker. Supporting both shapes keeps the parser robust across partner variants.

**Alternatives considered**:
- Ignore programme-level flags — rejected: the XML may expose the ad status that way instead of inline markers.
- Require inline markers only — rejected: would drop ad-only programmes from the schedule.

## D11. No new dependencies

**Decision**: Implement with the existing standard library, go-redis, GORM, and external tools. No new Go module or service is required.

**Rationale**: The change reworks control flow inside existing packages. The manifest parsing, sort, and XML handling all use the standard library.

**Alternatives considered**: Adding a scheduling or playlist library — rejected: unnecessary for the bounded scope and against the simplicity principle.

## D12. PTS space for splice `pts_time`

**Decision**: Placement-start and placement-end splice markers use `(currentPtsOffset - delay) * 90000` for `splice_time_pts`; `splice_time_seconds` carries the absolute channel-timeline position. Cue-out and cue-in remain immediate splices and do not emit `pts_time`.

**Rationale**: The engine rewrites each segment's output PTS with `output_ts_offset` equal to the segment's timeline offset. A splice `pts_time` expressed on the absolute wall clock would not match the rewritten stream and TSDuck/players would place the splice at the wrong point or reject it. The main branch used this relative form for placement markers; the 013 branch used the absolute value, which is one source of the reported timing problems.

**Alternatives considered**:
- Use the absolute wall-clock value — rejected: outside the rewritten PTS space and prone to 33-bit wraparound confusion.
- Use the asset's original PTS — rejected: the asset is re-timed onto the channel timeline before injection.

## D13. Verification approach

**Decision**: Verify in three layers: unit tests for parsing and marker placement; model tests for playlist output; integration checks that plan a day from a stub XML source, inspect the Redis timeline, run the engine, and use `pmt-verify`, `scte35-check`, and `hls-watch` to confirm PMT consistency, splice timing, and cue ordering.

**Rationale**: The constitution requires integration testing for HLS segment and SCTE-35 paths. The existing verification tools already cover PMT and SCTE-35 structure, so the new work only needs fixtures and expected marker sequences.

**Alternatives considered**: Unit tests only — rejected: does not exercise TSDuck/ffmpeg output or the engine's playlist assembly.
