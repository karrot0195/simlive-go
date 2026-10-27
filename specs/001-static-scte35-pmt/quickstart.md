# Quickstart: Static SCTE-35 PMT Validation

**Date**: 2026-10-23 | **Feature**: [Static SCTE-35 PMT](../spec.md)

## Prerequisites

- Running Docker Compose stack (Postgres 15, Redis 7)
- TSDuck `tsp` binary in PATH
- `ffmpeg` in PATH
- `rclone` configured for object storage access
- Go+ toolchain for building

## Setup

```sh
# Build the project
make build

# Start the full stack
make start-all
```

## Validation Scenarios

### Scenario 1: PMT contains SCTE-35 on VOD segments without splice events

**Objective**: Verify that a plain VOD segment (no ads, no splice events) declares the SCTE-35 stream in its PMT.

**Steps**:
1. Wait for the engine to prepare a VOD segment (no ad break nearby)
2. Locate the prepared segment: `{TMP_DIR}/{channel_id}/{rendition}/seg_N.ts`
3. Inspect the PMT using `tsp`:
   ```sh
   tsp -I file "<segment.ts>" -P psi --pid 0x10 2>/dev/null
   ```
4. Verify the output contains:
   - `stream_type=0x86` (SCTE-35)
   - PID `0x1F00` (or configured SCTE-35 PID)
   - Registration descriptor `CUEI` (`0x43554549`)

**Expected**: The PMT declares the SCTE-35 stream even though the segment carries no SCTE-35 packets.

---

### Scenario 2: PMT is identical across consecutive segments

**Objective**: Verify that two consecutive segments (one with SCTE-35 packets, one without) have byte-for-byte identical PMT content.

**Steps**:
1. Wait for the engine to prepare segments spanning an ad boundary
2. Locate two consecutive segments: one VOD (before ad) and one ad (first ad segment)
3. Extract PMT payload from each:
   ```sh
   tsp -I file "<vod_segment.ts>" -P psi --pid 0x10 -O drop 2>/dev/null > pmt_vod.txt
   tsp -I file "<ad_segment.ts>" -P psi --pid 0x10 -O drop 2>/dev/null > pmt_ad.txt
   ```
4. Compare:
   ```sh
   diff pmt_vod.txt pmt_ad.txt
   ```

**Expected**: No differences (identical PMT content). Note: version_number field may differ if PMT content legitimately changed — compare the elementary stream list separately.

---

### Scenario 3: PMT version_number increments on content change

**Objective**: Verify that if PMT content changes, the version_number increments by 1 mod 32.

**Steps**:
1. Trigger a legitimate PMT reconfiguration (e.g., add a new elementary stream)
2. Extract version_number from PMT before and after:
   ```sh
   tsp -I file "<before.ts>" -P psi --pid 0x10 2>/dev/null | grep version_number
   tsp -I file "<after.ts>" -P psi --pid 0x10 2>/dev/null | grep version_number
   ```

**Expected**: `version_number` in the after segment is `(before + 1) mod 32`.

---

### Scenario 4: PMT configuration failure is logged and retried

**Objective**: Verify that PMT configuration failures are logged and retried per FR-005 and FR-006.

**Steps**:
1. Temporarily break the `tsp` binary (e.g., `chmod -x $(which tsp)`)
2. Wait for the engine to attempt segment preparation
3. Check engine logs:
   ```sh
   grep -i "pmt" {TMP_DIR}/logs/ENGINE_*.log
   ```

**Expected**: Log contains PMT configuration failure message with channelID and segment sequence. After restoring `tsp`, segment preparation succeeds.

---

### Scenario 5: Full integration — partner demuxer does not reset

**Objective**: Verify end-to-end that the partner's demuxer does not trigger a stream metadata reset.

**Steps**:
1. Run the full stack with a test channel that has ad breaks
2. Serve segments via the API
3. Feed consecutive segments to a demuxer that checks PMT content changes
4. Monitor for stream metadata reset events

**Expected**: Zero stream metadata resets triggered by PMT content changes.

## Test Data

- Test channels with ad breaks configured in the PARTNER schedule
- VOD content with known PTS ranges
- Ad segments in the configured ads bucket

## Expected Outcomes

| Scenario | Pass Criteria |
|----------|---------------|
| 1 | SCTE-35 stream (0x86) present in PMT of VOD segment without splice events |
| 2 | PMT content byte-for-byte identical across consecutive segments |
| 3 | version_number increments by 1 mod 32 on legitimate content change |
| 4 | PMT failure logged; segment skipped after retry |
| 5 | Zero demuxer resets across segment boundaries |

## References

- [Data Model](../data-model.md)
- [Interface Contracts](../contracts/README.md)
- [Feature Specification](../spec.md)