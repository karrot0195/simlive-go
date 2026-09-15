# Engine — Sliding HLS + PTS Rewrite (Feature Detail)

> Feature: `engine-sliding-hls-pts-rewrite`
> Source: `docs/specification.md` §5 (Module 2: Engine), `.github/instructions/simlive-summary.instructions.md`, `internal/engine/engine.go`, `internal/engine/scte35.go`, `internal/engine/rendition.go`, `docs/memories.md`.

## Overview
The engine (`StartSimlive`) runs an infinite 1-second tick loop managing playout state. It reads mapped segment timelines from Redis (`simlive:timeline:{ch}:{rend}`), downloads `.ts` files to a bounded sliding-window directory (`{TMP_DIR}/{ch}/`), rewrites PTS/DTS (`rewritePtsOffset`), and writes dynamic HLS playlists (master `playlist.m3u8` + 3 per-rung `playlist_<rend>.m3u8`).

## Pipeline steps
1. **Fetch timeline** (`fetchTimeline`) — reads per-rung ZSET from Redis; falls back to flat `simlive:timeline:{ch}` on legacy.
2. **Sort + filter** — sort by `start_time`; filter to active window (`[-300s, +120s]`).
3. **Download + prepare** (`syncAndPreparePerRendition`) — `rclone copyto` each `.ts` to `{TMP_DIR}/{ch}/seg_N.ts`; write `.pts` marker.
4. **PTS/DTS rewrite** (`rewritePtsOffset`) — `target = seg.PtsOffset → fallback MediaStart → fallback PtsStart`; `offset = target - firstDTS`; applies `ffmpeg -c copy -copyts -output_ts_offset`.
5. **Generate playlists** — master (`buildMasterPlaylist`) with 3 `STREAM-INF` lines; per-rung media (`generateHLSPlaylist`) advertising only `candidateSegs` (playhead window, not full on-disk superset); `lookBehindSegment` provides 1-seg context for edge detection.
6. **SCTE-35 emission** (`buildMediaPlaylistBody`) — `#EXT-X-DATERANGE` (CUE-OUT before first ad, CUE-IN before first post-ad VOD) + per-segment `#EXT-X-SCTE35` (`CUE=`, `TYPE=`, `ID=`) for each `SpliceInsert` stamped by planner.
7. **GC / cleanup** (`syncLocalCacheDir`) — evicts `.ts`/`.pts` outside bounded window (`keepBehind` + window + ahead); preserves master + 3 media playlists.

## Key invariants
- Advertised window (`candidateSegs`) is always a subset of on-disk superset (`downloadSegs`) — prevents 404 on first advertised segment.
- Shared segment cache (`{TMP_DIR}/{ch}/seg_N.ts`) — all 3 rungs reference same files; M2 will split to per-rung subdirs.
- `PtsOffset` stays local (`MediaStart − timelineBase`) — avoids 2^33/90000 epoch wrap.
- `lookBehindSegment` (1 seg before window anchor) feeds `buildMediaPlaylistBody` edge detection without being advertised.

## Related docs
- `docs/specification.md` §5 Module 2
- `docs/300826_0620-multi-rendition-abr-ladder.md`
- `docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md`
- `docs/memories.md` (engine fix: advertise only playhead window; stop/delete cleanup; logging module)
