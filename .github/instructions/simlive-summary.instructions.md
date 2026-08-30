---
description: "SimLive-Go project summary. Use when implementing or changing Go code in this repo — keep this summary in sync with the code. Covers architecture, modules, quality tiers, env vars, and the post-task doc update rule. Triggers: summary, simlive, playout, planner, engine, timeline."
applyTo: "**/*.go"
---
# SimLive-Go Project Summary

> Canonical details live in `docs/specification.md` — do not duplicate
> them here; keep this page as a concise snapshot that is always in sync
> with the code. (`AGENTS.md` does not exist at the repo root; this file is
> the single canonical agent instruction.)

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
- `internal/planner/retry_breaker_test.go` — `TestFailureCounter_SuccessResetsZero`
  (Redis), `TestRetryBackoffDelay_Schedule` (pure), `TestMarkChannelInactive_RespectsDB`
  (pure, nil-db no-op).

## Post-task rule
After finishing any code change, update this summary and
`docs/specification.md` for everything the change affects (models, env vars,
constants, architecture, quality tiers) — and nothing else.

## Additional env vars
- `PLANNER_INTERVAL` (default 5m), `PLANNER_WORKERS` (default 4) — cron loop + per-tick channel concurrency.
- `RETENTION_SECONDS` (7200), `LOOKAHEAD_SECONDS` (7200) — 24/7 continuous cursor + cleanup.
- `MAX_PREPARE_RETRIES` (3), `PREPARE_RETRY_BASE_MS` (500), `PREPARE_RETRY_MAX_MS` (8000) — bounded exponential backoff for `ProcessEPGToTimeline`.
- Cursor: `planner:cursor:{channelID}`; cleanup: `ZREMRANGEBYSCORE` with `RETENTION_SECONDS`.
- Per-channel consecutive-failure counter: `planner:failures:{channelID}` (TTL 24h). After 5 consecutive `(channel, date)` failures the channel is flipped to `is_active=false` by the cron loop so a broken upstream stops burning CPU/EPG bandwidth.

## Resilience & observability (planner)
- **Worker pool:** active channels are processed in parallel by `PLANNER_WORKERS` goroutines (semaphore + WaitGroup) — bounded so a 50-channel fleet doesn't open 50 simultaneous EPG fetches.
- **Retry/backoff:** `ProcessEPGToTimelineWithRetry` wraps the raw EPG walk; schedule is `base * 2^(i-1)` capped at `PREPARE_RETRY_MAX_MS`, attempts = `MAX_PREPARE_RETRIES`. On final failure the channel's `planner:failures:{id}` counter is incremented via `HandlePrepareResultForChannel`.
- **Breaker:** once the counter ≥ `failureInactivateThreshold` (5) the channel is set `is_active=false`. A successful prepare (or a "date already prepared" short-circuit) resets the counter to 0.
- **Metrics:** each tick logs `[METRIC] channel={id} name=... zset_size={n} cursor={ts}` per active channel (via `TimelineSize` + `LastProcessedCursor`); greppable for Loki/etc.
