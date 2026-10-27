# SimLive-Go — Architecture Snapshot

> Concise architecture snapshot. Always in sync with code. Read-only for skills.

## Project Identity
- **Name**: simlive-go
- **Language**: Go 1.26.6 (module `simlive`)
- **Domain**: Simulated-live (24/7 linear) HLS playout system
- **Entry points**: `cmd/api/main.go`, `cmd/engines/main.go`, `cmd/planner/main.go`

## Modules
| Module | Path | Role |
|---|---|---|
| Planner | `internal/planner/` | EPG → Redis timeline mapping (`prepareDate`, `ProcessEPGToTimeline`, `SegmentProcessor`) |
| Engine | `internal/engine/` | Sliding HLS playlist + PTS rewrite (`StartSimlive`, tick loop, bounded window) |
| API | `internal/api/` | REST channel CRUD + HLS routes + PARTNER client |
| Models | `internal/models/` | Postgres + Redis structs (`SegmentItem`, `RenditionMeta`, `SpliceInsert`) |
| Storage | `internal/storage/` | rclone wrapper (cat / copyto / ls) |
| Config | `internal/configs/` | Env vars + config loading |

## Quality Tiers (SIMLIVE_QUALITY)
| Tier | VOD dir | Ads dir |
|---|---|---|
| 360p | `L_0` | `L2` |
| 720p | `L_1` | `L1` |
| 1080p | `L_2` | `L0` |

## Redis ZSETs (per-rung)
| Key | Purpose |
|---|---|
| `simlive:timeline:{ch}:{rend}` | Segment timeline (score=MediaStart, member=SegmentItem JSON) |
| `simlive:renditions:{ch}` | Rendition metadata (3 rungs, 24h TTL) |
| `simlive:hls:{ch}` | Master playlist |
| `simlive:hls:{ch}:rendition:{rend}` | Per-rung media playlist |
| `planner:cursor:{ch}` | Cursor (nowUnix + LOOKAHEAD_SECONDS) |
| `planner:failures:{ch}` | Breaker counter |
| `planner:cursor:{ch}` | Cursor |

## Master Playlist (M1.5)
- 3 `#EXT-X-STREAM-INF` lines (360p, 720p, 1080p)
- `#EXT-X-INDEPENDENT-SEGMENTS` on each rung
- Shared segment cache: `{TMP_DIR}/{ch}/seg_N.ts`

## Bounded Window
- `SIMLIVE_KEEP_BEHIND` (default 5) + 5 + `SIMLIVE_DOWNLOAD_AHEAD` (default 5) = 15 `.ts` files per rung
- Redis timeline window: `[-300s, +120s]`

## Warm Start
- `PrepareEnv` preserves `{TMP_DIR}/{ch}/` across restarts
- `ENABLE_CLEAN_UP=true` triggers cold start (`os.RemoveAll(tmpDir)`)

## SCTE-35 Emission
- Per-rung media playlists: `#EXT-X-DATERANGE` (CUE-OUT / CUE-IN)
- Per-segment: `#EXT-X-SCTE35` (`CUE="<base64>",TYPE=<segType>,ID="<adbreak-id>"`)
- `TYPE` = decimal `segmentation_type_id` (0x30 pre-roll, 0x34 slice-point, 0x35 cue-in)
- `DURATION` unquoted, trailing, capped at 3 decimal places

## Breaker Counter
- `planner:failures:{ch}` — incremented on start failure, reset on `POST /api/channels/:id/start`
- Preserved on `POST /api/channels/:id/stop` (soft delete)

## Env Vars
| Var | Default | Description |
|---|---|---|
| `SIMLIVE_QUALITY` | `720p` | Active quality tier |
| `SIMLIVE_RENDITIONS` | `360p,720p,1080p` | Active rungs (comma list) |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `TMP_DIR` | `/tmp/simlive` | Temporary segment storage |
| `PLANNER_INTERVAL` | `5m` | Planner cron interval |
| `SIMLIVE_KEEP_BEHIND` | `5` | Segments to keep behind current |
| `SIMLIVE_DOWNLOAD_AHEAD` | `5` | Segments to download ahead |
| `ENABLE_CLEAN_UP` | `false` | Cold start on restart when true |

## Resilience
- Engine reconciler gates on `is_active=true AND status='LIVE'`
- Planner is SINGLE writer of `status='LIVE'` (`markChannelLive`)
- Legacy timeline flush: `HasLegacyTimeline` / `DeleteTimeline` (first tick after M1 upgrade)
- Shared segment cache: one set of `seg_N.ts` for all 3 rungs (M1.5)
- M2 (true per-rung separate `.ts` files) deferred

## Tests
- `internal/models/models_test.go` — SegmentItem JSON serialization, ZSET member format
- `internal/models/scte35_fields_test.go` — SpliceInsert field validation
- `internal/planner/planner_test.go` — ProcessEPGToTimeline, resolveRenditionsFor
- `internal/engine/engine_test.go` — tick loop, playlist generation, PTS rewrite
- `internal/api/handler_test.go` — channel CRUD, HLS routes, path guard

## Tech Stack
- Backend: Go 1.26.6, gin-gonic/gin v1.12.0
- Database: PostgreSQL (gorm.io/driver/postgres v1.6.2 + gorm v1.31.2)
- Cache: Redis (redis/go-redis/v9 v9.22.0)
- Media: ffmpeg (PTS/DTS rewrite), m3u8-parser (client), video.js + videojs-contrib-hls (player)
- SCTE-35: github.com/Comcast/scte35-go v1.8.0
- Storage: rclone CLI
- Client: Vue 3.4.29 + Vite 5 + Vuetify 3.7.2 + TypeScript
- Build: makefile (Go binaries), pnpm (client), docker-compose.yml (orchestration)
- Config: joho/godotenv v1.5.1, internal/configs/configs.go
