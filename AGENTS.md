# AGENTS.md — SimLive-Go

Instructions for AI agents (and humans) working in this repository. Read this
before editing code, and consult `docs/HANDOFF.md` for the full problem
history and evidence.

## What this project is

A **simulated-live HLS playout system** in Go:

- **Planner** (`cmd/planner` → `internal/planner/`) — reads the EPG schedule
  from the ESPX API, combines VOD content m3u8 + ads m3u8 (`rclone`), and
  builds a full-day "simulated live" timeline into a Redis sorted set
  (`simlive:timeline:{channelID}`, score = MediaStart seconds).
- **Engine** (`cmd/engines` → `internal/engine/`) — runs a 1s tick loop, reads
  a sliding window from Redis, downloads segments to `./outputs/{channelID}/`,
  rewrites their PTS/DTS so playback is continuous across VOD↔ADS boundaries,
  and writes a 5-segment sliding `playlist.m3u8`.
- **API** (`cmd/api` → `internal/api/`) — serves the playlist/segments at
  `http://localhost:8080/live/{channelID}/playlist.m3u8`.
- **Models / config / storage / utils** — `internal/models/`,
  `internal/configs/`, `internal/storage/` (rclone), `internal/utils/`
  (env/times). `ai-sources/` holds the AI-generated reference code the
  internal packages were derived from.

The channel in use for testing: `7261da4b-9c45-4619-bfb0-a6119dc7b6e6`
(Playout 09). The live playlist:
`http://localhost:8080/live/7261da4b-9c45-4619-bfb0-a6119dc7b6e6/playlist.m3u8`

## Architecture (one paragraph each)

```
planner.go: ESPX schedules -> VOD m3u8 + ads m3u8 (rclone cat) -> Redis ZSET
            simlive:timeline:{channelID}  (each entry = SegmentItem JSON)
engine.go:  1s tick -> ZRangeByScore window -> download segs + PTS rewrite ->
            playlist.m3u8 (sliding 5 segs, only advertises prepared files)
```

- **Planner** (`internal/planner/planner.go`): for each schedule item, fetch
  the VOD m3u8 of the resolved rendition dir, parse `#EXTINF` +
  `#EXT-X-PTS-RANGE`, assign each segment a local `PtsOffset`
  (`MediaStart − timelineBase`), and insert 5-sec ad blocks every
  `AdInsertionInterval` (30s) seconds. Writes are pipelined in batches.
- **Engine** (`internal/engine/engine.go`): picks the window of segments
  containing "now", downloads missing files with an in-flight dedupe map,
  rewrites each `.ts` once with ffmpeg `-copyts -output_ts_offset`
  (`seg_N.ts.pts` marker = prepared), and writes the playlist only from
  prepared segments. Downloads run async so the 1s tick never blocks → the
  window slides exactly +1 segment per ~4s.
- **PTS rewrite** (`rewritePtsOffset`): target = `seg.PtsOffset` (local
  timeline) or `MediaStart` (legacy). First video DTS is probed with ffprobe;
  offset = target − firstDTS. ffmpeg remux `-c copy -copyts -muxdelay 0
  -muxpreload 0 -output_ts_offset <offset> -f mpegts`. No re-encode. Fallback:
  anchor on `seg.PtsStart`; if that fails too, keep the raw file but still
  mark prepared (never stall the window).

## Quality tiers (SIMLIVE_QUALITY) — current: 360p test mode

`SIMLIVE_QUALITY` = `auto` (default) | `360p` | `720p` | `1080p`.

Verified rendition map (VOD dir → ads dir):

| Quality | VOD dir | ADS dir |
|---------|---------|---------|
| 1080p   | `L_2`   | `L0`    |
| 720p    | `L_1`   | `L1`    |
| 360p    | `L_0`   | `L2`    |

Selection logic (`internal/planner/planner.go`):

- `resolveRenditions(streamID)` — VOD side: (1) exact pair VOD==ADS at target
  resolution; (2) closest-to-target with VOD==ADS; (3) min-area-diff pair.
  Logs the chosen pair.
- `resolveAdsRendition()` — ads side (used by `loadAdSegments` for the REAL
  ads paths — `pair.ads` in `resolveRenditions` is log-only!). Hard override
  `SIMLIVE_ADS_RENDITION` > tier-closest > tier fallback > largest (auto).
- Hard overrides `SIMLIVE_VOD_RENDITION` + `SIMLIVE_ADS_RENDITION` (both set)
  beat the tier.
- Probe-failure fallback map: 360p→`L_0`/`L2`, 720p→`L_1`/`L1`,
  1080p→`L_2`/`L0`.

**VOD and ads must always resolve to the SAME resolution** — a mid-stream
resolution change in one MSE SourceBuffer fails playback even with continuous
PTS. Keep this invariant when changing rendition logic.

## Key environment variables (`.env`)

| Variable | Default | Purpose |
|----------|---------|---------|
| `SIMLIVE_QUALITY` | `auto` | `360p`/`720p`/`1080p` tier (currently `360p`) |
| `SIMLIVE_VOD_RENDITION` / `SIMLIVE_ADS_RENDITION` | (empty) | Hard rendition overrides |
| `DB_*` | localhost/postgres | Postgres (`playout_channels`) |
| `REDIS_ADDR` | `localhost:6371` | Redis (note: 6371, not 6379) |
| `ESPX_API_URL` | `https://api2.espx.cloud` | EPG API |
| `TMP_DIR` | `./outputs` | Engine local segment cache |
| `MAX_WORKERS` / `WORKER_POOL_SIZE` | 50 | Download concurrency |
| `ENABLE_CLEAN_UP` | `false` | Cache cleanup toggle |

## Commands

```bash
# Build (GOCACHE must be writable — use workspace-local cache)
GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...

# Planner — DESTRUCTIVE: FlushAll Redis + wipes ./outputs, rebuilds timeline
GOCACHE=$PWD/.gocache go run ./cmd/planner/main.go

# Engine (long-running)
GOCACHE=$PWD/.gocache go run ./cmd/engines/main.go

# API server (long-running)
GOCACHE=$PWD/.gocache go run ./cmd/api/main.go
```

### Restart procedure (killing is fiddly)

`go run` children (the compiled `exe/main` binaries) can survive `kill` of the
parent. Always check before and after:

```bash
ps -e -o pid,ppid,cmd | grep -E "exe/main|go run ./cmd"
```

Kill the whole engine tree: kill the `go run` PID **and** the `exe/main` PID
(and any bwrap wrapper PIDs if the engine was started inside a sandbox).

## Verification workflow (after any planner/engine change)

1. Planner log must show the tier + pair:
   `[Planner] quality tier=360p, stream <id> -> renditions: VOD L_0 + ads L2`.
2. Redis paths: `redis-cli -p 6371 zrange 'simlive:timeline:<channelID>' 0 5`
   → VOD `/L_0/`, ads `/ads-v3/L2/`, every entry has `pts_offset`.
3. Playlist slides exactly +1 per ~4s: curl the playlist twice ~12s apart and
   compare `#EXT-X-MEDIA-SEQUENCE`.
4. Boundary DTS continuity (both directions). **Copy the segment to `/tmp`
   first** — ffprobe hangs on files being rewritten by the engine:
   ```bash
   cp outputs/<channelID>/seg_242.ts /tmp/x.ts
   timeout 15 ffprobe -v error -select_streams v:0 -show_entries packet=dts_time \
     -of csv=p=0 -read_intervals "%+#1" /tmp/x.ts | tr -d ',\n'
   ```
   Consecutive segments should differ by ~+4s (segment duration), never jump
   backward/forward by hundreds of seconds.
5. End-to-end playback probe (`probe/`): start `python3 probe/probe_server.py
   8897` (writes `probe/result.txt`), then:
   ```bash
   chromium-browser --headless=new --no-sandbox --disable-gpu \
     --disable-web-security --autoplay-policy=no-user-gesture-required \
     --user-data-dir=/tmp/ct "http://localhost:8897/player-test.html?t=120"
   ```
   Success looks like: `errors=[]`, `seq jumps=0`, `stalled=no`, `res=640x360`
   (for 360p), `currentTime≈t`.

## Gotchas / debugging tips

- **ffprobe resolution output prints TWICE** on these MPEG-TS files — take the
  first line only. `packet=dts_time` csv appends a trailing comma — strip it
  before `ParseFloat`.
- **`ffprobe -read_intervals "%+#1"` hangs** on a segment the engine is
  concurrently remuxing — always probe a `/tmp` copy.
- Rendition probing downloads each candidate's first segment at planner
  runtime (needs network + `rclone` + `ffprobe`). Missing renditions (e.g.
  `L_3`) fail the probe gracefully and are skipped — that's expected.
- The engine deletes `TMP_DIR` (`./outputs`) at startup — never keep test
  artifacts there.
- Shell is **fish** on this machine: `if ... else ... end`, `for x in ...;
  ...; end`, `or` for fallbacks. Don't use bash `fi`/`else`/`done`.
- **Don't restart the planner unless the timeline must be rebuilt** — it wipes
  Redis and all outputs. The engine can be restarted alone (it re-reads
  Redis); expect a brief cold-start while segments re-download (a single early
  `bufferStalledError` after restart is a known artifact, it recovers).
- PTS offset wraps at 2^33/90000 ≈ 95443.7s for huge epoch offsets — the
  local `PtsOffset` design avoids this entirely; keep it local.
- Comments in this codebase are a mix of English and Vietnamese — keep the
  style of the file you're editing.

## When the user asks to change quality tier

1. Set `SIMLIVE_QUALITY` in `.env` (e.g. `720p`).
2. Kill the engine tree (see restart procedure).
3. Re-run the planner (destructive rebuild), then the engine.
4. Verify per the workflow above — Redis paths must switch to the new tier's
   dirs (e.g. `/L_1/` + `/ads-v3/L1/`) and the probe must show the new
   resolution with zero errors.

## Docs map

- `docs/HANDOFF.md` — full problem history, root causes with evidence, current
  status, and gotchas (most detailed source).
- `docs/specification.md` — architecture spec + env var table + module
  summary.
- `docs/concept.md` — one-paragraph concept (very terse).
