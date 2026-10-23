---
type: hub
hub: true
schema_version: 1
project: simlive
---

# Config

Application configuration and settings. Loads environment variables, database settings, planner settings, and video bucket configuration.

## What belongs here

- Settings struct (DB host/port/user/password, Redis addr, ESPX URL, video bucket, ads stream ID)
- Planner settings (retry policy, lookahead seconds, retention seconds)
- DATE_FORMAT constant
- Environment variable loading

## Related hubs

- [[api]] — API server uses settings for DB/Redis/ESPX connections
- [[planner]] — planner uses settings for retry and timeline config
- [[engine]] — engine uses settings for TMP dir and max workers