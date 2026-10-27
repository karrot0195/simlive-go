# Implementation Plan: Improve Logging

**Branch**: `003-improve-logging` | **Date**: 2026-10-27 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-improve-logging/spec.md`

## Summary

Remove the custom V* logging wrapper methods (VInfo, VWarning, VError, VErrorWithError, VFatalf) from `internal/utils/logging.go` and migrate all call sites to the native slog methods (Info, Warn, Error) while preserving the `[name]: ` prefix via a retained format helper. Separately, enhance `RcloneService.CopyTo` (and `Cat` for parity) to measure elapsed execution time, capture stderr, and support an optional verbose mode (constructor flag) that passes rclone's `--verbose` so provider-level (B2) diagnostics surface in errors.

## Technical Context

**Language/Version**: Go 1.26+

**Primary Dependencies**: log/slog (standard library), os/exec, time (standard library); rclone binary (existing)

**Storage**: N/A (logging + subprocess wrapper)

**Testing**: `go test ./...`, `make build`, `make vet`

**Target Platform**: Linux server

**Project Type**: Backend services (api, planner, engines)

**Performance Goals**: No measurable regression; rclone timeout bounds unchanged (10s cat/copy)

**Constraints**: No new dependencies; preserve existing `[name]: ` prefix formatting and fatal-exit semantics at call sites

**Scale/Scope**: 3 files currently use V* methods (cmd/planner/main.go, internal/engine/engine.go, internal/utils/logging.go); rclone wrapper in internal/storage/rclone.go

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Compliance | Notes |
|-----------|------------|-------|
| **IV. Observability & Structured Logging** | ✅ Pass | Change strengthens observability: native level methods + rclone timing/provider errors. |
| **V. Simplicity & YAGNI** | ✅ Pass | Removes indirection (V* wrappers); minimal rclone enhancement. No new deps. |
| **Architecture Constraints** | ✅ Pass | Standard library only; rclone already a dependency. |

**Gate Result**: ✅ All gates pass.

**Post-Design Re-check**: ✅ Still passes.

## Project Structure

### Documentation (this feature)

```text
specs/003-improve-logging/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/           # (none - internal change)
└── tasks.md
```

### Source Code (repository root)

```text
internal/
├── utils/
│   └── logging.go       # Remove V* methods; keep Format helper
├── storage/
│   └── rclone.go        # Add timing + verbose mode
cmd/
└── planner/
    └── main.go          # Migrate V* call sites
internal/
└── engine/
    └── engine.go        # Migrate V* call sites
```

**Structure Decision**: Single Go project. Changes confined to `internal/utils/logging.go`, `internal/storage/rclone.go`, and two call-site files.

## Complexity Tracking

> No Constitution Check violations — section not required.