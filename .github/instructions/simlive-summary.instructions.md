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
- **API** → serves the playlist/segments at `http://localhost:8080/live/{channelID}/` and the playout-channel CRUD + start/stop surface at `http://localhost:8080/api/channels[/...]` (handlers in `internal/api/handler.go`). Channel `status` lifecycle: **PREPARE** (register / start) → **LIVE** (planner is the SINGLE writer, once timeline data is in Redis) → **INACTIVE** (stop / soft delete). The planner reads the same `is_active` / `status` columns, so a manual `start` / `stop` flip takes effect on the next planner tick (default 5m, `PLANNER_INTERVAL`). The engine's reconciler starts the playout loop on `is_active=true AND status != 'INACTIVE'` (PREPARE or LIVE) — it does NOT wait for LIVE. The loop idles until the planner writes timeline data. `start` also deletes the breaker counter key `planner:failures:{id}` to reset the per-channel failure breaker. `stop` and `delete` (soft) call `purgeChannelRedisData(id)` which DELs every per-channel key in Redis (HLS master + per-rung media, planner timeline, planner renditions metadata, planner cursor) — the breaker counter is intentionally preserved across the purge.

## Architecture snapshot (keep in sync)
- **Planner** → per-rung Redis ZSETs `simlive:timeline:{channelID}:{rend}`
  (one per active rung: 360p / 720p / 1080p); score = `MediaStart`
  (seconds), member = `SegmentItem` JSON incl. local `pts_offset`, `rendition`
  (omitempty), and the four ad-break fields `is_ad / ad_break_id /
  ad_break_start / ad_break_duration` (all omitempty so legacy VOD members
  round-trip unchanged). 24/7 cron walks **today + tomorrow** in each
  channel's `content_timezone` per tick (`PLANNER_INTERVAL`, default 5m);
  per-date `horizon` (EndOfDay of the parsed date) lets a single
  `ProcessEPGToTimeline` call walk past midnight. **M2: `ProcessEPGToTimeline`
  walks EVERY active rung** (from `activeRenditions()`), each into its own
  per-rung ZSET with its own `SegmentProcessor` (per-rung sequence counter,
  per-tier ads rendition, per-rung timeline key). All rungs share the same
  local `PtsOffset` timeline (MediaStart) so players can switch rungs at
  segment boundaries. `HasDateInTimeline` (ZCOUNT `[dayStart, dayEnd] > 0`
  across all rungs) short-circuits re-prepare of a date that already has
  data. A successful prepare (or that short-circuit) also flips the
  channel's `status` to `'LIVE'` via `markChannelLive` /
  `MarkChannelLiveForChannel` — the planner is the SINGLE writer of 'LIVE',
  and the engine's reconciler only streams `is_active AND status='LIVE'`
  channels. **M1 upgrade guard:** `HasLegacyTimeline` checks the first ZSET
  member of each rung for `rendition`; if absent, `DeleteTimeline` flushes
  all rung keys so members are repopulated with the new tag.
- **Planner → `simlive:renditions:{channelID}`** (M1.5): JSON array of
  `RenditionMeta` (rendition, vod_dir, ads_dir, width, height, bandwidth)
  for **all 3 rungs** (360p / 720p / 1080p). Written by `writeRenditionMetadata`
  after each pipeline flush. Consumed by the engine to build the master
  playlist without runtime probing.
- **Engine** → per-rung window `[now−300s, now+120s]` on each rung's ZSET;
  ffmpeg `-c copy -copyts -output_ts_offset` rewrite; `.pts` marker =
  prepared. **M2: per-rung segment cache** -- each rung downloads its OWN
  `seg_N.ts` files into its own subdir `{TMP_DIR}/{ch}/{rend}/` (from that
  rung's VOD dir via each member's `StoragePath`). The engine writes:
    - 1 master `playlist.m3u8` (3 `#EXT-X-STREAM-INF` lines +
      `#EXT-X-INDEPENDENT-SEGMENTS`) → Redis `simlive:hls:{ch}`
    - 1 per-rung media playlist `{ch}/{rend}/playlist.m3u8` (references
      that rung's OWN `seg_N.ts` files) → Redis
      `simlive:hls:{ch}:rendition:{rend}`. The `groupSegsByRendition`
  helper rescues empty `Rendition` to `DefaultRendition = "360p"`. The
  per-channel ladder is resolved via `activeRungMeta` (planner metadata
  → hard-coded `ActiveLadder` fallback) so the master always lists 3
  rungs even before the planner has run.
  - **SCTE-35 cue tags:** per-rung media playlists also emit
    `#EXT-X-DATERANGE` lines on VOD<->ad transitions (CUE-OUT before the
    first ad EXTINF; CUE-IN before the first VOD EXTINF after the break),
    AND per-segment `#EXT-X-SCTE35` lines for each `SpliceInsert` the
    planner stamped (BEFORE the segment's `#EXTINF`, AFTER any
    DATERANGE). The DATERANGE lines are the edge-detection summary
    tags; the per-segment inserts carry the 4-event ad-break contract
    (8s pre-roll, 4s pre-roll, slice-point, cue-in). Tag format
    mirrors `tmp/SCTE35Utils.ts` (DATERANGE) and
    `tmp/PrepareSimulatedLive.ts markSpliceTargets` (per-segment
    inserts). DURATION is unquoted and trailing (threefive TagParser
    gotcha) and capped at 3 decimal places (matches `#EXTINF:%.3f`;
    FP-noise tails like `20.019999999999996` are rounded to `20.02`).
    The base64 `SpliceInfoSection` is built in Go with the
    `Comcast/scte35-go` library (`internal/engine/scte35.go`:
    `buildSpliceInsert`, `buildCueOutLine`, `buildCueInLine`,
    `buildSpliceInsertLine`, `buildMediaPlaylistBody`,
    `adBreakEventID`, `formatBreakStartDate`, `formatBreakDuration`).
    Per-segment tag wire format:
    `#EXT-X-SCTE35:CUE="<base64>",TYPE=<segType>,ID="<adbreak-id>"`
    where `<segType>` is the decimal segmentation_type_id (0x30 pre-roll,
    0x34 slice-point, 0x35 cue-in). Plans:
    [300826_0616-implement-scte35-cue-markers.md](../docs/300826_0616-implement-scte35-cue-markers.md)
    (DATERANGE summary tags) and
    [030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md](../docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md)
    (per-segment pre-roll + slice-point + cue-in).
- **Invariants:** VOD↔ads must resolve to the SAME resolution per rung;
  `PtsOffset` stays local (`MediaStart − timelineBase`) to avoid the
  2^33/90000 ≈ 95443.7s epoch PTS wrap.

## Quality tiers (`SIMLIVE_QUALITY`)
| Tier | VOD dir | ADS dir |
|------|---------|---------|
| 360p | `L_0` | `L2` |
| 720p | `L_1` | `L1` |
| 1080p | `L_2` | `L0` |

## Key env vars
`SIMLIVE_QUALITY` (auto default), `SIMLIVE_RENDITIONS` (M2: comma list of
active rungs; unset → full ladder `360p,720p,1080p`; all listed rungs are
walked by the planner and served by the engine),
`SIMLIVE_VOD_RENDITION` / `SIMLIVE_ADS_RENDITION` (hard overrides),
`REDIS_ADDR` (6371), `TMP_DIR` (./outputs), `MAX_WORKERS` (50),
`ENABLE_CLEAN_UP` (false), `SIMLIVE_KEEP_BEHIND` (5),
`SIMLIVE_DOWNLOAD_AHEAD` (5). Full table in `docs/specification.md` §8.

## Operational Details
- **Bounded on-disk window:** the engine keeps at most
  `SIMLIVE_KEEP_BEHIND + 5 + SIMLIVE_DOWNLOAD_AHEAD` `.ts` files (and the same
  number of `.pts` markers) **per rung** on disk (default 15 × 3 rungs = 45
  total). This bounded storage ensures low disk usage and fast startup
  cleanup. The Redis timeline window (`[-300s, +120s]`) is still queried for
  awareness, but only the active window is downloaded/kept; everything else
  is evicted each tick.
- **Warm start by default:** the engine's `PrepareEnv` preserves
  `{TMP_DIR}/{ch}/` across restarts so a restarted engine can resume
  immediately on whatever the previous run had downloaded. Set
  `ENABLE_CLEAN_UP=true` for a cold start (`os.RemoveAll(tmpDir)` on
  startup). The planner's `PrepareEnv` similarly defaults to a scoped
  wipe of just `simlive:timeline:*` and only falls back to a full
  `FlushAll + os.RemoveAll(tmpDir)` when `ENABLE_CLEAN_UP=true`. This
  lets a fleet restart (or a binary deploy) keep data flowing without
  re-downloading every segment from rclone.
- **Planner dedup:** `prepareDate(ch, date)` calls `HasDateInTimeline` first;
  the cron only re-walks EPG when `ZCOUNT simlive:timeline:{ch} [start,end]`
  is zero. Cold start fills today + tomorrow (~1–2 min); subsequent ticks
  skip both dates. A successful prepare or the "already prepared"
  short-circuit also flips the channel to `status='LIVE'`.

## Tests
- `internal/planner/horizon_rollover_test.go` — `TestIsEndOfTimeline_...` (pure
  Go) + `TestHasDateInTimeline_...` (skipped when no Redis at `REDIS_ADDR`).
- `internal/planner/retry_breaker_test.go` — `TestFailureCounter_SuccessResetsZero`
  (Redis), `TestRetryBackoffDelay_Schedule` (pure), `TestMarkChannelInactive_RespectsDB`
  (pure, nil-db no-op), `TestMarkChannelLive_RespectsDB` (pure, nil-db no-op).
- `internal/planner/rendition_test.go` — `TestActiveRenditions_...` (pure:
  default full ladder, comma-list parse, all-listed-rungs),
  `TestProbeBitrateFallback_Formula` (pure), `TestParseBitrateStdout_...`
  (pure), `TestWriteRenditionMetadata_SerializesJSON` (Redis),
  `TestHasLegacyTimeline_ReportsAndSkips` (Redis).
- `internal/models/models_test.go` — `TestSegmentItem_Rendition_...` (pure):
  omitempty, roundtrip, legacy-unmarshal rescue, multi-rung isolation.
  `TestSegmentItem_SliceInserts_...` (pure): omitempty for the per-segment
  splice-inserts list, roundtrip (all 7 fields on every entry preserved),
  legacy-rescue (pre-feature JSON decodes to `nil` slice).
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
  `DURATION=20.02` / `DURATION=0.3`), and `TestBuildSpliceInsertLine_...`
  (pure: 4 tests for the per-segment `#EXT-X-SCTE35` line emitter --
  format check that asserts the right `CUE=` / `TYPE=` / `ID=`
  attributes and round-trips the base64, OON=false for cue-in,
  NaN-duration degrades gracefully, ID fallback to
  `adbreak-<int64(SpliceTimeSeconds)>` when the segment carries no
  AdBreakID).
- `internal/engine/cue_tag_test.go` — `TestBuildMediaPlaylistBody_...`
  (pure): no-ads shape, CUE-OUT before first ad EXTINF, CUE-IN between
  last ad and first post-ad VOD, cold-start with ad as first segment,
  multiple breaks, empty input error, trailing-unquoted-attr gotcha,
  SegmentItem roundtrip, per-segment splice-insert emission (2
  pre-rolls on their VOD positions, slice-point before first ad
  EXTINF, cue-in on the last ad, no `#EXT-X-SCTE35` for segments
  with empty `SliceInserts`, all 4 events in chronological order).
- `internal/engine/master_playlist_test.go` — `TestBuildMasterPlaylist_...`
  + `TestBuildMasterPlaylist_ThreeRungs` (pure),
  `TestActiveLadder_HasThreeRungs` (pure),
  `TestSortRungsByLadder` (pure), `TestGroupSegsByRendition_...`
  (pure), `TestPickRenditionURIs` / `TestRungPlaylistFilename` /
  `TestChannelDirAndRedisKeys` (pure, M2: `{rend}/playlist.m3u8` URIs +
  `rungDir`),
  `TestSyncLocalCacheDir_PerRungDir` (pure, `t.TempDir()`),
  `TestGenerateMasterPlaylist_ThreeRungs` (Redis end-to-end),
  `TestGenerateMasterPlaylist_ColdStartEmptyMetadata` (Redis),
  `TestActiveRungMeta_FallsBackToActiveLadder` (Redis).
- `internal/api/handler_test.go` — `TestRungPlaylistGuard_...` (pure,
  M2 shape: `/{rend}/playlist.m3u8` URL): unknown-rendition reject, valid
  ladder acceptance, dotdot, slash; `TestSegmentGuard_...` (pure, M2
  3-param `/{rend}/{segment}`): non-`.ts` reject, valid, dotdot/slash;
  Gin's smoke test.

## Post-task rule
After finishing any code change, update this summary and
`docs/specification.md` for everything the change affects (models, env vars,
constants, architecture, quality tiers) — and nothing else.

## Additional env vars
- `PLANNER_INTERVAL` (default 5m), `PLANNER_WORKERS` (default **4**, floored at 1) — cron loop + per-tick channel concurrency.
- `RETENTION_SECONDS` (7200), `LOOKAHEAD_SECONDS` (7200) — 24/7 continuous cursor + cleanup.
- `MAX_PREPARE_RETRIES` (3), `PREPARE_RETRY_BASE_MS` (500), `PREPARE_RETRY_MAX_MS` (8000) — bounded exponential backoff for `ProcessEPGToTimeline`.
- Cursor: `planner:cursor:{channelID}`; cleanup: `ZREMRANGEBYSCORE` with `RETENTION_SECONDS`.
- Per-channel consecutive-failure counter: `planner:failures:{channelID}` (TTL 24h). After 5 consecutive `(channel, date)` failures the channel is flipped to `is_active=false` by the cron loop so a broken upstream stops burning CPU/EPG bandwidth.

## Resilience & observability (planner)
- **Worker pool:** active channels are processed in parallel by `PLANNER_WORKERS` goroutines (semaphore + WaitGroup) — bounded so a 50-channel fleet doesn't open 50 simultaneous EPG fetches. Default is **4**. Different channels run concurrently because the per-call walk state lives on `SegmentProcessor`, not the shared `Planner` (see the NOTE on the Planner struct in `internal/planner/planner.go`). Values < 1 are floored to 1.
- **Concurrency contract:** `Planner` is safe to call concurrently for DIFFERENT channels: per-call state (`runningTime`, `totalSegmentsAdded`, `channelID`, `horizon`) is owned by the `SegmentProcessor` created per call; the shared `Planner` holds only connection handles + the `rendMu`-guarded rendition caches. The per-channel in-flight guard in `cmd/planner runPlannerTick` still prevents overlapping calls for the SAME channel. **History:** the planner used to hold this per-call state on the shared `Planner` with no lock; with 2 active channels the two parallel `ProcessEPGToTimeline` calls would clobber each other's state mid-walk and produce a corrupt timeline (one channel's segments landing in the other's ZSET, or the EPG walk stopping early). Symptom: 2 active channels → only 1 had data. A stopgap `epgMu` mutex + clamping `PLANNER_WORKERS` to 1 fixed the race but serialized whole cold-start walks (~30s–2min per date, rclone had no timeouts), so channel #2 had no data for the entire window — that stopgap was removed in favour of the per-call-state refactor. `rclone cat`/`copyto` now run under hard timeouts (30s/60s) so a hung remote cannot wedge a worker.
- **Retry/backoff:** `ProcessEPGToTimelineWithRetry` wraps the raw EPG walk; schedule is `base * 2^(i-1)` capped at `PREPARE_RETRY_MAX_MS`, attempts = `MAX_PREPARE_RETRIES`. On final failure the channel's `planner:failures:{id}` counter is incremented via `HandlePrepareResultForChannel`.
- **Breaker:** once the counter ≥ `failureInactivateThreshold` (5) the channel is set `is_active=false`. A successful prepare (or a "date already prepared" short-circuit) resets the counter to 0 AND flips `status` to `'LIVE'` (`markChannelLive` — the planner is the single writer of LIVE). The breaker key `planner:failures:{id}` can also be reset manually via `POST /api/channels/:id/start` (best-effort on Redis errors — DB flip is the durable part).
- **Metrics:** each tick logs `[METRIC] channel={id} name=... zset_size={n} cursor={ts}` per active channel (via `TimelineSize` + `LastProcessedCursor`); greppable for Loki/etc.
