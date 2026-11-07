---
name: "verify-simlive-go"
description: "Full SimLive-Go verification: PMT/SCTE-35 declaration, ad-break lifecycle, audio language, PTS timing across segments"
metadata:
  author: "simlive"
  source: "specs/001-static-scte35-pmt"
---

# SimLive-Go Full Verification Skill

Verify the complete SimLive HLS output: PMT/SCTE-35 declaration, full ad-break lifecycle (pre-roll → cue-out → ads → placement-end → cue-in), audio language, and PTS timing across all segments.

## Source References

- Request: [231026-partner-feedback-number-version-pmt-static.md](../../requests/231026-partner-feedback-number-version-pmt-static.md)
- Spec: [specs/001-static-scte35-pmt/spec.md](../../specs/001-static-scte35-pmt/spec.md)
- Contract: [specs/017-xml-epg-scte35-pipeline/contracts/hls-cue-output.md](../../specs/017-xml-epg-scte35-pipeline/contracts/hls-cue-output.md)
- Pre-roll doc: [docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md](../../docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md)

## Prerequisites

- Running services: `make start-all` (API, planner, engines, partner)
- TSDuck `tsp` / `tstables` binary in PATH
- `ffprobe` (from ffmpeg) in PATH
- Built binaries: `make build`

## Quick Start

```sh
# 1. Start the full stack
make start-all

# 2. Wait for engines to process segments (~15-30s)
tail -f logs/engines.log

# 3. Run HTTP SCTE-35 checker (fetches live playlist, verifies PMT + splice events)
go run ./cmd/scte35-check --api http://localhost:8080 --channel <channel-id> --res 360p

# 4. Run full HLS watcher (PTS, PMT, SCTE-35 per segment)
go run ./cmd/hls-watch --url http://localhost:8080/live/<channel-id>/360p/playlist.m3u8

# 5. Run filesystem PMT watcher (monitors tmp dir)
go run ./cmd/pmt-verify --channel <channel-id> --res 360p --pmt=true --tsp=true
```

## Remote Stream Verification

When verifying a deployed stream (full m3u8 URL) instead of localhost:

1. Fetch the master playlist; pick the resolution sub-playlist (e.g. `360p/playlist.m3u8`).
2. **Capture immediately**: segments evict within ~5 target durations. Download the entire current window in one shot:
   ```sh
   base="https://host/live/<channel>/<res>"
   curl -s "$base/playlist.m3u8" | grep -o 'seg_[0-9]*.ts' | sort -u | while read f; do curl -s -o "$f" "$base/$f"; done
   ```
3. Poll the playlist every ~10–15s for `TYPE=48` (pre-roll) / `TYPE=52` (cue-out); on first hit, re-run the bulk download to capture the full break (pre-roll → cue-out → ads → placement-end → cue-in) before eviction.
4. Ad breaks recur ~5 min after the previous one ends (duration ~78s); budget ~6 min per capture cycle.
5. Analyze offline with `tstables` / `tsp` / `ffprobe` (commands below).

### PMT fingerprint (consistency) one-liner

```sh
for f in seg_*.ts; do printf "%s  " "$f"; tsp -I file "$f" -P psi -O drop 2>/dev/null | md5sum; done
# expect: identical hash across content, pre-roll, ad, placement-end, cue-in segments
```

### Bulk capture helper (recommended)

Save the snippet above as a small script; it avoids the manual sleep/poll loop and prevents segment loss.

## Full Ad-Break Lifecycle

The expected marker sequence in the HLS playlist:

```
[Pre-roll]  TYPE=48  → content segments at break start − 8s and − 4s
[Cue-out]   TYPE=52  → first ad segment of the break
[Ads]       (no tag) → remaining ad segments
[Placement-end] TYPE=54 → ad segment at break end − 4s
[Cue-in]    TYPE=53  → first content segment after the break
```

Marker details per the HLS cue contract:

| Marker | Type (dec) | Type (hex) | Segmentation Type | Where |
|--------|-----------|------------|-------------------|-------|
| Placement opportunity start (pre-roll) | 48 | 0x30 | 0x30 (Provider Advertisement Start) | Content segs at break start − 8s and − 4s |
| Cue-out | 52 | 0x34 | 0x34 (Provider Placement Opportunity Start) | First ad segment |
| Placement opportunity end | 54 | 0x36 | 0x36 (Distributor Placement Opportunity Start) | Ad seg at break end − 4s |
| Cue-in | 53 | 0x35 | 0x35 (Provider Placement Opportunity End) | First content seg after break |

Pre-roll delays: `PreRollDelaysSec = {8, 4}` (before cue-out), `PreRollCUEINDelaysSec = {4}` (placement-end before cue-in).

## Verification Scenarios

### Scenario 1: PMT — SCTE-35 declared on every segment

**Pass criteria**: `stream_type 0x86` present in 100% of segments, PID 0x1F00, CUEI registration descriptor present.

**scte35-check output**:
```
✅ SCTE-35 stream: PID 0x1F00, stream_type 0x86
✅ Registration descriptor "CUEI" present
```

**Manual**:
```sh
tsp -I file "<segment.ts>" -P psi -O drop 2>/dev/null | grep -E "SCTE-35|0x86|CUEI"
```

### Scenario 2: PMT identical across consecutive segments

**Pass criteria**: Same fingerprint on all segments (VOD, pre-roll, ads, cue-in).

**scte35-check output**:
```
✅ PMT matches baseline
```

**Manual**:
```sh
tsp -I file "<seg1.ts>" -P psi -O drop 2>/dev/null > pmt1.txt
tsp -I file "<seg2.ts>" -P psi -O drop 2>/dev/null > pmt2.txt
diff pmt1.txt pmt2.txt   # expect: no differences
```

### Scenario 3: Audio language

**Pass criteria**: All audio streams report `eng` (or configured `PMT_AUDIO_LANGUAGE`).

**scte35-check / pmt-verify output**:
```
✅ audio language eng on all N audio stream(s)
```

**Manual**:
```sh
tsp -I file "<segment.ts>" -P psi -O drop 2>/dev/null | grep -A1 "ISO-639 Language"
```

### Scenario 4: Pre-roll before CUE OUT

**Pass criteria**: Two content segments before the break carry TYPE=48 markers with segmentation type 0x30.

**Playlist check**:
```sh
curl -s <playlist.m3u8> | grep "TYPE=48"
```
Expect two lines: one at break start − 8s, one at break start − 4s.

**scte35-check output**:
```
[Preroll] SCTE-35 splice info:
  Segmentation type id: 0x30 (Provider Advertisement Start)
```

**TS verification**:
```sh
tstables <segment.ts> 2>&1 | grep "Segmentation type id"
# expect: 0x30 (Provider Advertisement Start)
```

### Scenario 5: CUE OUT

**Pass criteria**: First ad segment carries TYPE=52 marker, SCTE-35 SpliceInsert with immediate splice, out-of-network=yes, duration set.

**Playlist check**:
```sh
curl -s <playlist.m3u8> | grep "TYPE=52"
```

**TS verification**:
```sh
tstables <segment.ts> 2>&1 | grep -E "Command type|Out of network|immediate|Segmentation type id|Duration PTS"
# expect: Command type: 0x05 (SpliceInsert)
#         Out of network: yes, program splice: yes, duration set: yes, immediate: yes
#         Segmentation type id: 0x22 (Provider Ad End)
```

### Scenario 6: Placement-end (pre-roll before CUE IN)

**Pass criteria**: Ad segment at break end − 4s carries TYPE=54 marker with segmentation type 0x36.

**Playlist check**:
```sh
curl -s <playlist.m3u8> | grep "TYPE=54"
```

**TS verification**:
```sh
tstables <segment.ts> 2>&1 | grep "Segmentation type id"
# expect: 0x36 (Distributor Placement Opportunity Start)
```

### Scenario 7: CUE IN

**Pass criteria**: First content segment after break carries TYPE=53 marker, SCTE-35 SpliceInsert with immediate splice, out-of-network=no.

**Playlist check**:
```sh
curl -s <playlist.m3u8> | grep "TYPE=53"
```

**TS verification**:
```sh
tstables <segment.ts> 2>&1 | grep -E "Command type|Out of network|immediate|Segmentation type id"
# expect: Command type: 0x05 (SpliceInsert)
#         Out of network: no, program splice: yes, duration set: no, immediate: yes
#         Segmentation type id: 0x21 (Provider Ad Start)
```

### Scenario 8: PTS timing / pre-roll

**Pass criteria**: No PTS gaps, PTS range ≥ segment duration − 0.5s, no pre-roll PTS timing warnings.

**hls-watch output**:
```
✅ No issues detected
```

**Manual check**:
```sh
ffprobe -v error -show_entries packet=pts_time -select_streams v:0 -of csv=p=0 <segment.ts> | head -5
```

## Tool Reference

### `cmd/pmt-verify` (filesystem watcher)

Monitors `tmp/simlive-go/<channel>/<resolution>/` for new `.ts` segments.

| Flag | Default | Description |
|------|---------|-------------|
| `--channel` | (prompt) | Channel ID |
| `--res` | `360p` | Resolution |
| `--pmt` | `true` | Inspect PMT (SCTE-35 presence, fingerprint, consistency) |
| `--tsp` | `false` | Also run `tsp -P psi` for raw PMT + CUEI registration check |
| `--expect-lang` | `PMT_AUDIO_LANGUAGE`/`eng` | Expected audio language; warns on mismatch |

### `cmd/scte35-check` (HTTP watcher)

Fetches live HLS playlists via HTTP and runs TSDuck on each segment. Labels segments as `[Preroll]`, `[CueOut]`, `[Ads]`, `[VOD]` based on TYPE= tags.

| Flag | Default | Description |
|------|---------|-------------|
| `--api` | `http://localhost:8080` | API base URL |
| `--channel` | `f1533ebe-...` | Channel ID |
| `--res` | `360p` | Resolution |

### `cmd/hls-watch` (HLS watcher)

Full HLS segment analysis with SCTE-35, PTS, and PMT inspection. Detects segmentation descriptors and reports event type, event ID, duration, immediate flag, and segmentation type.

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | (hardcoded) | Full m3u8 URL |

## Expected Results Summary

| Check | Pass Criteria |
|-------|---------------|
| SCTE-35 in PMT | stream_type 0x86 present in 100% of segments |
| PMT consistency | Same fingerprint across all segments (including ad boundaries) |
| CUEI registration | Registration descriptor 0x43554549 present |
| Pre-roll (cue-out) | TYPE=48 on 2 content segments before break |
| Cue-out | TYPE=52 on first ad segment, immediate splice |
| Placement-end | TYPE=54 on ad segment at break end − 4s |
| Cue-in | TYPE=53 on first content segment after break, immediate splice |
| Audio language | eng on all audio streams |
| PTS timing | No gaps, range ≥ duration − 0.5s |
| Error handling | Retry once, log on failure, skip segment |

## Troubleshooting

- **No segments appearing**: Ensure engines are running (`make start-engines`), check `logs/engines.log`
- **tsp fails with signal: killed**: TSDuck timeout too short — check `context.WithTimeout` in the engine code
- **rclone copyto error**: Verify object storage is accessible (`rclone ls partner-video-lib:`)
- **Verifier shows no output**: Ensure the channel has active segments in the playlist
- **tsp --no-warnings not recognized**: tsp 3.44 removed these flags; tools have been updated to omit them
- **Pre-roll not detected**: Ensure playlist contains `EXT-X-SCTE35:CUE` tags with `TYPE=48`; if missing, check planner's `stampPreRollCues` and `PreRollDelaysSec`
- **Placement-end not detected**: Ensure `processAdBreak` stamps `SEG_TYPE_PLACE_OPPORTUNITY_END` at `breakEnd - 4`; check `PreRollCUEINDelaysSec` (currently inlined on main)
- **Segmentation type id differs from table**: Verification pass criteria key off HLS `TYPE=` tags and splice attributes (immediate/out-of-network/duration), not the SCTE-35 segmentation type id. The table above reflects the actual stream (cue-out=0x34, cue-in=0x35); do not fail a run solely on seg-type id mismatch.