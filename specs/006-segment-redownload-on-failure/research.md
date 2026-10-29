# Research: Segment Re-download on Failure

## Decisions

- **Marker creation only on full success**: `prepareSegmentFromSource` must call `markPrepared` only after download, PTS rewrite, and slice/PMT configuration all succeed. Current code marks prepared even when `rewritePtsOffset` fails; fix that.
- **Size gate in playlist builder**: `generateRenditionPlaylist` already filters by `.pts` marker; add a minimum file-size check (~100KB) so empty/partial files are never appended.
- **Stale partial cleanup**: Before re-downloading, delete any existing dest file that is missing or below the threshold, preventing reuse of partial writes.
- **Retry bound**: Retries continue until the segment leaves the computed download window; no separate retry counter.
- **Fixed threshold**: Use a fixed ~100KB minimum; no env var, per simplicity principle.

## Alternatives Considered

- Configurable threshold via env var: rejected to keep behavior simple; can revisit if a failing test demands it.
- Separate retry queue: rejected; the playout loop already recomputes the download window each tick.