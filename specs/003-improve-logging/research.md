# Research: Improve Logging

**Date**: 2026-10-27 | **Feature**: [Improve Logging](../spec.md)

## Decisions

### Decision 1: Native slog methods replace V* wrappers

- **Decision**: Remove VInfo/VWarning/VError/VErrorWithError/VFatalf from `internal/utils/logging.go`. Migrate call sites to `logger.Info/Warn/Error` with the existing `[name]: ` prefix produced by a retained `Format` helper method.
- **Rationale**: The V* wrappers only forwarded to slog with extra formatting; native methods are clearer and standard. Keeping a `Format(msg, args...)` helper preserves the prefix without duplicating format logic.
- **Alternatives considered**:
  1. Keep V* as deprecated aliases — rejected: leaves dead code and conflicts with "remove" requirement.
  2. Inline prefix at every call site — rejected: noisy and error-prone.

### Decision 2: Fatal-exit semantics

- **Decision**: VFatalf's os.Exit(1) is reproduced at call sites that need crash-on-fatal behavior via explicit `os.Exit(1)` after `logger.Error(...)`. Call sites that only logged become `logger.Error(...)`.
- **Rationale**: Fatal termination is a control-flow concern, not a logging concern.
- **Alternatives considered**: Add a `Fatalf` helper on Logger — rejected: reintroduces a V*-like wrapper.

### Decision 3: rclone verbose mode control

- **Decision**: Constructor option `NewRcloneService(verbose bool)`. When true, append `--verbose` to rclone args. Default false.
- **Rationale**: Clarified by user; keeps rclone behavior explicit and testable; avoids global env coupling.

### Decision 4: rclone timing + stderr surfacing

- **Decision**: In both `Cat` and `CopyTo`, record `start := time.Now()` before `cmd.Run()` and include `elapsed` (ms) in the error message on failure and log it on success. Keep stderr capture; with verbose mode, stderr includes provider (B2) diagnostics.
- **Rationale**: Meets FR-004/FR-005/FR-006/FR-007/FR-008. No change to timeouts.

## Dependencies

- rclone binary (existing)
- log/slog, os/exec, time (standard library)

## Best Practices

- Use `time.Since(start)` and report in milliseconds.
- Preserve existing timeout behavior (context.WithTimeout).
- Keep error message format `rclone ... error: %v, stderr: %s, elapsed: %s`.