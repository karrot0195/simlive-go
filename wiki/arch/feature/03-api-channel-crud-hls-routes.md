# API — Channel CRUD + HLS Routes (Feature Detail)

> Feature: `api-channel-crud-hls-routes`
> Source: `docs/specification.md` §3 (REST API Surface), `.github/instructions/simlive-summary.instructions.md`, `internal/api/handler.go`, `cmd/api/main.go`, `docs/memories.md`.

## Overview
The local REST API (`cmd/api`) serves two surfaces: (1) playout-channel management (`/api/channels`) and (2) HLS delivery (`/live/{ch}/...`). All `:id` routes apply a path-guard (`..` / `/` → 400). `DELETE` is soft (`is_active=false / status='INACTIVE'`); `POST /start` resets breaker (`planner:failures:{ch}`); `POST /stop` preserves it.

## Routes
| Verb | Path | Role |
| `POST` | `/api/channels` | Create / upsert; validates `channel_id` against ESPX API 2 |
| `GET` | `/api/channels` | List (page, page_size, status, is_active, q) |
| `GET` | `/api/channels/esp` | ESPX upstream + `registered` map |
| `GET` | `/api/channels/:id` | Read one |
| `PUT` | `/api/channels/:id` | Full update (Status enum: PREPARE/LIVE/INACTIVE) |
| `PATCH` | `/api/channels/:id` | Partial update (pointer-field semantics) |
| `DELETE` | `/api/channels/:id` | Soft delete (`is_active=false`, `status='INACTIVE'`, purge Redis) |
| `POST` | `/api/channels/:id/start` | `is_active=true`, `status='PREPARE'`, clear breaker |
| `POST` | `/api/channels/:id/stop` | `is_active=false`, `status='INACTIVE'`, preserve breaker |
| `GET` | `/live/{ch}/playlist.m3u8` | Master playlist (Redis `simlive:hls:{ch}` → disk fallback) |
| `GET` | `/live/{ch}/playlist_:rendition` | Per-rung media (`simlive:hls:{ch}:rendition:{rend}`) |
| `GET` | `/live/{ch}/:segment` | Shared-cache `.ts` segment |

## Key behaviors
- `applyChannelUpdate` (shared by PUT/PATCH): when `status='INACTIVE'` → forces `is_active=false` + `purgeChannelRedisData`.
- `purgeChannelRedisData` deletes: `simlive:hls:{ch}`, `simlive:hls:{ch}:rendition:{rend}` (×3), `simlive:timeline:{ch}`, `simlive:renditions:{ch}`, `planner:cursor:{ch}`. Breaker counter (`planner:failures:{ch}`) is intentionally preserved.
- Engine reconciler starts on `is_active=true AND status != 'INACTIVE'` (PREPARE or LIVE); does NOT wait for LIVE.

## Related docs
- `docs/specification.md` §3
- `.github/instructions/simlive-summary.instructions.md`
- `docs/memories.md` (stop/delete cleanup completeness; planner warm-pickup)
