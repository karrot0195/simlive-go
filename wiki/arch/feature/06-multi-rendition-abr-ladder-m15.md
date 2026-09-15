# Multi-Rendition ABR Ladder — M1.5 (Feature Detail)

> Feature: `multi-rendition-abr-ladder-m15`
> Source: `docs/300826_0620-multi-rendition-abr-ladder.md`, `.github/instructions/simlive-summary.instructions.md`, `internal/models/models.go`, `internal/planner/planner.go`, `internal/engine/engine.go`, `docs/memories.md`.

## Overview
M1.5 (shipped) = full master-playlist shape + 3 advertised rungs + shared segment cache + `playlist_<rend>.m3u8` API shape. M2 (true per-rung separate segments) deferred. The engine downloads ONE set of `seg_N.ts` files (from active rung's VOD dir) and serves them as 3 rung playlists + 1 master.

## Key differences (M1 → M1.5)
| M1 (original) | M1.5 (shipped) |
|---|---|
| `{ch}/{rend}/seg_N.ts` per rung | `{ch}/seg_N.ts` shared |
| `{ch}/{rend}/playlist.m3u8` | `{ch}/playlist_<rend>.m3u8` |
| Master: 1 `STREAM-INF` | Master: 3 `STREAM-INF` |
| `simlive:renditions:{ch}` = 1 entry | `simlive:renditions:{ch}` = 3 entries |
| API: `/{ch}/{rend}/playlist.m3u8` | API: `/{ch}/playlist_:rendition` |

## Data model
- `SegmentItem.Rendition string` (`json:"rendition,omitempty"`) — empty = legacy = engine default `360p`.
- `simlive:renditions:{ch}` — JSON array of `RenditionMeta` (rendition, vod_dir, ads_dir, width, height, bandwidth) for all 3 rungs; 24h TTL.
- `SegmentItem.Renditions` — storage-tier dirs (`["L_0","L_1","L_2"]` VOD / `["L2","L1","L0"]` ads) in ladder order; rung selection = index → `RENDITIONMAP[i]` = `{360p,720p,1080p}`.

## Planner (`resolveRenditionsFor`)
- `ladderCache map[string]map[string]renditionPair` — keyed `streamID → tierLabel → pair`. `rendCache` (legacy single-pair) coexists; do not collapse.
- `activeRenditions()` — reads `SIMLIVE_RENDITIONS` (comma list). Unset → single rung = `SIMLIVE_QUALITY` tier label, fallback `360p`. Multi-rung values (`"360p,720p"`) → WARN once, use first entry.
- `probeBitrate(remotePath)` — primary: ffprobe `format=bit_rate`; fallback: `clamp(w*h*0.08, 100_000, 20_000_000)`.

## Engine (`channelState`)
- Keyed `channelID` only (shared cache = single state per channel; M1 `{ch}:{rend}` keying reverted).
- `activeRungMeta(ctx, chID)` — planner metadata (`simlive:renditions:{ch}`) → hard-coded `ActiveLadder` fallback (360p/720p/1080p) when Redis key absent (cold start before planner runs).
- `groupSegsByRendition` — rescues empty `Rendition` to `DefaultRendition = "360p"`; actual download/GC/playlist emission is single pass against channel root.

## API routes
- `GET /live/{ch}/playlist.m3u8` → master (`simlive:hls:{ch}`)
- `GET /live/{ch}/playlist_:rendition` → rung media (`simlive:hls:{ch}:rendition:{rend}`)
- `GET /live/{ch}/:segment` → shared `.ts`

## Related docs
- `docs/300826_0620-multi-rendition-abr-ladder.md`
- `.github/instructions/simlive-summary.instructions.md`
- `docs/memories.md` (M1.5 reconciliation; M2 deferred)
