<!-- bmad:context -->
<!-- Verified 2026-10-16 against simlive-go. Managed by bmad-project-context; edits inside this block are replaced on refresh. -->

## Orientation

The simlive-go project is a simulated live streaming service that generates mock live video streams for testing and benchmarking. It's designed to emulate live broadcast scenarios with various codecs and resolutions. The stack is Go (golang) with Docker Compose for orchestration.

## Policy

- **Branch policy**: Pull requests require one approval before merging to `main`.
- **Code ownership**: Core simulation logic in `src/`; never modify `legacy/` (frozen).
- **Generated code**: Never manually edit `src/generated/` — use `go generate`.
- **Testing**: Unit tests via `make test`; integration tests require Docker Compose.
- **CI**: Runs `go vet` and `make test` (includes `go fmt` and `go test`).

## Where things are

- **Simulation core**: `harness-flow-espx-wrap/projects/simlive-go/src/`
- **Docker setup**: `harness-flow-espx-wrap/projects/simlive-go/` (Dockerfile, compose)
- **Configuration**: `harness-flow-espx-wrap/projects/simlive-go/env-files/`
- **Tests**: `harness-flow-espx-wrap/projects/simlive-go/` (unit tests)
- **Documentation**: `harness-flow-espx-wrap/projects/simlive-go/docs/`
- **Source code**: `harness-flow-espx-wrap/projects/simlive-go/`

## Running and verifying

- **Unit tests**: `make test` (runs `go test ./...`)
- **Full build**: `make build`
- **Integration tests**: `docker compose up -d` then run tests
- **Linting/formatting**: `go fmt`, `go vet` (part of CI)
- **Code generation**: `go generate` for any generated code

## Conventions that differ from defaults

- **Go module versioning**: Uses `go.mod` with module name `simlive-go`
- **Simulated workload**: Generates synthetic HLS streams for performance testing
- **Docker multi-container**: Separates simulation engine from streaming server
- **Mock data**: Test fixtures are often generated programmatically

## Known pitfalls

- **Docker Compose networking**: Services must be properly tagged and accessible from clients
- **Go module imports**: Ensure cross-project imports (e.g., from esp-webservices) are declared correctly
- **Legacy code** in `legacy/` is frozen — do not modify
- **Environment variables**: Many config values are injected via env-files; missing vars cause panics
- **Resource exhaustion**: Simulations can consume significant CPU/memory; monitor during tests

<!-- /bmad:context -->
