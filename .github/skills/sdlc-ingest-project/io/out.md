---
project_name: simlive-go
language: Go
domain: simulated-live HLS playout (24/7 linear streaming)
modules:
  - planner (PrepareSimlive — EPG → Redis timeline)
  - engine (StartSimlive — sliding HLS playlist + PTS/DTS rewrite)
  - api (REST channel management + PARTNER client)
  - models (Postgres + Redis structs, SegmentItem, SpliceInsert)
  - storage (rclone wrapper)
  - config (env vars + config loading)
support:
  - Planner: per-channel EPG schedule fetch (today + tomorrow), stream ID mapping, segment timeline (per-rung ZSET `simlive:timeline:{ch}:{rend}`), ad insertion (`insertAdsIfNeeded`), quality tier resolution (`SIMLIVE_QUALITY`), legacy-timeline flush (`HasLegacyTimeline` / `DeleteTimeline`), warm-pickup cron loop (`pollInterval` 3s, `PLANNER_INTERVAL` 5m).
  - Engine: 1-second tick loop, bounded on-disk window (`keepBehind` + window + ahead), per-rung media playlists (`playlist_<rend>.m3u8`), master playlist (`playlist.m3u8` with 3 `STREAM-INF` lines), shared segment cache (`{TMP_DIR}/{ch}/seg_N.ts`), PTS/DTS rewrite (`rewritePtsOffset`), SCTE-35 `#EXT-X-DATERANGE` (CUE-OUT / CUE-IN) + per-segment `#EXT-X-SCTE35`, `lookBehindSegment` for edge detection.
  - API: `POST/GET/PUT/PATCH/DELETE /api/channels`, `POST /start`, `POST /stop`, `GET /live/{ch}/playlist.m3u8`, `GET /live/{ch}/playlist_:rendition`, `GET /live/{ch}/:segment`, Redis purge (`purgeChannelRedisData`), breaker counter (`planner:failures:{ch}`).
  - Storage: `rclone cat` (manifest parsing), `rclone copyto` (segment download), `rclone ls` (directory listing).
  - Quality tiers: 360p (`L_0` VOD / `L2` ads), 720p (`L_1` / `L1`), 1080p (`L_2` / `L0`).
  - Multi-rendition (M1.5): shared segment cache + 3 advertised rungs + `simlive:renditions:{ch}` metadata + `playlist_<rend>.m3u8` API shape. M2 (true per-rung separate segments) deferred.
feature:
  title: multi-feature-ingest (all 6 features detached)
  description: >
    Full project scan detaches 6 features: planner-prepare-mapping-data, engine-sliding-hls-pts-rewrite, api-channel-crud-hls-routes, storage-rclone-manifest-download, scte35-cue-markers-daterange, multi-rendition-abr-ladder-m15. Each has its own doc under wiki/arch/feature/.
  features:
    - planner-prepare-mapping-data (wiki/arch/feature/01-planner-prepare-mapping-data.md)
    - engine-sliding-hls-pts-rewrite (wiki/arch/feature/02-engine-sliding-hls-pts-rewrite.md)
    - api-channel-crud-hls-routes (wiki/arch/feature/03-api-channel-crud-hls-routes.md)
    - storage-rclone-manifest-download (wiki/arch/feature/04-storage-rclone-manifest-download.md)
    - scte35-cue-markers-daterange (wiki/arch/feature/05-scte35-cue-markers-daterange.md)
    - multi-rendition-abr-ladder-m15 (wiki/arch/feature/06-multi-rendition-abr-ladder-m15.md)
  feature_doc_dir: wiki/arch/feature/
  summary_doc_path: wiki/arch/01-summary.md
generated_at: "2026-09-15T00:00:00Z"
source_refs:
  - docs/specification.md
  - .github/instructions/simlive-summary.instructions.md
  - docs/memories.md
  - docs/structure.md
  - docs/300826_0620-multi-rendition-abr-ladder.md
  - docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
  - internal/planner/planner.go
  - internal/planner/segment_processor.go
  - internal/engine/engine.go
  - internal/models/models.go
  - cmd/planner/main.go
  - cmd/api/main.go
standard_format_version: "1.0"
