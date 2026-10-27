# Data Model: Improve Logging

**Date**: 2026-10-27 | **Feature**: [Improve Logging](../spec.md)

## Overview

No new domain entities. Changes are behavioral: logging method surface and rclone wrapper internals.

## Modified Components

### Logger (internal/utils/logging.go)

- Removed methods: `VInfo`, `VWarning`, `VError`, `VErrorWithError`, `VFatalf`.
- Retained: `format(msg, args...)` helper (renamed/kept as `Format`? keep unexported `format`) for `[name]: ` prefix. Expose a helper method `Format(msg string, args ...interface{}) string` so call sites can preserve prefix when using native methods.
- Native methods used directly: `Info`, `Warn`, `Error` (embedded slog.Logger).

### RcloneService (internal/storage/rclone.go)

- New field: `verbose bool`.
- Constructor: `NewRcloneService(verbose bool) *RcloneService`.
- `Cat(remotePath string) (string, error)`: unchanged signature; internal timing + verbose flag.
- `CopyTo(parentCtx, src, dest) error`: unchanged signature; internal timing + verbose flag.

## Validation Rules

- No V* references remain outside logging.go after migration.
- `make build` and `go test ./...` pass.
- Failed CopyTo error includes elapsed time and stderr.