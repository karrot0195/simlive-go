# Research: Static SCTE-35 PMT

**Date**: 2026-10-23 | **Feature**: [Static SCTE-35 PMT](../spec.md)

## Decisions

### Decision 1: PMT configuration approach

- **Decision**: Add a new `ConfigurePmtForVodTs` function in `internal/utils/tsduck.go` that runs `tsp -P pmt --add-pid <scte35-pid>/0x86 --add-registration 0x43554549` on VOD segments (mirroring the existing `ConfigurePmtForAdsTs`). Call it from `applySliceEvents` in `internal/engine/engine.go` for segments that are neither ads nor carry splice events.
- **Rationale**: The existing `ConfigurePmtForAdsTs` already does exactly what's needed for ad segments. VOD segments without splice events are the only gap. Rather than refactoring the pipeline, adding a parallel call path is minimal and low-risk.
- **Alternatives considered**:
  1. **Refactor PMT config into a shared pre-step**: Move PMT configuration out of `applySliceEvents` into `prepareSegmentFromSource` so it runs unconditionally. Higher refactor risk; touches the PTS rewrite marker logic.
  2. **Use a single `tsp` pipeline that always adds the PMT entry**: Modify the `rewritePtsOffset` ffmpeg command to also configure PMT. ffmpeg doesn't support PMT manipulation; would require a separate `tsp` pass anyway.
  3. **Patch the upstream VOD source files**: Modify the source m3u8 segments to include SCTE-35 in PMT. Not feasible — upstream content is read-only.

### Decision 2: SCTE-35 PID value

- **Decision**: Use `0x1F00` for ALL segments — VOD, ad, and splice-injected — by normalizing the `SCTE35_PID` constant from `0x30` to `0x1F00` (T002). `InjectSpliceEvent` now passes `SCTE35_PID` instead of a hard-coded PID, so every path declares the same SCTE-35 PID.
- **Rationale**: The existing code used two different PIDs (`0x1F00` in `ConfigurePmtForAdsTs`, `0x30` in `InjectSpliceEvent`). To keep PMT content identical across all segments (FR-002), one PID must be used consistently. `0x1F00` is the safer choice since it is already used for ad-only segments — the exact scenario extended to VOD — avoiding regression on existing ad segment behavior.
- **Alternatives considered**:
  1. **Use `0x30` everywhere**: Would require changing `ConfigurePmtForAdsTs` to use `0x30`, risking regression on existing ad segment behavior.
  2. **Make PID configurable**: Over-engineering for now; YAGNI.

### Decision 3: PMT construction timing

- **Decision**: PMT is configured per-segment at prepare time, not once at stream init. The PMT `tsp` command is idempotent — running `--add-pid` on a segment that already has the SCTE-35 stream in its PMT is a no-op. This means all segments converge to the same PMT content regardless of when they're prepared.
- **Rationale**: The engine processes segments as they become available (sliding window). There's no single "stream init" event that covers all segments. Per-segment idempotent configuration achieves the same result (identical PMT across all segments) without requiring an architectural change.
- **Alternatives considered**:
  1. **True stream-init approach**: Add a PMT initialization step when the engine starts processing a channel. Would require tracking whether PMT has been configured and could miss segments prepared before the init event.

### Decision 4: Error handling

- **Decision**: Follow FR-006 — retry PMT configuration once on failure; if still fails, skip the segment and log the anomaly per FR-005.
- **Rationale**: PMT configuration via `tsp` is a local filesystem operation; failures are rare and typically indicate disk space or binary issues. A single retry covers transient failures without excessive delay.

## Dependencies

- **TSDuck `tsp` binary**: Must be installed and in PATH. Already a project dependency.
- **Existing `ConfigurePmtForAdsTs` function**: Serves as the template for the VOD variant. Already tested in production for ad segments.

## Best Practices

- **Idempotency**: `tsp -P pmt --add-pid` is idempotent — running it on a segment that already has the SCTE-35 stream in its PMT produces identical output. This is critical for correctness.
- **Temporary file cleanup**: Follow the existing pattern in `applySliceEvents` — use a `.pmt.tmp` suffix, rename on success, clean up on failure.
- **Logging**: Use `utils.GetLogger(...)` with structured key-value fields (`channel`, `seq`, `dest`, `error`) for PMT configuration failures, matching the project's slog conventions. Include channelID, segment sequence, and dest in log messages.