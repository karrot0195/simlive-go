---
type: hub
hub: true
schema_version: 1
project: simlive
---

# Deployment

Deployment and build configuration for SimLive. Docker Compose services, makefile targets, and client build process.

## What belongs here

- Docker Compose (Postgres 15, Redis 7)
- Makefile targets (start-api, start-planner, start-engines, build, build-client, start-all)
- Go module dependencies (Gin, Gorm, Redis, godotenv, lumberjack, SCTE35)
- Vue 3 + Video.js client build

## Related hubs

- [[api]] — the API server is started via make targets
- [[planner]] — the planner binary is built via make
- [[engine]] — the engine binary is built via make