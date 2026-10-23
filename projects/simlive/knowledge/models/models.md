---
type: hub
hub: true
schema_version: 1
project: simlive
---

# Models

Shared data models used across the SimLive system. Defines PlayoutChannel, SegmentItem, SpliceInsert, ESPX response types, and SCTE-35 segmentation type constants.

## What belongs here

- PlayoutChannel (channel state, timezone, EPG ID)
- SegmentItem (timeline members, PTS ranges, ad markers, renditions)
- SpliceInsert (SCTE-35 splice insert events)
- ESPX API response types (channels, schedules, programme streams)
- SCTE-35 segmentation_type_id constants

## Related hubs

- [[api]] — API handlers use these models for request/response bodies
- [[planner]] — the planner reads/writes SegmentItem to Redis timelines
- [[engine]] — the engine reads SegmentItem for segment preparation