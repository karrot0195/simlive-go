---
name: "scte35-pmt-verify"
description: "Verify SCTE-35 PMT configuration across HLS segments using pmt-verify, scte35-check, and hls-watch tools"
metadata:
  author: "simlive"
  source: "specs/001-static-scte35-pmt"
---

# SCTE-35 PMT Verification Skill

Verify that the SCTE-35 elementary stream (stream_type 0x86) is correctly declared in the PMT for every HLS segment, and that PMT content is identical across consecutive segments.

## Source Request

- Request: [231026-partner-feedback-number-version-pmt-static.md](../../requests/231026-partner-feedback-number-version-pmt-static.md)
- Spec: [specs/001-static-scte35-pmt/spec.md](../../specs/001-static-scte35-pmt/spec.md)
- Standard flow: [.agents/skills/feature-delivery-flow/SKILL.md](../feature-delivery-flow/SKILL.md)

## Prerequisites

- Running Docker Compose stack (Postgres 15, Redis 7) or local Postgres+Redis
- TSDuck `tsp` binary in PATH
- `ffprobe` (from ffmpeg) in PATH
- `rclone` configured for object storage access
- Built binaries: `make build` or pre-built in `config/bin/`

## Quick Start

```sh
# 1. Start the full stack
make start-all

# 2. Wait for engines to process segments (~15-30s)
#    Check logs: tail -f logs/engines.log

# 3. Run the filesystem watcher (monitors tmp dir for new segments)
go run ./cmd/pmt-verify --channel <channel-id> --res 360p --pmt=true --tsp=true

# 4. Or run the HTTP SCTE-35 checker (fetches live playlist)
go run ./cmd/scte35-check --api http://localhost:8080 --channel <channel-id> --res 360p

# 5. Or run the full HLS watcher
go run ./cmd/hls-watch --url http://localhost:8080/live/<channel-id>/360p/playlist.m3u8
```

## Verification Scenarios

### Scenario 1: SCTE-35 present in VOD segment PMT

**Objective**: Verify that a plain VOD segment (no ads, no splice events) declares the SCTE-35 stream in its PMT.

**Using pmt-verify**:
```sh
go run ./cmd/pmt-verify --channel <channel-id> --res 360p --pmt=true
```
Look for: `✅ SCTE-35 declared: PID 0x1F00, stream_type 0x86`

**Using scte35-check**:
```sh
go run ./cmd/scte35-check --channel <channel-id> --res 360p
```
Look for: `✅ SCTE-35 stream: PID 0x1F00, stream_type 0x86`

**Manual**:
```sh
tsp -I file "<segment.ts>" -P psi -O drop 2>/dev/null | grep -E "SCTE-35|0x86|CUEI"
```
Expected: `stream_type=0x86 (SCTE 35 Splice Info), PID: 0x1F00 (7936)` and `Format identifier: 0x43554549 ("CUEI")`

### Scenario 2: PMT identical across consecutive segments

**Objective**: Verify that two consecutive segments have byte-for-byte identical PMT content.

**Using pmt-verify**:
```sh
go run ./cmd/pmt-verify --channel <channel-id> --res 360p --pmt=true
```
Look for: `✅ PMT matches baseline <fingerprint>` on all segments after the first.

**Using scte35-check**:
```sh
go run ./cmd/scte35-check --channel <channel-id> --res 360p
```
Look for: `✅ PMT matches baseline`

**Manual**:
```sh
tsp -I file "<seg1.ts>" -P psi -O drop 2>/dev/null > pmt1.txt
tsp -I file "<seg2.ts>" -P psi -O drop 2>/dev/null > pmt2.txt
diff pmt1.txt pmt2.txt
```
Expected: No differences.

### Scenario 3: PMT version_number increments on content change

**Objective**: Verify that if PMT content changes, the version_number increments by 1 mod 32.

**Using pmt-verify with `--tsp=true`**:
```sh
go run ./cmd/pmt-verify --channel <channel-id> --res 360p --pmt=true --tsp=true
```
Look for: `Version: N` in the PSI dump output.

**Manual**:
```sh
# Check current version
tsp -I file "<segment.ts>" -P psi -O drop 2>/dev/null | grep "Version:"

# Force version increment
tsp -a 1/10 -I file "<segment.ts>" -P pmt --add-pid 0x1F02/0x06 --increment-version -O file /tmp/test.ts
tsp -I file /tmp/test.ts -P psi -O drop 2>/dev/null | grep "Version:"
```
Expected: Version increments by 1 (0→1, 31→0 wrap).

### Scenario 4: PMT configuration failure is logged and retried

**Objective**: Verify that PMT configuration failures are logged and retried per FR-005 and FR-006.

**Manual**:
```sh
# Temporarily break tsp
chmod -x $(which tsp)

# Wait for engine to attempt segment preparation
# Check engine logs:
grep -i "pmt" logs/ENGINE_*.log

# Restore tsp
chmod +x $(which tsp)
```
Expected log format: `[SCTE35] event=pmt_config_retry_exhausted channel=%s seq=%d dest=%s error=%v`

### Scenario 5: Full integration — partner demuxer does not reset

**Objective**: Verify end-to-end that the partner's demuxer does not trigger a stream metadata reset.

**Using pmt-verify**:
```sh
go run ./cmd/pmt-verify --channel <channel-id> --res 360p --pmt=true
```
Run for 60+ seconds across an ad boundary. Verify:
- All segments show `✅ SCTE-35 declared`
- All segments show `✅ PMT matches baseline`
- No `❌ PMT DIFFERS from baseline` messages

## Tool Reference

### `cmd/pmt-verify` (filesystem watcher)

Monitors `tmp/simlive-go/<channel>/<resolution>/` for new `.ts` segments.

| Flag | Default | Description |
|------|---------|-------------|
| `--channel` | (prompt) | Channel ID |
| `--res` | `360p` | Resolution |
| `--pmt` | `true` | Inspect PMT (SCTE-35 presence, fingerprint, consistency) |
| `--tsp` | `false` | Also run `tsp -P psi` for raw PMT + CUEI registration check |

### `cmd/scte35-check` (HTTP watcher)

Fetches live HLS playlists via HTTP and runs TSDuck on each segment.

| Flag | Default | Description |
|------|---------|-------------|
| `--api` | `http://localhost:8080` | API base URL |
| `--channel` | `f1533ebe-...` | Channel ID |
| `--res` | `360p` | Resolution |

### `cmd/hls-watch` (HLS watcher)

Full HLS segment analysis with SCTE-35, PTS, and PMT inspection.

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | (hardcoded) | Full m3u8 URL |

## Expected Results

| Check | Pass Criteria |
|-------|---------------|
| SCTE-35 in PMT | stream_type 0x86 present in 100% of segments |
| PMT consistency | Same fingerprint across all segments |
| version_number | Increments by 1 mod 32 on content change |
| CUEI registration | Registration descriptor 0x43554549 present |
| Error handling | Retry once, log on failure, skip segment |

## Troubleshooting

- **No segments appearing**: Ensure engines are running (`make start-engines`), check `logs/engines.log`
- **tsp fails with signal: killed**: TSDuck timeout too short — check `context.WithTimeout` in the engine code
- **rclone copyto error**: Verify object storage is accessible (`rclone ls partner-video-lib:`)
- **Verifier shows no output**: Ensure the channel has active segments in the playlist
- **tsp --no-warnings not recognized**: tsp 3.44 removed these flags; `pmt-verify` has been updated to omit them