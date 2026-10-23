---
type: hub
hub: true
schema_version: 1
project: simlive
---

# Engine

The playout engine drives HLS segment delivery per channel. It downloads and prepares segments, rewrites PTS offsets, generates rendition playlists, and manages the per-channel playout loop.

## What belongs here

- Per-channel playout loop (StartPlayoutLoop)
- Segment download and preparation pipeline
- PTS offset rewriting for continuous DTS across VOD + ads
- Rendition playlist generation (360p / 720p / 1080p)
- Master playlist generation

## Related hubs

- [[planner]] — the planner writes timeline data the engine consumes
- [[storage]] — the engine fetches segments via rclone from storage buckets
- [[models]] — shared data models used by the engine