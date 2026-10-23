---
type: project-knowledge
schema_version: 1
project: simlive
---

# PTS Offset Rewriting

The engine rewrites PTS/DTS on `.ts` segments using `ffmpeg -copyts -output_ts_offset` so that DTS stays continuous across VOD content and ad breaks. The offset is computed per-segment from the `PtsOffset` field on `SegmentItem`.

Parent: [[engine]]

## How it works

1. The planner stamps `PtsStart`, `PtsEnd`, and `PtsOffset` on every `SegmentItem` in the Redis timeline.
2. The engine reads these values when preparing segments for download.
3. `PtsOffset` is the segment's position on the channel's local timeline (seconds): `MediaStart - timelineBase`, where `timelineBase` = `MediaStart` of the first segment.
4. `ffmpeg -copyts -output_ts_offset` applies the offset so the output `.ts` has continuous timestamps regardless of the source's absolute epoch.

## Related

- [[segment-item]] — the SegmentItem model that carries PTS fields
- [[planner]] — the planner computes and stamps PTS values