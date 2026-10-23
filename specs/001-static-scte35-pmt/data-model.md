# Data Model: Static SCTE-35 PMT

**Date**: 2026-10-23 | **Feature**: [Static SCTE-35 PMT](../spec.md)

## Overview

No new entities are introduced by this feature. The PMT configuration is a behavioral change in the segment preparation pipeline. This document describes the existing entities affected and the contract between the engine and TSDuck utilities.

## Existing Entities

### SegmentItem

Defined in `internal/models/models.go`. Key fields relevant to PMT configuration:

| Field | Type | Description |
|-------|------|-------------|
| `IsAd` | `bool` | True if this segment is a planner-injected ad |
| `SliceInserts` | `[]SpliceInsert` | SCTE-35 splice events to inject into this segment |
| `Sequence` | `int64` | Monotonic sequence number for ordering |
| `StoragePath` | `string` | Path template for the source segment |

### SpliceInsert

Defined in `internal/models/models.go`. Carries a single SCTE-35 splice event:

| Field | Type | Description |
|-------|------|-------------|
| `Type` | `string` | `"out"` or `"in"` |
| `EventID` | `uint32` | SCTE-35 splice_event_id |
| `SpliceTimePTS` | `int64` | PTS timestamp in 90kHz ticks |
| `Duration` | `float64` | Break duration in seconds |
| `SegmentationTypeID` | `int` | SCTE-35 segmentation_type_id (0x30, 0x34, 0x35) |

## PMT State Model

The PMT for each segment transitions through these states during preparation:

```
Source Segment (no SCTE-35 in PMT)
    │
    ├── Ad segment with splice events
    │   └── InjectSpliceEvent() → PMT has SCTE-35 (PID 0x30) + splice command
    │
    ├── Ad segment without splice events
    │   └── ConfigurePmtForAdsTs() → PMT has SCTE-35 (PID 0x1F00), no splice command
    │
    ├── VOD segment with splice events (pre-roll)
    │   └── InjectSpliceEvent() → PMT has SCTE-35 (PID 0x30) + splice command
    │
    └── VOD segment without splice events ← THE GAP
        └── (nothing) → PMT has NO SCTE-35 stream ❌
```

**Target state** (after fix):

```
Source Segment (no SCTE-35 in PMT)
    │
    ├── Any segment with splice events
    │   └── InjectSpliceEvent() → PMT has SCTE-35 (PID 0x1F00) + splice command
    │
    └── Any segment without splice events
        └── ConfigurePmtForVodTs() → PMT has SCTE-35 (PID 0x1F00), no splice command ✅
```

**Key invariant**: After preparation, ALL segments have identical PMT content (same PID `0x1F00` for SCTE-35 stream, same registration descriptor `0x43554549`).

## Validation Rules

- PMT content must be byte-for-byte identical across consecutive segments (SC-001)
- SCTE-35 stream (0x86) must be declared in PMT for 100% of segments (SC-002)
- PMT configuration must be idempotent: running it twice produces identical output
- On PMT configuration failure: retry once, then skip segment and log (FR-006)