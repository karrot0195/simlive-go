# Data Model: XML EPG Ad Pipeline

## EPG Item (input)

One entry from the partner XML guide.

| Field | Type | Required | Rules |
|-------|------|----------|-------|
| `start` | timestamp | yes | Programme start in the partner XML layout; normalized to the server clock. Must parse successfully. |
| `stop` | timestamp | yes | Programme end in the same clock; must be after `start`. |
| `stream` | stream reference | yes | Primary stream selection: first stream where `is_vod && !is_trailer`, or `name == "Default Stream"`. Item is omitted when no stream matches. |
| `ads` | ad marker list | no | Zero or more attribute-bearing markers. A text-only `true` acts as a programme-level ad designation. |

## Ad Marker (input)

One declared advertising break.

| Field | Type | Required | Rules |
|-------|------|----------|-------|
| `path` | string | yes for inline markers | Bucket-inclusive storage base for the ad asset, used verbatim. |
| `duration` | integer seconds | yes for inline markers | Must be > 0. Scheduling hint; the played duration comes from the asset segments. |
| `start` | timestamp | yes for inline markers | Break position on the server clock. Markers behind the plan start are discarded. |

A programme-level designation produces one synthetic marker with the programme's primary stream as `path`, the programme start as `start`, and the programme length as `duration`.

## Ad Break (derived)

The planned placement of one marker.

| Field | Type | Rules |
|-------|------|-------|
| `id` | 32-bit integer | Assigned from a per-plan counter. Shared by every marker and ad segment of the break. Full value is the SCTE-35 event ID. |
| `start` | timeline seconds | Segment boundary at or after the marker start; never behind the current timeline position. |
| `declared_duration` | seconds | Marker duration, used for scheduling and horizon checks. |
| `played_duration` | seconds | Sum of the inserted ad segment durations, used for break end, splice times, and date-range duration. |
| `asset_path` | string | Marker path, used as the storage base. |

**State transitions**: declared → due (start <= current timeline position) → inserted (ad segments appended) → ended (break end reached, cue-in pending) → resumed (cue-in attached to first following content segment).

## Ad Asset Segment (derived)

| Field | Type | Rules |
|-------|------|-------|
| `storage_path` | string | `<asset_path>/{rendition}/<file>` |
| `duration` | seconds | From `#EXT-X-PTS-RANGE`; fallback to `#EXTINF` when absent. Must be > 0. |
| `pts_start` / `pts_end` | 90 kHz ticks | Raw asset range, retained for reference. |
| `renditions` | string list | Resolved from the ad master playlist. |

## Segment Item (existing timeline member, changed semantics)

| Field | Type | Change in this feature |
|-------|------|------------------------|
| `sequence` | int64 | Unchanged; continues across content and ads. |
| `storage_path` | string | Unchanged; bucket-inclusive base plus `{rendition}` placeholder. |
| `duration` | seconds | Content from asset PTS range; ad from ad asset range. |
| `pts_start` / `pts_end` | ticks | Unchanged. |
| `media_start` / `media_end` | timeline seconds | Continuous across content and ads. |
| `pts_offset` | timeline seconds | Continuous; used by the engine to rewrite output PTS. |
| `renditions` | string list | Unchanged. |
| `is_ad` | bool | True only for ad segments. The cue-in content segment stays false. |
| `ad_type` | string | `out` on the first ad segment (cue-out); `in` on the first content segment after the break (cue-in); empty elsewhere. |
| `ad_break_id` | uint32 | Set on every ad segment and on the cue-in content segment. |
| `ad_break_start` | timeline seconds | Set on every ad segment and on the cue-in content segment. |
| `ad_break_duration` | seconds | Set on every ad segment and on the cue-in content segment. |
| `splice_inserts` | marker list | Content segments may carry placement-opportunity-start markers and/or a cue-in; ad segments carry cue-out and placement-opportunity-end as applicable. |

## Splice Marker

| Field | Type | Rules |
|-------|------|-------|
| `type` | string | `out` for cue-out and placement-opportunity markers; `in` for cue-in. |
| `event_id` | uint32 | Break `id`. |
| `splice_time_pts` | 90 kHz ticks | Position in the segment's rewritten PTS space (`currentPtsOffset`-relative). Required for placement markers; omitted from the wire for immediate cue-out/cue-in. |
| `splice_time_seconds` | seconds | Absolute position on the channel timeline, used for correlation and logging. |
| `duration` | seconds | Played break duration; used on the first marker of the break. |
| `unique_program_id` | uint32 | Break `id` masked to 16 bits at emission. |
| `segmentation_type_id` | integer | `0x30` placement start, `0x34` cue-out, `0x35` cue-in, `0x36` placement end. |

## PMT (existing, unchanged)

Every segment from the same rendition declares the same SCTE-35 elementary stream:

| Property | Value |
|----------|-------|
| Stream type | `0x86` |
| PID | `0x1F00` |
| Registration | `0x43554549` (CUEI) |
| Content across segments | Identical |

## Validation Summary

- A marker with missing or invalid `start`, a non-positive duration, or a start behind the plan start is discarded.
- A marker whose asset master or rendition playlist cannot be read skips that break; content processing continues.
- A break never rewinds the timeline; the break start is the current segment boundary.
- Pre-roll markers are only emitted when their positions fall inside a produced content segment.
- Placement-end markers are only emitted when their positions fall inside a produced ad segment.
- Cue-in is emitted exactly once per break, on the first following content segment; if no content follows before the horizon, it is not emitted.
- When content ends before a future marker, the break plays at the content end to avoid a gap, and the timing adjustment is logged.
- When breaks are consecutive with no content between them, only the last break's cue-in can be placed when content resumes; earlier unplaceable cue-ins are logged and dropped.
- Break IDs are unique within a plan; `unique_program_id` is always within 16 bits.
