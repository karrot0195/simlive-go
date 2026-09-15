# Planner — Prepare Mapping Data (Feature Detail)

> Feature: `planner-prepare-mapping-data`
> Source: `docs/specification.md` §5 (Module 1: Planner), `.github/instructions/simlive-summary.instructions.md`, `internal/planner/planner.go`, `internal/planner/segment_processor.go`, `docs/300826_0620-multi-rendition-abr-ladder.md`.

## Overview
The planner's `prepareDate(ch, date)` pipeline is the entry point for mapping EPG schedule items to `.ts` segments and persisting them in Redis. It runs inside a 24/7 cron loop (`PLANNER_INTERVAL`, default 5m) with a warm-pickup mechanism (`pollInterval` 3s) that only runs a full tick when the active-channel set changed or the periodic interval elapsed.

## Pipeline steps

1. **Query active channels** (`playout_channels` DB; `is_active = true`).
2. **Compute today + tomorrow** in the channel's `content_timezone` (`utils.GetLocFor`).
3. **Short-circuit check** (`HasDateInTimeline`) — `ZCOUNT simlive:timeline:{ch}:{rend} [dayStart, dayEnd] > 0` across all active rungs. If non-empty, skip `ProcessEPGToTimeline`.
4. **Process EPG** (`ProcessEPGToTimeline`) — for each schedule item:
   - Read `programme.short_url` (Stream ID).
   - Resolve VOD + ads rendition pair per active rung (`resolveRenditionsFor(streamID, tier)` + `resolveAdsRenditionForTier(tier)`).
   - Execute `rclone cat` to read the rung's variant `m3u8` contents.
   - Compute timeline timestamps (`start_time`, `pts_start`, `pts_end`, `media_start`, `media_end`, `pts_offset`).
   - Stamp ad-break metadata (`IsAd`, `AdBreakID`, `AdBreakStart`, `AdBreakDuration`) via `insertAdsIfNeeded`.
5. **Write per-rung ZSET** (`simlive:timeline:{ch}:{rend}`) — score = `MediaStart` (seconds), member = `SegmentItem` JSON (incl. `pts_offset`, `rendition` omitempty, 4 ad-break fields omitempty).
6. **Batch insert** — Redis pipeline batching every 50 items.
7. **Write rendition metadata** (`simlive:renditions:{ch}`) — JSON array of `RenditionMeta` (rendition, vod_dir, ads_dir, width, height, bandwidth) for all 3 rungs; 24h TTL.
8. **Legacy-timeline flush** (`HasLegacyTimeline` / `DeleteTimeline`) — first tick after M1 upgrade checks first ZSET member for `rendition`; if absent, `DEL` all rung keys so members are repopulated with the new tag.
9. **Per-tick bookkeeping** — `ProcessWindow` bumps `planner:cursor:{ch}` to `nowUnix + LOOKAHEAD_SECONDS`; `CleanupOldSegments` trims each rung's ZSET with `ZREMRANGEBYSCORE` at `nowUnix - RETENTION_SECONDS`.

## Key data structures
- `SegmentItem` (`internal/models/models.go`) — `sequence`, `storage_path`, `duration`, `start_time`, `pts_start`, `pts_end`, `media_start`, `media_end`, `pts_offset`, `rendition` (omitempty), `is_ad`, `ad_break_id`, `ad_break_start`, `ad_break_duration` (all omitempty).
- `RenditionMeta` (`internal/planner/planner.go`) — `Rendition`, `VodDir`, `AdsDir`, `Width`, `Height`, `Bandwidth`.
- `SegmentProcessor` (`internal/planner/segment_processor.go`) — `currentRendition` set from `activeRenditions()[0]`; calls `resolveRenditionsFor(streamID, currentRendition)`.

## Key functions
- `prepareDate(ch, date)` — entry; calls `HasDateInTimeline` then `ProcessEPGToTimeline`.
- `ProcessEPGToTimeline(ch, date)` — walks EPG items, builds `SegmentProcessor`, writes ZSET + metadata.
- `resolveRenditionsFor(streamID, tier)` — returns `ladderCache[streamID][tier]`, probing + populating on miss.
- `resolveAdsRenditionForTier(tier)` — ads dir per tier (`L2`/`L1`/`L0`).
- `insertAdsIfNeeded(...)` — stamps 4 splice events (`0x30` pre-roll 8s/4s, `0x34` slice-point, `0x35` cue-in) on correct segments.
- `writeRenditionMetadata(ctx, chID)` — single `SET simlive:renditions:{ch}` (no pipeline).
- `HasLegacyTimeline(ch)` / `DeleteTimeline(ch)` — legacy flush.

## Invariants preserved
- Per-rung VOD↔ads resolution match (same tier per rung).
- Shared local `PtsOffset` timeline (`MediaStart − timelineBase`) — avoids 2^33/90000 ≈ 95443.7s epoch PTS wrap.
- Bounded on-disk window per rung (`keepBehind` + window + ahead).
- `SIMLIVE_QUALITY` tier mapping: 360p (`L_0`/`L2`), 720p (`L_1`/`L1`), 1080p (`L_2`/`L0`).

## Related docs
- `docs/specification.md` §5 (Module 1: Planner)
- `.github/instructions/simlive-summary.instructions.md` (architecture snapshot)
- `docs/300826_0620-multi-rendition-abr-ladder.md` (M1.5 ABR ladder plan)
- `docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md` (SCTE-35 splice-insert plan)
- `docs/memories.md` (session log — M1.5 reconciliation, SCTE-35 cue markers, engine fix)
