---
type: project-knowledge
schema_version: 1
project: simlive
---

# SCTE-35

SCTE-35 splice insert markers are used for ad break insertion. The planner stamps splice inserts on segments; the engine emits `#EXT-X-DATERANGE` tags with `SCTE35-OUT`/`SCTE35-IN` payloads.

Parent: [[models]]

## Constants

- `SegTypePlacementOpportunityStart` (0x30) — pre-roll (8s/4s warning)
- `SegTypeAdvertisementStart` (0x34) — slice point / cue-out
- `SegTypeAdvertisementEnd` (0x35) — cue-in

## Ad break markers

- `IsAd` — marks a segment as belonging to a planner-injected ad break
- `AdBreakID` — stable identifier for the whole break (derived from start second)
- `AdBreakStart` — break start on local timeline (RFC3339 UTC in DATERANGE)
- `AdBreakDuration` — break length in seconds (used for DATERANGE DURATION and splice_insert BreakDuration)

## Related

- [[segment-item]] — the model carrying SCTE-35 fields
- [[epg-processing]] — the planner stamps splice inserts