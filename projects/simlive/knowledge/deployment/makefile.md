---
type: project-knowledge
schema_version: 1
project: simlive
---

# Makefile

The makefile provides targets to start services and build binaries.

Parent: [[deployment]]

## Targets

- `start-api` — `go run ./cmd/api`
- `start-planner` — `go run ./cmd/planner`
- `start-engines` — `go run ./cmd/engines`
- `start-all` — starts all three services concurrently
- `build` — builds all three binaries to `config/bin/`
- `build-client` — `cd ./client && pnpm build`

## Related

- [[docker-compose]] — the Docker Compose services the binaries connect to
- [[api]] — the API server started by `start-api`