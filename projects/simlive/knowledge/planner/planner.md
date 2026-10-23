---
type: hub
hub: true
schema_version: 1
project: simlive
---

# Planner

The planner processes EPG schedule data from ESPX, builds per-channel timelines, loads ad segments, and manages retry/failure logic for channel preparation.

## What belongs here

- EPG schedule fetching and timeline generation
- Ad segment loading from the ads bucket
- Retry logic with exponential backoff
- Channel lifecycle state transitions (PREPARE → LIVE → FAILED → INACTIVE)
- Old segment cleanup (retention)

## Related hubs

- [[engine]] — the engine consumes planner-produced timeline data
- [[models]] — PlayoutChannel and SegmentItem definitions
- [[storage]] — ad segments are fetched via rclone