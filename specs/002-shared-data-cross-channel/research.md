# Research

## Decision
Move `processingSegments`, `isBlockingProcessing`, `adsId` from `Planner` to `SegmentProcessor`.

## Rationale
These fields are mutated during a single `ProcessEPGToTimeline` call and only meaningful per channel. On shared `Planner`, concurrent channels overwrite each other's buffers, causing cross-channel segment leakage during ad injection.

## Alternatives considered
- Mutex around Planner fields: rejected; still mixes state across channels.
- Per-channel Planner instances: larger refactor; unnecessary.