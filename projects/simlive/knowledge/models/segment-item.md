---
type: project-knowledge
schema_version: 1
project: simlive
---

# SegmentItem

The core timeline member model. Stored as JSON in a Redis sorted set, ordered by `StartTime` (Unix timestamp).

Parent: [[models]]

## Fields

- `Sequence` — monotonic sequence number across days
- `StoragePath` — rclone path to the `.ts` segment
- `Duration` — segment duration (for `#EXTINF`)
- `StartTime` — Unix timestamp (for Redis ZSet ordering)
- `PtsStart` / `PtsEnd` — raw PTS values from `#EXT-X-PTS-RANGE`
- `MediaStart` / `MediaEnd` — PTS converted to seconds
- `PtsOffset` — position on the channel's local timeline
- `Renditions` — active quality rung tags (360p, 720p, 1080p)
- `IsAd` / `AdType` / `AdBreakID` / `AdBreakStart` / `AdBreakDuration` — SCTE-35 ad break markers
- `SliceInserts` — SCTE-35 splice insert events

## Related

- [[scte35]] — SCTE-35 constants and marker types
- [[pts-offset]] — how PtsOffset is used by the engine