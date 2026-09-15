# Storage — Rclone Manifest + Download (Feature Detail)

> Feature: `storage-rclone-manifest-download`
> Source: `docs/specification.md` §4 (Media Storage Access via RClone), `.github/instructions/simlive-summary.instructions.md`, `internal/storage/rclone.go`, `docs/memories.md`.

## Overview
Storage access is mediated by `rclone` (not direct S3/HTTP). The planner uses `rclone cat` to parse HLS manifests; the engine uses `rclone copyto` to download `.ts` segments. The `rclone` binary and config (`rclone.conf`) are bundled in the repo.

## Operations
- `rclone cat espx-video-lib:espx-video-lib/{streamID}/L_0/vod.m3u8` — read master/variant manifest (planner).
- `rclone cat .../ads-v3/{adsRend}/vod.m3u8` — read ads manifest (planner).
- `rclone copyto espx-video-lib:.../segment_000.ts {TMP_DIR}/{ch}/seg_N.ts` — download segment (engine).
- `rclone ls` — directory listing (defensive checks).

## Key points
- VOD dir (`L_0`/`L_1`/`L_2`) and ads dir (`L2`/`L1`/`L0`) resolved per `SIMLIVE_QUALITY` tier so VOD and ads always match resolution.
- `SegmentItem.Renditions` holds storage-tier dirs (`["L_0","L_1","L_2"]` VOD / `["L2","L1","L0"]` ads) in ladder order — NOT quality labels. Rung selection maps index → `RENDITIONMAP[i]` = `{360p,720p,1080p}`.
- `groupSegsByRendition` (commented out) — raw tier labels cannot be used as rung keys; index-based mapping is correct.

## Related docs
- `docs/specification.md` §4
- `.github/instructions/simlive-summary.instructions.md`
- `docs/300826_0620-multi-rendition-abr-ladder.md`
