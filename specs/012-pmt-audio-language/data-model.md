# Data Model: Static PMT Audio Language

**Date**: 2026-11-03 | **Feature**: [Static PMT Audio Language](spec.md)

## Overview

No new persistent database entities are introduced. The feature changes how an existing PMT field is produced, adds one configuration value, and adds one on-disk bookkeeping stamp. This document describes those entities and their transitions.

## Configuration Entity

### Platform Audio Language

Resolved lazily from the environment by `utils.GetPmtAudioLanguage()`.

| Field | Type | Source | Rules |
|-------|------|--------|-------|
| `language` | `string` (3 chars) | `PMT_AUDIO_LANGUAGE` | Trim, lowercase; must match `^[a-z]{3}$`; default `eng`; invalid/empty → `eng` + warning |

Constraints:
- Platform-wide; no per-channel or per-asset override.
- One value per process; changing it requires an engine restart (the prep-version stamp then triggers invalidation).

## PMT Model (verification view)

### `verify.StreamInfo` (extended)

| Field | Type | Description |
|-------|------|-------------|
| `PID` | `int` | Elementary stream PID (`0x0101`, `0x1F00`, …) |
| `StreamType` | `int` | MPEG-TS stream_type (`0x0F` AAC, `0x1B` H.264, `0x86` SCTE-35, …) |
| `CodecName` | `string` | ffprobe codec name |
| `IsSCTE35` | `bool` | Stream is the SCTE-35 elementary stream |
| `Language` | `string` | **NEW** ISO 639 language from the PMT descriptor; `""` when no descriptor is present |

### `verify.PmtInfo` (extended)

| Field | Type | Description |
|-------|------|-------------|
| `Streams` | `[]StreamInfo` | Elementary streams in PMT order |
| `Fingerprint` | `string` | SHA-256 (first 6 bytes, hex) over `pid:stream_type:language;` for each stream |
| `Raw` | `string` | Raw ffprobe JSON (diagnostics) |

Fingerprint rules:
- `Language` participates in the hash; `eng` vs `und` must produce different fingerprints.
- Missing language is encoded as `-`, distinct from any real code.
- `version_number` does not participate (it is unchanged by this feature and was never fingerprint input).

### `verify.PmtBaseline` (extended)

| Field | Type | Description |
|-------|------|-------------|
| `info` | `*PmtInfo` | **NEW** full baseline snapshot (replaces the string-only baseline) |
| `set` | `bool` | Whether a baseline has been captured |

Behavior:
- First observed PMT becomes the baseline.
- `Check(info)` returns the baseline, a consistency verdict, and whether this call set the baseline.
- `DiffPmt(baseline, current)` returns human-readable differences, e.g.
  - `PID 0x0101 stream_type 0x0F: language "und" -> "eng"`
  - `PID 0x0102 stream_type 0x0F: stream only in baseline`
  - `PID 0x0103 stream_type 0x0F: stream only in current`

## Prepared-Segment Cache Entities

### Prepared Segment (existing)

| Attribute | Value |
|-----------|-------|
| Path | `{TMP_DIR}/{channelID}/{rendition}/seg_{sequence}.ts` |
| Validity marker | `seg_{sequence}.ts.pts` (zero-byte) + file size ≥ 1000 bytes |
| Cache key | `(channelID, rendition, sequence)` |
| Contents | Source segment remuxed with PTS offset, PMT augmented with SCTE-35 ES + CUEI, audio language normalized |

### Preparation-Version Stamp (new)

| Attribute | Value |
|-----------|-------|
| Path | `{TMP_DIR}/{channelID}/.prep_version` |
| Content | `v1;audio-language=<lang>` |
| Written | After a purge, or asserted unchanged when it already matches |
| Read | Once per channel playout-loop start |

Invalidation rule:
- If the stamp is missing or its content differs from the expected value, all rendition directories under the channel are removed and recreated, `playlist.m3u8` at the channel root is removed, and Redis keys `simlive:hls:{channel}` plus `simlive:hls:{channel}:rendition:{rendition}` are deleted.
- A matching stamp leaves the cache untouched (no re-download on engine restart).

## State Transitions

### PMT language descriptor

```
Source PMT (VOD encode / ad encode / splice-injected preparation)
    │
    ├── audio descriptor = eng      ─┐
    ├── audio descriptor = und       │
    ├── audio descriptor = <other>   ├── tsp -P pmt --audio-language <configured>
    └── no audio descriptor        ─┘
                                     │
                                     ▼
                     Prepared PMT: exactly one ISO 639 descriptor
                     on the audio ES with the configured value
                     (version_number unchanged, all other PMT fields unchanged)
```

### Cached-segment lifecycle

```
Channel loop start
    │
    ├── stamp missing or != expected
    │       → purge rendition dirs + playlists + Redis HLS keys
    │       → write stamp
    │       → segments re-downloaded and re-prepared with normalized PMT
    │
    └── stamp == expected
            → keep prepared segments (normalized PMT already in place)
```

## Validation Rules

- Every served segment's PMT declares exactly one audio language descriptor on its audio elementary stream, equal to the configured value (FR-001/FR-005/SC-001).
- No other PMT content changes: ES list, stream types, PIDs, SCTE-35 ES, CUEI registration descriptor (FR-006).
- PMT content identical across consecutive segments of the same rendition across origins (FR-007/SC-002).
- `version_number` unchanged unless content legitimately changes (FR-008).
- A descriptor-only difference must alter the verification fingerprint and be reported with the differing stream/descriptor (FR-009/SC-004).
- A missing/invalid configured language falls back to `eng` with a warning (FR-013).
- Normalization failure follows the existing policy: retry once, then skip the segment and log (FR-011).
