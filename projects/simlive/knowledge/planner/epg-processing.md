---
type: project-knowledge
schema_version: 1
project: simlive
---

# EPG Processing

The planner fetches EPG schedule data from ESPX, processes it into a Redis sorted-set timeline, and manages channel state transitions.

Parent: [[planner]]

## Flow

1. Fetch schedule items from ESPX (`GET /api/v2/common/channel-playouts` and schedule endpoints).
2. For each schedule item, create `SegmentItem` entries in the Redis timeline (`simlive:timeline:{channel_id}`).
3. If `UseAdEpgAds` is false, load ad segments from the ads bucket via rclone.
4. Track sequence numbers and running time across days for continuity.
5. Mark channel as LIVE on success, FAILED on error, INACTIVE after threshold failures.

## Related

- [[retry-logic]] — retry with exponential backoff wraps EPG processing
- [[segment-item]] — the timeline member model
- [[storage]] — ad segments are loaded from the ads bucket