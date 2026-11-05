# Implementation Plan: Dynamic Partner Communication Data Downloader

**Branch**: `018-partner-data-downloader` | **Date**: 2026-11-05 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/018-partner-data-downloader/spec.md`

## Summary

Introduce a pluggable `Downloader` abstraction for partner communication data acquisition. Currently the system uses rclone indirectly via storage/planner paths; per spec, we need:
- `Downloader` interface with implementations `RcloneDownload` (default, preserves behavior) and `ClientDownload` (HTTP)
- Per-partner `data_provider` selection in `partner.yaml` with default `rclone`
- `ClientDownload` reads endpoint/headers/timeout from partner profile; validates config at load time
- Callers depend only on `Downloader`; selection per request (config read per request)

This fits existing patterns: Go, library-first under `internal/`, config via `partner.yaml` read per request.

## Technical Context

**Language/Version**: Go 1.26+

**Primary Dependencies**: Gin, GORM/PostgreSQL, gopkg.in/yaml.v3 (existing). HTTP via net/http (stdlib). No new deps.

**Storage**: N/A (reads partner.yaml per request; no DB changes)

**Testing**: `go test ./...` (TDD per constitution)

**Target Platform**: Linux server

**Project Type**: Backend service (library-first under `internal/`)

**Performance Goals**: N/A (preserve existing behavior)

**Constraints**: Must preserve rclone default behavior (SC-001/SC-006); config read per request (SC-003); validation at load time (SC-004)

**Scale/Scope**: Per-partner configuration, affects partner data acquisition paths only

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Library-First**: New interfaces/impls go under `internal/` (e.g. `internal/downloader/` or `internal/storage/downloader/` as fits existing structure). Binaries remain thin. ✓
- **II. CLI Interface**: No new CLI binary required; affects service behavior. N/A.
- **III. Test-First**: Will write tests for config validation, provider selection, and Downloader implementations. ✓
- **IV. Integration Testing**: May need contract/integration tests if touching config loading paths used by multiple components. ✓
- **V. Observability & Simplicity**: Use existing logging patterns; keep simple, YAGNI. ✓

**Result:** PASS (no violations).

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file ($speckit-plan command output)
├── research.md          # Phase 0 output ($speckit-plan command)
├── data-model.md        # Phase 1 output ($speckit-plan command)
├── quickstart.md        # Phase 1 output ($speckit-plan command)
├── contracts/           # Phase 1 output ($speckit-plan command)
└── tasks.md             # Phase 2 output ($speckit-tasks command - NOT created by $speckit-plan)
```

### Source Code (repository root)

```text
internal/
├── partner/
│   ├── config.go        # extend with DataProvider, validation
│   └── config_test.go   # add tests
├── downloader/          # new package (or internal/storage/downloader)
│   ├── downloader.go    # interface + factory/resolver
│   ├── rclone.go        # RcloneDownload (wraps existing rclone usage)
│   └── http.go          # ClientDownload
└── storage/
    └── rclone.go        # existing RcloneService (may be reused)

cmd/                     # no changes expected
```

**Structure Decision**: Add `internal/downloader/` package for the abstraction and implementations (library-first). Extend `internal/partner/config.go` for provider selection and validation. Reuse `internal/storage/rclone.go` where appropriate.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
