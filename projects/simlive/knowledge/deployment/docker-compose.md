---
type: project-knowledge
schema_version: 1
project: simlive
---

# Docker Compose

SimLive runs on Docker Compose with Postgres 15 and Redis 7.

Parent: [[deployment]]

## Services

- `postgres:15-alpine` — database for channel state (port 5432)
- `redis:7-alpine` — cache for timelines and HLS playlists (port 6371 → 6379)

## Related

- [[makefile]] — makefile targets to start services
- [[config]] — settings for DB and Redis connection strings