# Data Model: Segment Readiness

## Segment File (on disk)

- `seg_<seq>.ts` — downloaded/prepared segment file.
- `seg_<seq>.ts.pts` — prepared marker; presence indicates preparation completed successfully.

## Readiness Rules

A segment is considered ready for playlist inclusion only when:
1. The `.pts` marker exists.
2. The `.ts` file exists and its size is >= 100KB (fixed threshold).

## State Transitions

- **pending** → no marker and/or undersized file.
- **prepared** → marker exists and file size >= threshold.
- **failed** → marker missing or file undersized; retried on next playout cycle after stale file removal.