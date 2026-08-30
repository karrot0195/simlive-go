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
**API** (serves the playlist/segments at `http://localhost:8080/live/{channelID}/` and the playout-channel CRUD + start/stop surface at `http://localhost:8080/api/channels[/...]` — handlers in `internal/api/handler.go`). The planner reads the same `is_active` / `status` columns, so a manual `start` / `stop` flip takes effect on the next planner tick (default 5m, `PLANNER_INTERVAL`). `start` also deletes the breaker counter key `planner:failures:{id}` to reset the per-channel failure breaker.

## Architecture snapshot (keep in sync)
- **Planner** → `simlive:timeline:{channelID}` Redis ZSET; score = `MediaStart`
  (seconds), member = `SegmentItem` JSON incl. local `pts_offset`, `rendition`
  (omitempty), and the four ad-break fields `is_ad / ad_break_id /
  ad_break_start / ad_break_duration` (all omitempty so legacy VOD members
  round-trip unchanged). 24/7 cron walks **today + tomorrow** in each
  channel's `content_timezone` per tick (`PLANNER_INTERVAL`, default 5m);
  per-date `horizon` (EndOfDay of the parsed date) lets a single
  `ProcessEPGToTimeline` call walk past midnight. `HasDateInTimeline` (ZCOUNT
  `[dayStart, dayEnd] > 0`) short-circuits re-prepare of a date that already
  has data. **M1 upgrade guard:** `HasLegacyTimeline` checks the first ZSET
  member for `rendition`; if absent, `DeleteTimeline` flushes so members are
  repopulated with the new tag.
- **Planner → `simlive:renditions:{channelID}`** (M1.5): JSON array of
  `RenditionMeta` (rendition, vod_dir, ads_dir, width, height, bandwidth)
  for **all 3 rungs** (360p / 720p / 1080p). Written by `writeRenditionMetadata`
  after each pipeline flush. Consumed by the engine to build the master
  playlist without runtime probing.
- **Engine** → window `[now−300s, now+120s]`; ffmpeg `-c copy -copyts
  -output_ts_offset` rewrite; `.pts` marker = prepared. **M1.5: shared
  segment cache** -- ONE flat set of `seg_N.ts` files in
  `{TMP_DIR}/{ch}/` is downloaded from the active rung's VOD dir and
  shared across all 3 rung playlists. The engine writes:
    - 1 master `playlist.m3u8` (3 `#EXT-X-STREAM-INF` lines +
      `#EXT-X-INDEPENDENT-SEGMENTS`) → Redis `simlive:hls:{ch}`
    - 1 per-rung media playlist `playlist_360p.m3u8` / `_720p` / `_1080p`
      (each references the same shared `seg_N.ts` files) → Redis
      `simlive:hls:{ch}:rendition:{rend}`. The `groupSegsByRendition`
  helper rescues empty `Rendition` to `DefaultRendition = "360p"`. The
  per-channel ladder is resolved via `activeRungMeta` (planner metadata
  → hard-coded `ActiveLadder` fallback) so the master always lists 3
  rungs even before the planner has run.
  - **SCTE-35 cue tags:** per-rung media playlists also emit
    `#EXT-X-DATERANGE` lines on VOD<->ad transitions (CUE-OUT before the
    first ad EXTINF; CUE-IN before the first VOD EXTINF after the break).
    Tag format mirrors `tmp/SCTE35Utils.ts`; DURATION is unquoted and
    trailing (threefive TagParser gotcha) and capped at 3 decimal places
    (matches `#EXTINF:%.3f`; FP-noise tails like `20.019999999999996`
    are rounded to `20.02`). The base64 `SpliceInfoSection` is built in
    Go with the `Comcast/scte35-go` library (`internal/engine/scte35.go`:
    `buildSpliceInsert`, `buildCueOutLine`, `buildCueInLine`,
    `buildMediaPlaylistBody`, `adBreakEventID`, `formatBreakStartDate`,
    `formatBreakDuration`). Plan:
    [300826_0616-implement-scte35-cue-markers.md](../docs/300826_0616-implement-scte35-cue-markers.md).
- **Invariants:** VOD↔ads must resolve to the SAME resolution; `PtsOffset`
  stays local (`MediaStart − timelineBase`) to avoid the 2^33/90000 ≈
  95443.7s epoch PTS wrap.

## Quality tiers (`SIMLIVE_QUALITY`)
| Tier | VOD dir | ADS dir |
|------|---------|---------|
| 360p | `L_0` | `L2` |
| 720p | `L_1` | `L1` |
| 1080p | `L_2` | `L0` |

## Key env vars
`SIMLIVE_QUALITY` (auto default), `SIMLIVE_RENDITIONS` (M1: exactly 1 rung
allowed; multi-rung values are WARN-logged once per process and the first
entry is used. M1.5 ignores the multi-rung case because the engine serves
ONE shared segment cache and always advertises 3 rungs via `ActiveLadder`),
`SIMLIVE_VOD_RENDITION` / `SIMLIVE_ADS_RENDITION` (hard overrides),
`REDIS_ADDR` (6371), `TMP_DIR` (./outputs), `MAX_WORKERS` (50),
`ENABLE_CLEAN_UP` (false), `SIMLIVE_KEEP_BEHIND` (5),
`SIMLIVE_DOWNLOAD_AHEAD` (5). Full table in `docs/specification.md` §8.

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
- `internal/planner/rendition_test.go` — `TestActiveRenditions_...` (pure),
  `TestProbeBitrateFallback_Formula` (pure), `TestParseBitrateStdout_...`
  (pure), `TestWriteRenditionMetadata_SerializesJSON` (Redis),
  `TestHasLegacyTimeline_ReportsAndSkips` (Redis).
- `internal/models/models_test.go` — `TestSegmentItem_Rendition_...` (pure):
  omitempty, roundtrip, legacy-unmarshal rescue, multi-rung isolation.
  `internal/models/scte35_fields_test.go` — `TestSegmentItem_AdFields_...`
  (pure): omitempty for the four ad-break fields, roundtrip, legacy rescue.
- `internal/planner/scte35_stamping_test.go` — `TestAdBreakIDFor_...` (pure:
  format / stability / distinctness of `adbreak-<int64(breakStart)>`),
  `TestTotalBreakDuration_...` (pure: empty / 5×6s pool / fractional),
  `TestStampAdBreak_OnSegmentItem_LocksContract` (pure: every ad in the
  break carries the same 4 stamped fields), `TestStampedSegmentItem_JSONRoundTrip`
  (pure: marshal/unmarshal preserves the 4 fields), and
  `TestInsertAdsIfNeeded_StampsAllAdsWithBreakMetadata` (Redis-gated:
  drives real `insertAdsIfNeeded` against `*redis.Client`, ZRANGEs the
  ZSET back, asserts every member carries the break metadata).
- `internal/engine/scte35_test.go` — `TestBuildSpliceInsert_Out/In/Duration`
  (pure; round-trips through `scte35.DecodeBase64` and asserts
  `OutOfNetworkIndicator`, `SpliceImmediateFlag`, `BreakDuration`,
  90kHz ticks, CRC32, `Tier:4095`), `TestAdBreakEventID_Stable`,
  `TestDecodeSpliceInsert_BadInput`, `TestBuildCueOutLine_Format` /
  `TestBuildCueInLine_Format` (lock in the exact DATERANGE line shape),
  `TestFormatBreakDuration_PrecisionCappedAt3` / `_NeverMoreThan3Decimals`
  (lock the DURATION precision at 3 decimal places so FP noise like
  `20.019999999999996` never reaches the playlist; trailing zeros
  + lone dot are stripped for clean output like `DURATION=30` /
  `DURATION=20.02` / `DURATION=0.3`).
- `internal/engine/cue_tag_test.go` — `TestBuildMediaPlaylistBody_...`
  (pure): no-ads shape, CUE-OUT before first ad EXTINF, CUE-IN between
  last ad and first post-ad VOD, cold-start with ad as first segment,
  multiple breaks, empty input error, trailing-unquoted-attr gotcha,
  SegmentItem roundtrip.
- `internal/engine/master_playlist_test.go` — `TestBuildMasterPlaylist_...`
  + `TestBuildMasterPlaylist_ThreeRungs` (pure),
  `TestActiveLadder_HasThreeRungs` (pure),
  `TestSortRungsByLadder` (pure), `TestGroupSegsByRendition_...`
  (pure), `TestPickRenditionURIs` / `TestRungPlaylistFilename` /
  `TestChannelDirAndRedisKeys` (pure),
  `TestSyncLocalCacheDir_SharedChannelRoot` (pure, `t.TempDir()`),
  `TestGenerateMasterPlaylist_ThreeRungs` (Redis end-to-end),
  `TestGenerateMasterPlaylist_ColdStartEmptyMetadata` (Redis),
  `TestActiveRungMeta_FallsBackToActiveLadder` (Redis).
- `internal/api/handler_test.go` — `TestRungPlaylistGuard_...` (pure,
  M1.5 shape: `playlist_<rend>` URL): unknown-rendition reject, valid
  ladder acceptance, dotdot, slash; `TestSegmentGuard_...` (pure):
  non-`.ts` reject, valid, dotdot/slash; Gin's smoke test.

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
- **Concurrency caveat (NOT YET SAFE):** `Planner` carries shared mutable state across calls (`globalRunningTime`, `totalSegmentsAdded`, `processDate`, `horizon`, `channelID`) without a mutex. Setting `PLANNER_WORKERS > 1` will race two concurrent `ProcessEPGToTimeline` calls on the same `*Planner` instance and produce corrupt timelines. Keep `PLANNER_WORKERS=1` (the default is 4 — see `internal/planner/planner.go` NOTE block; the parallel loop is wired but the underlying state isn't safe until per-call state is moved onto `SegmentProcessor`).
- **Retry/backoff:** `ProcessEPGToTimelineWithRetry` wraps the raw EPG walk; schedule is `base * 2^(i-1)` capped at `PREPARE_RETRY_MAX_MS`, attempts = `MAX_PREPARE_RETRIES`. On final failure the channel's `planner:failures:{id}` counter is incremented via `HandlePrepareResultForChannel`.
- **Breaker:** once the counter ≥ `failureInactivateThreshold` (5) the channel is set `is_active=false`. A successful prepare (or a "date already prepared" short-circuit) resets the counter to 0. The breaker key `planner:failures:{id}` can also be reset manually via `POST /api/channels/:id/start` (best-effort on Redis errors — DB flip is the durable part).
- **Metrics:** each tick logs `[METRIC] channel={id} name=... zset_size={n} cursor={ts}` per active channel (via `TimelineSize` + `LastProcessedCursor`); greppable for Loki/etc.
