---
description: "SimLive-Go project summary. Use when implementing or changing Go code in this repo — keep this summary in sync with the code. Covers architecture, modules, quality tiers, env vars, and the post-task doc update rule. Triggers: summary, simlive, playout, planner, engine, timeline."
applyTo: "**/*.go"
---
# SimLive-Go Project Summary

> Canonical details live in `docs/specification.md` and `AGENTS.md` — do not
> duplicate them here; keep this page as a concise snapshot that is always in
> sync with the code.

## What this is
A simulated-live (24/7 linear) HLS playout system in Go:
**Planner** (ESPX EPG schedules + `rclone` manifests → Redis timeline) →
**Engine** (1s tick → download + PTS/DTS rewrite → sliding HLS playlist) →
**API** (serves the playlist/segments at `http://localhost:8080/live/{channelID}/`).

## Architecture snapshot (keep in sync)
- **Planner** → `simlive:timeline:{channelID}` Redis ZSET; score = `MediaStart`
  (seconds), member = `SegmentItem` JSON incl. local `pts_offset`. 24/7 cron
  walks **today + tomorrow** in each channel's `content_timezone` per tick
  (`PLANNER_INTERVAL`, default 5m); per-date `horizon` (EndOfDay of the parsed
  date) lets a single `ProcessEPGToTimeline` call walk past midnight.
  `HasDateInTimeline` (ZCOUNT `[dayStart, dayEnd] > 0`) short-circuits
  re-prepare of a date that already has data.
- **Engine** → window `[now−300s, now+120s]`; ffmpeg `-c copy -copyts
  -output_ts_offset` rewrite; `.pts` marker = prepared; 5-segment sliding
  `playlist.m3u8` advertises prepared segments only.
- **Invariants:** VOD↔ads must resolve to the SAME resolution; `PtsOffset`
  stays local (`MediaStart − timelineBase`) to avoid the 2^33/90000 ≈ 95443.7s
  epoch PTS wrap.

## Quality tiers (`SIMLIVE_QUALITY`)
| Tier | VOD dir | ADS dir |
|------|---------|---------|
| 360p | `L_0` | `L2` |
| 720p | `L_1` | `L1` |
| 1080p | `L_2` | `L0` |

## Key env vars
`SIMLIVE_QUALITY` (auto default), `SIMLIVE_VOD_RENDITION` /
`SIMLIVE_ADS_RENDITION` (hard overrides), `REDIS_ADDR` (6371), `TMP_DIR`
(./outputs), `MAX_WORKERS` (50), `ENABLE_CLEAN_UP` (false),
`SIMLIVE_KEEP_BEHIND` (5), `SIMLIVE_DOWNLOAD_AHEAD` (5). Full table in
`docs/specification.md` §8.

## Operational Details
- **Bounded on-disk window:** the engine keeps at most
  `SIMLIVE_KEEP_BEHIND + 5 + SIMLIVE_DOWNLOAD_AHEAD` `.ts` files (and the same
  number of `.pts` markers) per channel on disk (default 15). This bounded
  storage ensures low disk usage and fast startup cleanup. The Redis timeline
  window (`[-300s, +120s]`) is still queried for awareness, but only the
  active window is downloaded/kept; everything else is evicted each tick.
- **Planner dedup:** `prepareDate(ch, date)` calls `HasDateInTimeline` first;
  the cron only re-walks EPG when `ZCOUNT simlive:timeline:{ch} [start,end]`
  is zero. Cold start fills today + tomorrow (~1–2 min); subsequent ticks
  skip both dates.

## Tests
- `internal/planner/horizon_rollover_test.go` — `TestIsEndOfTimeline_...` (pure
  Go) + `TestHasDateInTimeline_...` (skipped when no Redis at `REDIS_ADDR`).

## Post-task rule
After finishing any code change, update this summary and
`docs/specification.md` for everything the change affects (models, env vars,
constants, architecture, quality tiers) — and nothing else.

## Additional env vars
- `PLANNER_INTERVAL` (default 5m), `RETENTION_SECONDS` (7200), `LOOKAHEAD_SECONDS` (7200) — 24/7 continuous cursor + cleanup.
- Cursor: `planner:cursor:{channelID}`; cleanup: `ZREMRANGEBYSCORE` with `RETENTION_SECONDS`.
