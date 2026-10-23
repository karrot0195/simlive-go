---
type: hub
hub: true
schema_version: 1
project: simlive
---

# Storage

Storage abstraction for video and ad content. Uses rclone to fetch segments from object storage buckets (video bucket and ads bucket).

## What belongs here

- Rclone service for remote file access
- Video bucket (VOD content)
- Ads bucket (ad segments for ad break injection)

## Related hubs

- [[engine]] — the engine downloads segments via rclone
- [[planner]] — the planner loads ad segments from the ads bucket