---
type: hub
hub: true
schema_version: 1
project: simlive
---

# Utils

Shared utility packages: logging, time helpers, m3u8 parsing, tsduck integration, and environment loading.

## What belongs here

- Structured logger (VInfo, VWarning, VFatalf, Error)
- Time location and day boundary helpers (StartOfDay, EndOfDay)
- M3U8 master playlist rendition parsing
- TS/DTS handling via tsduck
- Environment variable getters with defaults

## Related hubs

- [[api]] — API handlers use logging and time helpers
- [[planner]] — planner uses time helpers for day boundaries
- [[engine]] — engine uses logging and tsduck for segment prep