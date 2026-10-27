# Feature Specification: Improve Logging

**Feature Branch**: `003-improve-logging`

**Created**: 2026-10-27

**Status**: Draft

**Input**: User description: "improve logging (1) remove V* method on internal/utils/logging.go as VInfo, VWarning, ... and use native method : Info, ... Remember update for related method (2) rclone -> add time execulted for dowload. When rclone error. We want know what error from provider (B2). Can enable to verbose mode or something else to see it ?"

## Clarifications

### Session 2026-10-27

- Q: How should rclone verbose mode be enabled? → A: Constructor option on RcloneService (NewRcloneService(verbose bool)).
- Q: Should elapsed download time be logged on successful downloads as well as on failures? → A: Log elapsed time on both success and failure for full observability.
- Q: Should the [name]: prefix formatting helper be retained in logging.go for use by native method calls? → A: Keep a Format/Prefix helper method so call sites can preserve the [name]: prefix.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Use Native Logger Methods (Priority: P1)

As a developer/operator reading service logs, I want all log calls to use the logger's native level methods (Info, Warn, Error, etc.) instead of the custom V* wrappers, so that log output is consistent with standard slog semantics and easier to filter by level.

**Why this priority**: The custom V* wrappers (VInfo, VWarning, VError, VErrorWithError, VFatalf) add an extra indirection layer that formats messages differently and obscures the underlying level. Removing them aligns logging with the standard library and reduces maintenance surface.

**Independent Test**: Can be verified by building all binaries and running the test suite; no V* method calls should remain in the codebase outside of logging.go, and the removed methods should no longer be defined.

**Acceptance Scenarios**:

1. **Given** the codebase after the change, **When** a developer searches for VInfo/VWarning/VError/VErrorWithError/VFatalf calls, **Then** no call sites reference the removed methods.
2. **Given** a service running with LOG_LEVEL=info, **When** it emits an informational message, **Then** the message appears via the native Info method with the same `[name]: message` formatting preserved.
3. **Given** the change is applied, **When** the project is built (`make build`), **Then** compilation succeeds with no errors.

---

### User Story 2 - Surface rclone Download Timing and Provider Errors (Priority: P2)

As an operator troubleshooting a failed segment download, I want the rclone download path to record how long the download took and to expose the underlying provider (B2) error details (via rclone verbose output when available), so that I can distinguish a slow transfer from a provider-side failure.

**Why this priority**: Today rclone failures surface only a generic error plus stderr. Without timing and provider-level detail, operators cannot tell whether a download stalled, timed out, or was rejected by B2.

**Independent Test**: Can be verified by triggering a download against a misconfigured/invalid remote and confirming the returned error includes both elapsed duration and rclone's verbose stderr (including any B2-specific message).

**Acceptance Scenarios**:

1. **Given** a successful CopyTo download, **When** it completes, **Then** the elapsed execution time is recorded in the log/error context.
2. **Given** a failing CopyTo download (e.g., invalid remote or auth error), **When** rclone exits non-zero, **Then** the returned error includes the elapsed time and rclone's stderr (which, with verbose mode enabled, contains the provider-level B2 error).
3. **Given** verbose mode is enabled for rclone, **When** a download fails, **Then** the captured stderr includes provider-specific diagnostic detail beyond the generic exit error.

---

### Edge Cases

- What happens when rclone times out (context deadline exceeded)? The error must still include elapsed time and any partial stderr captured.
- What happens when stderr is empty? The error should still report elapsed time and the underlying exec error.
- What happens when a call site used VFatalf (which exits the process)? After removal, callers must use the native Error method followed by an explicit os.Exit or return, preserving prior crash-on-fatal behavior where intended.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The logging package MUST remove the VInfo, VWarning, VError, VErrorWithError, and VFatalf methods.
- **FR-002**: All existing call sites of the removed V* methods MUST be updated to use the native slog methods (Info, Warn, Error) with equivalent message formatting.
- **FR-003**: The `[name]: ` prefix formatting previously applied by V* methods MUST be preserved for native calls (either by keeping a format helper or inlining the prefix).
- **FR-004**: The rclone CopyTo method MUST measure and include elapsed execution time in its error reporting (and log it on success where a logger is available).
- **FR-005**: The rclone CopyTo method MUST capture stderr from the rclone subprocess.
- **FR-006**: The rclone invocation MUST support an optional verbose mode that passes rclone's `--verbose` (or `-v`) flag so provider-level (B2) diagnostics are emitted to stderr.
- **FR-007**: When verbose mode is enabled and rclone fails, the returned error MUST include the verbose stderr content, surfacing the provider-side error message.
- **FR-008**: The Cat method SHOULD also report elapsed time and stderr consistently with CopyTo (parity).

### Key Entities

- **Logger**: Shared slog-based logger cached by name; provides native level methods.
- **RcloneService**: Wrapper around rclone subprocess calls (Cat, CopyTo) with timeouts.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A repository-wide search finds zero references to VInfo/VWarning/VError/VErrorWithError/VFatalf outside of logging.go after the change.
- **SC-002**: `make build` and `make test` complete successfully after the change.
- **SC-003**: A failed CopyTo download returns an error string containing both an elapsed-time value and non-empty stderr content when rclone emits diagnostics.
- **SC-004**: With verbose mode enabled, a failed download's error includes provider-specific (B2) detail that is absent in non-verbose mode.

## Assumptions

- Verbose mode is controlled by a constructor option on RcloneService (NewRcloneService(verbose bool)); default off to avoid noisy logs in normal operation.
- The `[name]: ` prefix behavior is preserved by routing through a small format helper retained in logging.go.
- Fatal-exit semantics previously provided by VFatalf are reproduced at call sites that need them (explicit os.Exit) rather than in the logger.
- Elapsed time is measured with time.Since around cmd.Run() and reported in milliseconds or seconds.
