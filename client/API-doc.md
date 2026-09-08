# Client API Documentation — SimLive-Go

Generated from `internal/api/handler.go` (canonical source). All endpoints served by `cmd/api/main.go` (`http://localhost:8080` default; see `client/src/constants/index.ts`).

> **NOTE vs. `swagger.yaml`:** the swagger file in this repo is **out of date** for the HLS surface. The Go server (`cmd/api/main.go`) uses the **nested** rung path (`/live/{ch}/{rend}/playlist.m3u8`, `/live/{ch}/{rend}/{seg_N.ts}`), not the flat `playlist_{rend}` / flat `/{segment}` shape the swagger describes. The client (`src/libs/hls.ts`) follows the server. Update the swagger file when the routes change — and do not regenerate client URLs from it.

---

## 1. Base URLs

| Constant (`client/src/constants/index.ts`) | Default | Purpose |
|---|---|---|
| `API` | `http://localhost:8080/api` | REST CRUD + start/stop |
| `LIVE_API` | `http://localhost:8080/live` | HLS master + per-rung playlists + `.ts` segments |
| `REST_API` | `https://api2.espx.cloud/api/v2` | Legacy ESPX upstream (no longer used by the client) |

---

## 2. Channel Lifecycle (`handler.go`)

Status flow (planner = SINGLE writer of `LIVE`):
`PREPARE` (register/start) → `LIVE` (timeline in Redis) → `INACTIVE` (stop/soft delete).

- `start`: `is_active=true`, `status=PREPARE`, DEL `planner:failures:{id}`.
- `stop`: `is_active=false`, `status=INACTIVE`, best-effort DEL HLS + timeline + renditions + cursor (preserves breaker).
- `delete` (soft): same DB flip + purge; row preserved.

---

## 3. REST Endpoints (`/api/*`)

All routes are registered on `r.Group("/api")` in `cmd/api/main.go`. Client paths in `src/libs/api.ts` are relative to `baseURL: API` (= `…/api`) and are prefixed with `/channels` so the final URL is `…/api/channels…`.

### POST /api/channels — Create / Upsert
Body: `{"channel_id":"..."}` (validated vs ESPX). Response `200`: `{"message":"...","data":{...PlayoutChannel...}}`. Errors: `400` (invalid/ESPX missing), `500` (DB).

### GET /api/channels — List (paginated)
Query: `?page=1&page_size=20&status=PREPARE&is_active=true&q=search`. `page_size` clamped `[1,100]`. Response: `{"data":[...],"total":N,"page":1,"page_size":20}`.

### GET /api/channels/esp — ESPX upstream + registered map
Response: `{"data":[...],"registered":{"id":true,...}}`.

### GET /api/channels/:id — Get single
Path guard (`..`/`/` → `400`). `404` not found.

### PUT /api/channels/:id — Full update
Body (`UpdateChannelReq`, pointer fields; `nil` = leave alone): `name`, `base_name`, `content_timezone`, `channel_epg_id`, `is_active`, `status` (must be `PREPARE`/`LIVE`/`INACTIVE`). `updated_at` bumps. `404` missing; `400` no fields / bad status.

### PATCH /api/channels/:id — Partial update
Same body/validation as PUT; shares `updateColumnsAndValues` + `assignmentMap`.

### DELETE /api/channels/:id — Soft delete
Idempotent (`204`). Flips `is_active=false`, `status=INACTIVE`, best-effort `purgeChannelRedisData`. Breaker `planner:failures:{id}` preserved.

### POST /api/channels/:id/start — Start
DB: `is_active=true`, `status=PREPARE`. Best-effort DEL breaker. Idempotent (`200` with row; `404` missing). Log includes `PLANNER_INTERVAL` hint.

### POST /api/channels/:id/stop — Stop
DB: `is_active=false`, `status=INACTIVE`. Best-effort purge (HLS master + 3 rung media + timeline + renditions + cursor). Breaker preserved. Idempotent.

---

## 4. HLS Routes (`/live/*` — `cmd/api/main.go`)

Route order matters: `/:channel_id/:rendition/playlist.m3u8` must be before `/:channel_id/:rendition/:segment` (Gin radix shadowing).

### GET /live/{channel_id}/playlist.m3u8 — Master
Reads `simlive:hls:{ch}` (Redis); falls back to `{TMP_DIR}/{ch}/playlist.m3u8`. `404` if neither. `Content-Type: application/x-mpegURL`; `Cache-Control: no-cache`. Path guard (`..`/`/` → `400`).

### GET /live/{channel_id}/{rendition}/playlist.m3u8 — Per-rung media (M2)
`rendition` ∈ `{360p,720p,1080p}` (`hlsRungs`). Unknown → `404`. Reads `simlive:hls:{ch}:rendition:{rend}`; disk fallback `{TMP_DIR}/{ch}/{rend}/playlist.m3u8`.

### GET /live/{channel_id}/{rendition}/{segment} — Segment
`segment` must end `.ts`; else `404`. Path guard on all 3 params (`..`/`/`/`\` → `400`). File: `{TMP_DIR}/{ch}/{rend}/{segment}`.

---

## 5. Redis Key Schema (`handler.go` constants)

| Key | Writer | Purged on stop? |
|---|---|---|
| `simlive:hls:{ch}` | engine (master) | yes |
| `simlive:hls:{ch}:rendition:{360p\|720p\|1080p}` | engine (media) | yes |
| `simlive:timeline:{ch}:{rend}` | planner (per-rung ZSET) | yes |
| `simlive:timeline:{ch}` | planner (legacy) | yes |
| `simlive:renditions:{ch}` | planner (ladder meta) | yes |
| `planner:cursor:{ch}` | planner | yes |
| `planner:failures:{ch}` | planner (breaker) | **NO** (cleared by `start`) |

---

## 6. Client Usage Patterns (workspace)

The legacy `/node-lives` family (`client.get('/node-lives')`, `/node-lives/{uuid}/reset`, `/node-lives/{uuid}` DELETE, and the `/sse/node-live` SSE feed) is **not implemented in the Go server** and **not in `swagger.yaml`**. It was removed from the client.

Current client surface:

- `ChannelEpgPanel.vue`:
  - `api.listChannels({ is_active: true, page_size: 100 })` → visible list.
  - `api.listESpxChannels()` → "Add Channel" picker, filtered to `registered[id] !== true`.
  - `channelStore.startChannel(id)` → flips `status=PREPARE`.
  - `channelStore.stopChannel(id)` → flips `status=INACTIVE`.
  - `channelStore.addChannel(channelId)` → `POST /api/channels`.
- `StreamPanel.vue`: still calls the legacy `/start_live?name=&m3u8Url=` (VOD→live launcher, not yet ported). The HLS link it surfaces uses `masterPlaylistUrl()` from `src/libs/hls.ts`.
- `LiveStream.vue`: `<source :src="masterPlaylistUrl(node.uuid)">`. Polls the master, resolves the `#EXT-X-STREAM-INF` media playlist, parses `#EXT-X-DATERANGE` + `#EXT-X-SCTE35`, keeps last 50 segments.
- `SimulatedLive.vue`: drives the right-hand HLS player from the channels store — picks the first channel with `status ∈ {LIVE, PREPARE}` and falls back to the first active row. Polls `api.listChannels({ is_active: true })` every 5s (replaces the old SSE feed).

---

## 7. Env Vars (`handler.go` / `cmd/api/main.go`)

- `PLANNER_INTERVAL` (default `5m`) — `plannerTickHint()`.
- `REDIS_ADDR` (default `6371`) — `rdb`.
- `TMP_DIR` (default `./outputs`) — disk fallback.

---

*Source of truth: `internal/api/handler.go` + `cmd/api/main.go`. Update when either changes.*
