# Implementation: PTS Sliding Window Logic — 2026-08-27

> **Status:** ✅ Implemented, verified end-to-end in **360p** mode.
> **Commit:** `56d606a fix: pts offset + sliding windows`
> **Scope:** Planner (local PtsOffset timeline + 3-tier quality) · Engine (PTS/DTS
> rewrite + sliding 5-seg window) · Models · `.env` · `probe/` harness · docs.
> **Docs:** `docs/specification.md` (architecture) · `AGENTS.md` (agent instructions)

---

## 1. Problem Statement

Before this change the simulated-live stream broke at **VOD ↔ ads boundaries**:

- Players (hls.js + Chrome MSE) threw `Parsed buffers not in DTS sequence` and
  `bufferStalledError` when the timeline jumped from a VOD segment to an ad
  segment (or back).
- Each `.ts` came from a different source manifest with its **own PTS/DTS
  domain** — concatenating them raw produced huge DTS jumps (hundreds of
  seconds) at boundaries.
- The engine downloaded segments synchronously inside the tick, so the window
  stalled and the playlist sometimes jumped 2–3 indices at once.
- Playback depended on absolute epoch PTS, which wraps at 2^33/90000 ≈
  95443.7s for large offsets.

## 2. Solution Overview

Three coordinated changes make playback continuous across boundaries:

1. **Planner — local timeline.** Every segment is assigned
   `PtsOffset = MediaStart − timelineBase` (seconds), where `timelineBase` is
   the `MediaStart` of the first segment. All offsets are therefore **small and
   local** (no epoch wrap), and VOD + ads live on one continuous timeline.
2. **Engine — PTS/DTS rewrite.** Each downloaded `.ts` is remuxed once with
   ffmpeg `-c copy -copyts -muxdelay 0 -muxpreload 0 -output_ts_offset
   <target − firstVideoDTS>` so the first video DTS lands exactly on the
   segment's `PtsOffset`. DTS stays continuous across every VOD↔ads boundary.
3. **Engine — non-blocking sliding window.** A 1s tick selects a 5-segment
   window containing "now" (with auto-catchup), downloads/rewrites missing
   segments in **background goroutines** (in-flight dedupe + `.pts` marker),
   and writes the playlist **only from prepared segments**. The window slides
   exactly +1 segment per ~4s with no 404s.

## 3. What Changed (by file)

### `internal/planner/planner.go` (+616 lines)

| Area | Change |
|------|--------|
| `PtsOffset` timeline | `SegmentProcessor` gains `timelineBase` + `currentPtsOffset`; every segment (VOD **and** ad) gets `PtsOffset = MediaStart − timelineBase` via `setTimelineBase()` |
| `#EXT-X-PTS-RANGE` | Manifests parsed for raw `pts_start`/`pts_end` (90 kHz); shifted by `currentPtsOffset` → `media_start`/`media_end` (seconds) |
| Quality tiers | `SIMLIVE_QUALITY` (`auto` default \| `360p` \| `720p` \| `1080p`) with `qualityTierMap` (640×360 / 1280×720 / 1920×1080) and `fallbackRenditionForTier` (360p→`L_0`/`L2`, 720p→`L_1`/`L1`, 1080p→`L_2`/`L0`) |
| Rendition matching | `resolveRenditions()`: explicit VOD+ADS env overrides > exact VOD==ads pair at tier > closest VOD==ads pair > min-area-diff pair; `auto` = highest equal-resolution pair |
| Ads rendition | `resolveAdsRendition()` (cached once): `SIMLIVE_ADS_RENDITION` > tier-closest > tier fallback > largest (auto). **Note:** `pair.ads` from `resolveRenditions()` is log-only — real ads paths come from `resolveAdsRendition()` |
| Ad insertion | `insertAdsIfNeeded()` — 5-sec ad block every `AdInsertionInterval` (30s), ads placed on the same local timeline |
| Rendition probing | `probeResolution()`/`probeRenditions()` download first segment → ffprobe w×h (missing renditions e.g. `L_3` fail gracefully) |

### `internal/engine/engine.go` (+320 lines)

| Area | Change |
|------|--------|
| Tick | `time.NewTicker(1 * time.Second)` (was 2s) |
| Window | `ZRangeByScore [now−300, now+120]` → sorted by sequence → 5-seg candidate window containing "now" |
| Auto-catchup | If "now" is past the last segment → play the final window; if future/out-of-sync → closest `MediaStart` |
| Async pipeline | `downloadAndPrepareSegments()` runs in a goroutine; `inFlight` map prevents duplicate download/rewrite across ticks; worker pool bounds concurrency |
| `prepareSegment()` | Download via `rclone copyto` if missing → `rewritePtsOffset()` → `.pts` marker (idempotent) |
| `rewritePtsOffset()` | Probe first video DTS (ffprobe, 10s timeout) → `offset = target − firstDTS` → ffmpeg remux `-c copy -copyts -muxdelay 0 -muxpreload 0 -output_ts_offset <offset> -f mpegts` (20s timeout). **Target priority:** `PtsOffset` (local) → `MediaStart` (legacy) → `PtsStart` anchor; if all fail, keep raw file but still mark prepared (window never stalls) |
| `probeFirstVideoDTS()` | `ffprobe -select_streams v:0 -show_entries packet=dts_time -of csv=p=0 -read_intervals "%+#1"`; strips the trailing csv comma |
| Playlist | `generateHLSPlaylist()` advertises **only** `.pts`-prepared segments; atomic write via `playlist.m3u8.tmp` + rename; keeps previous playlist while window warms up |
| Cleanup | `syncLocalCacheDir()` removes `.ts` + `.pts` outside the active window |

### `internal/models/models.go`

`SegmentItem` extended with PTS timeline fields:

```go
PtsStart   int64    `json:"pts_start"`   // raw #EXT-X-PTS-RANGE start (90 kHz)
PtsEnd     int64    `json:"pts_end"`     // raw #EXT-X-PTS-RANGE end (90 kHz)
MediaStart float64  `json:"media_start"` // seconds (PtsStart/90000 + offset)
MediaEnd   float64  `json:"media_end"`   // seconds
PtsOffset  *float64 `json:"pts_offset,omitempty"` // local timeline position (MediaStart − timelineBase); nil on legacy entries
```

### `.env`

- Added `SIMLIVE_QUALITY=360p` (test mode). `auto` remains the code default.

### `probe/` (new verification harness)

- `probe_server.py` — serves the player page on port 8897, writes `probe/result.txt`.
- `player-test.html` — hls.js 1.5.13 test player; reports `errors`, `seq jumps`,
  `stalled`, `res`, `currentTime` after a `?t=<seconds>` run.

### Docs

- `docs/specification.md` — rewritten to match the implemented system (§6.1
  quality tiers, PTS fields, corrected constants/env vars).
- `AGENTS.md` — created at repo root (architecture, commands, verification
  workflow, gotchas).
- `docs/implement-pts-sliding-window-logic-27-08.md` — this file.

### English-only comments (uncommitted, working tree)

- All Vietnamese comments **and log strings** translated to English across 8
  files: `internal/{engine,planner,models,storage,api}`, `cmd/{engines,planner}`,
  plus `ai-sources/*.go`. Verified by diacritic grep (0 matches) + `go build` (OK).

## 4. Data Flow

```text
ESPX API schedules
      │
      ▼
Planner: resolveQualityTier → resolveRenditions (VOD L_0 + ads L2)
      │  parse #EXTINF + #EXT-X-PTS-RANGE
      │  PtsOffset = MediaStart − timelineBase   (VOD + ads, local seconds)
      ▼
Redis ZSET simlive:timeline:{channel}   (score = media_start, member = SegmentItem JSON)
      │
      ▼
Engine 1s tick:
  ZRangeByScore [now−300, now+120] → 5-seg window (auto-catchup)
      │
      ├─ async download (in-flight dedupe)
      ├─ rewritePtsOffset: ffmpeg -c copy -copyts -output_ts_offset (target − firstDTS)
      ├─ .pts marker = prepared
      └─ playlist.m3u8 ← prepared segs only   →  API :8080/live/{channel}/playlist.m3u8
```

## 5. Quality Tier System (SIMLIVE_QUALITY)

| Tier | Target | VOD dir | Ads dir |
|------|--------|---------|---------|
| 360p  | 640×360  | `L_0` | `L2` |
| 720p  | 1280×720 | `L_1` | `L1` |
| 1080p | 1920×1080| `L_2` | `L0` |

Selection priority (planner): `SIMLIVE_VOD_RENDITION`+`SIMLIVE_ADS_RENDITION`
(hard overrides) > tier pair (exact VOD==ads → closest VOD==ads → min area
diff) > tier fallback map > auto (largest equal-resolution pair).

> **Invariant:** VOD and ads must resolve to the **same resolution** — a
> mid-stream resolution change in one MSE SourceBuffer fails playback even with
> continuous PTS.

## 6. Verification (360p, 2026-08-27)

| Check | Result |
|-------|--------|
| Planner log | `quality tier=360p, stream … -> renditions: VOD L_0 + ads L2` ✅ |
| Redis entries | VOD `/L_0/`, ads `/ads-v3/L2/`, every entry has `pts_offset` ✅ |
| Ad pattern | 5-sec AD block (seg 160–164) then VOD (165–172) then AD (173–177) … every ~30s ✅ |
| Playlist slide | `#EXT-X-MEDIA-SEQUENCE` 85→88 over 12s = exactly +1 per ~4s ✅ |
| Boundary DTS (VOD→AD) | seg_172 677.80 → seg_173 681.72 (+3.92 ✓) |
| Boundary DTS (AD→VOD) | seg_242 954.66 → seg_243 958.67 (+4.00 ✓) |
| Headless Chromium 120s | `currentTime=119.84s`, fragments=35, `seq jumps=0`, `errors=[]`, `stalled=no`, `res=640x360` ✅ |

## 7. How to Re-Verify

1. Planner log shows the tier + pair (see §6).
2. `redis-cli -p 6371 zrange 'simlive:timeline:<channel>' 0 5` → VOD `/L_0/`,
   ads `/ads-v3/L2/`, `pts_offset` present.
3. Playlist slides exactly +1 per ~4s: compare `#EXT-X-MEDIA-SEQUENCE` twice,
   ~12s apart.
4. Boundary DTS — **copy the segment to `/tmp` first** (ffprobe hangs on files
   the engine is rewriting):
   ```bash
   cp outputs/<channel>/seg_172.ts /tmp/x.ts
   timeout 15 ffprobe -v error -select_streams v:0 -show_entries packet=dts_time \
     -of csv=p=0 -read_intervals "%+#1" /tmp/x.ts | tr -d ',\n'
   ```
   Consecutive segments should differ by ~+4s.
5. End-to-end probe:
   ```bash
   python3 probe/probe_server.py 8897   # writes probe/result.txt
   chromium-browser --headless=new --no-sandbox --disable-gpu \
     --disable-web-security --autoplay-policy=no-user-gesture-required \
     --user-data-dir=/tmp/ct "http://localhost:8897/player-test.html?t=120"
   ```
   Success: `errors=[]`, `seq jumps=0`, `stalled=no`, `res=640x360`, `currentTime≈t`.

## 8. Known Artifacts / Remaining Work

- **Cold-start stall:** after an engine restart a single early
  `bufferStalledError` is a known artifact while the window re-downloads; it
  recovers. Hardening this (pre-download the first window before advertising)
  is optional future work.
- **720p / 1080p tiers** use the same code path (env-driven) but have not been
  exercised end-to-end yet.
- **Restart discipline:** the planner is **destructive** (FlushAll Redis + wipes
  `./outputs`) — never restart it unless the timeline must be rebuilt. The
  engine can be restarted alone.
- `docs/HANDOFF.md` no longer exists in this workspace — `AGENTS.md` +
  `docs/specification.md` + this file are the canonical references.
