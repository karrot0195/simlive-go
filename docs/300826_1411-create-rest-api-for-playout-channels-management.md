<!--
  Full plan for the playout-channel REST API surface.
  Generated via the simlive-plan skill (./.github/skills/simlive-plan).
-->

# REST API for Playout Channel Management

**Plan version:** 0.1
**Date:** 2026-08-30
**Author:** (agent, via simlive-plan skill)
**Status:** Draft

> Continues the **RegisterChannel** MVP in
> [`internal/api/handler.go`](../internal/api/handler.go#L1-L60) and the
> existing ESPX HTTP client in
> [`internal/api/espx_client.go`](../internal/api/espx_client.go#L1-L122).
> No planner/engine changes — this plan lives entirely in `internal/api`
> and `cmd/api/main.go`.

---

## 0. Progress

Track the work; tick as you go.

- [ ] Read pre-flight context (`docs/specification.md`, summary instructions,
      previous plans `290826_1448-…`, `300826_0213-…`, `300826_0214-…`,
      `300826_0236-…`, `300826_0620-…`, target package).
- [ ] Drafted §1–§7 below.
- [ ] Reviewed by [TODO: reviewer].
- [ ] Implementation started.
- [ ] Implementation merged.
- [ ] Verification (§6) passed end-to-end.
- [ ] Post-task docs updated (`docs/specification.md` §2 + API section, summary instructions).

---

## 1. Problem Statement

Today the API surface in `cmd/api/main.go` exposes exactly **one** business
endpoint:

```go
// cmd/api/main.go (current, ~L95)
apiGroup.POST("/channels", channelHandler.RegisterChannel)
```

— which only **upserts** a single channel by `channel_id` after validating it
against the ESPX detail endpoint
(`internal/api/handler.go#L23-L60`). Everything else is the **read-side**
HLS surface (`GET /live/:channel_id/playlist.m3u8`, the M1.5 per-rung
routes, the `GET /live/:channel_id/:segment` segment route).

Operators have **no** way to:

1. **List** the available playout channels. They either have to ask the
   ESPX dashboard or hand-craft an HTTP call to
   `GET https://api2.espx.cloud/api/v2/common/channel-playouts` (spec §3
   "API 1"). The client wrapper already exists
   (`internal/api/espx_client.go#L18-L32`), it just isn't served.
2. **Read** a single channel from the local `playout_channels` table —
   `is_active` / `last_error` / `status` are unreachable from outside the
   planner.
3. **Update / delete** a channel. The current upsert only refreshes
   ESPX-derived fields; renaming a channel or retiring it has no path.
4. **Start / stop** a playout. There is no operational command that flips a
   channel between "planner walks this channel" and "planner skips it"
   without `psql`. The breaker logic in
   `internal/planner/planner.go` already auto-disables a channel after 5
   consecutive `(channel, date)` failures
   (`docs/specification.md` §9, "Repeated Channel Failures"), but there is
   no manual control.

The cause is structural — the only model field the planner/engine share for
the channel is `PlayoutChannel` (`internal/models/models.go#L9-L19`), and
`ChannelHandler` only knows how to upsert by `id`.

---

## 2. Solution Overview

A new REST surface mounted at `/api/channels/*` with the **nine** endpoints
the operator needs. The shape is deliberately CRUD-flavoured:

| Verb   | Path                                | Role |
|--------|-------------------------------------|------|
| `GET`  | `/api/channels`                     | List **local** channels (DB-backed, paginated) |
| `GET`  | `/api/channels/esp`                 | List **ESPX** channels (proxied from API 1) |
| `POST` | `/api/channels`                     | Create / upsert (existing `RegisterChannel`) |
| `GET`  | `/api/channels/:id`                 | Read a single channel from the DB |
| `PUT`  | `/api/channels/:id`                 | Full update (name, base_name, timezone, epg_id, is_active, status) |
| `PATCH`| `/api/channels/:id`                 | Partial update (any subset of the above) |
| `DELETE`| `/api/channels/:id`                | Soft-delete: flip `is_active=false`, `status='INACTIVE'` |
| `POST` | `/api/channels/:id/start`           | Flip `is_active=true`, `status='ACTIVE'`, clear breaker counter |
| `POST` | `/api/channels/:id/stop`            | Flip `is_active=false`, `status='INACTIVE'` (no DB delete) |

The endpoints split cleanly into two responsibilities:

- **`internal/api/espx_client.go`** — already exposes `GetChannels()`
  (`L18-L32`). A new thin wrapper `ListESpxChannels(ctx)` proxies it,
  returning the ESPX JSON verbatim (or a normalised list with a
  `registered` boolean derived from a single `SELECT id` over the DB).
  No new HTTP calls beyond what API 1 already does.
- **`internal/api/handler.go`** — grows `ChannelHandler.ListChannels`,
  `GetChannel`, `UpdateChannel`, `PatchChannel`, `DeleteChannel`,
  `StartChannel`, `StopChannel`. The existing `RegisterChannel` is
  renamed to `CreateOrUpsertChannel` (route still `POST /api/channels`)
  for naming consistency.

**Start/Stop semantics.** The `PlayoutChannel` model already carries
`Status` (`'ACTIVE'` / `'INACTIVE'` / `'ERROR'`); the planner's cron loop
filters active channels via the existing `is_active` column (see
`docs/specification.md` §5 Module 1, "Query active channels from
`playout_channels`"). `start` / `stop` simply flip those two columns and
return. To avoid the planner auto-re-enabling a channel whose breaker
counter is still hot, `start` also deletes the
`planner:failures:{channelID}` Redis key (the same key the breaker logic
increments — see `.github/instructions/simlive-summary.instructions.md`,
"Resilience & observability (planner)", breaker paragraph). `stop` leaves
the counter intact so a re-start behaves identically to a fresh start.

**Invariants preserved:**

- `is_active=false` channels are still honoured by the planner's worker
  pool loop (no race with the manual control — they read the same column).
- The 5-channel-failure auto-disable breaker in
  `internal/planner/planner.go` is **untouched**; `start` only clears the
  counter after a manual stop, not during normal operation.
- The `PlayoutChannel` schema in `docs/specification.md` §2 (PG `playout_channels`
  table) is **not** modified — every new field is read/written via the
  existing columns. A future plan can add `last_started_at`, `last_stopped_at`,
  `last_error`, etc. (see §7).

No new env vars. No new dependencies.

---

## 3. What Changed (by file)

### `internal/api/espx_client.go`

| Area | Change |
|------|--------|
| `ESPXClient.ListESpxChannels(ctx)` (new) | Thin wrapper over `GetChannels` that returns `[]models.ESPXChannelListItem`; keeps the same 10s timeout and JSON decode. Re-uses the existing URL constant from `GetChannels` (`espx_client.go#L19`) |

> Reuses the existing `internal/api/espx_client.go#L18-L32` — no new HTTP
> round-trips, no schema breaks.

### `internal/models/models.go`

| Area | Change |
|------|--------|
| `ESPXChannelListItem` (new, promoted from anonymous struct) | `{ ID, Name, BaseName, ContentTimezone }` with `json` tags — used by both `ESPXChannelListResp.Data` and the new `/api/channels/esp` route so callers get a stable shape regardless of source |
| `UpdateChannelReq` (new) | `Name *string \`json:"name,omitempty"\`` / `BaseName *string` / `ContentTimezone *string` / `ChannelEpgID *string` / `IsActive *bool` / `Status *string`. Pointer fields so `PUT` can do a full replace while `PATCH` skips zero values cleanly. Validate `Status ∈ {"ACTIVE","INACTIVE","ERROR"}` server-side |
| `ListChannelsResp` (new) | `{ Data []PlayoutChannel \`json:"data"\`; Total int64 \`json:"total"\`; Page int \`json:"page"\`; PageSize int \`json:"page_size"\` }` |
| `ESpxChannelsListResp` (new) | `{ Data []ESPXChannelListItem \`json:"data"\`; Registered map[string]bool \`json:"registered"\` }` — the boolean map is keyed by ESPX channel id and tells the operator at a glance which upstream channels are already in our DB |
| `PlayoutChannel` (no schema change) | Keep `Status`/`IsActive` semantics; the new endpoints write through these existing fields |

### `internal/api/handler.go`

| Area | Change |
|------|--------|
| `ChannelHandler` fields | Add `rdb *redis.Client` so `start` can `DEL planner:failures:{id}` |
| `NewChannelHandler` signature | Becomes `NewChannelHandler(db *gorm.DB, espx *ESPXClient, rdb *redis.Client) *ChannelHandler` — caller in `cmd/api/main.go` updates |
| `RegisterChannel` → `CreateOrUpsertChannel` | Pure rename; route is still `POST /api/channels`. Behaviour unchanged (upsert with ESPX validation) |
| `ListChannels(c)` (new) | Query params: `?page=1&page_size=20&status=ACTIVE&is_active=true&tz=Asia/Saigon&q=<name-contains>`. Builds a `*gorm.DB` with optional filters; returns `ListChannelsResp`. Default sort: `created_at DESC`. Page size clamped to `[1, 100]` |
| `ListESpxChannels(c)` (new) | Calls `espx.ListESpxChannels`; single `SELECT id FROM playout_channels WHERE id IN (...)` produces the `registered` map; returns `ESpxChannelsListResp`. Returns `502` on ESPX error |
| `GetChannel(c)` (new) | `404` when not found, `200 + PlayoutChannel` otherwise |
| `UpdateChannel(c)` (new) | Validates path `:id` against `..` / `/` (same guard as the HLS routes); validates `Status` enum when non-nil; full `db.Save(&channel)` |
| `PatchChannel(c)` (new) | Builds `clause.AssignmentColumns` from the non-nil fields of `UpdateChannelReq` so empty fields aren't overwritten — same upsert pattern as the existing `RegisterChannel` (`handler.go#L40-L51`) |
| `DeleteChannel(c)` (new) | **Soft delete**: `db.Model(&PlayoutChannel{}).Where("id = ?", id).Updates(map[string]any{"is_active": false, "status": "INACTIVE", "updated_at": time.Now()})`. Returns `204`. Idempotent (subsequent deletes return `204`, not `404`) |
| `StartChannel(c)` (new) | `404` if not found; `Updates({is_active:true, status:"ACTIVE", updated_at:now})`; **then** `rdb.Del(ctx, "planner:failures:"+id)` (best-effort — log + ignore Redis errors so a Redis blip doesn't 500 the operator). Returns `200 + PlayoutChannel` |
| `StopChannel(c)` (new) | Mirror of `StartChannel`; flips `is_active=false`, `status='INACTIVE'`; does **not** touch the breaker counter |

### `cmd/api/main.go`

| Area | Change |
|------|--------|
| Imports | Add `time` (already imported). No new modules |
| `channelHandler := api.NewChannelHandler(db, espxClient)` | Becomes `api.NewChannelHandler(db, espxClient, rdb)` — `rdb` is already constructed at `main.go:L40` |
| `apiGroup.POST("/channels", channelHandler.RegisterChannel)` → `CreateOrUpsertChannel` | Pure rename |_0620
| New routes (in `apiGroup`) | `GET /channels`, `GET /channels/esp`, `GET /channels/:id`, `PUT /channels/:id`, `PATCH /channels/:id`, `DELETE /channels/:id`, `POST /channels/:id/start`, `POST /channels/:id/stop`. Register `:id`-bearing routes **after** the `/channels/esp` literal so Gin's radix tree treats `esp` as a static prefix (mirrors the `playlist_:rendition` ordering rule from the M1.5 plan, `300826-multi-rendition-abr-ladder.md` §3 "cmd/api/main.go") |
| Path-param guard | Add a small `channelIDGuard(c)` helper that returns `400` when the `:id` contains `..` or `/` — used by **every** `:id` route. Keeps the spirit of the existing HLS guards (`cmd/api/main.go#L66-L70`, `L98-L101`, `L122-L125`) |

### `internal/api/handler_test.go`

New tests (pure, no Redis required — they exercise the handler logic with
a `nil` Redis on the `start`/`stop` paths; the Redis branch is exercised
in the integration smoke test in §6):

| Test | Asserts |
|------|---------|
| `TestListChannels_ClampsPageSize` | `?page_size=999` → clamped to `100`; `?page_size=0` → `20` (pure helper) |
| `TestUpdateChannel_RejectsBadStatus` | `Status: "FOO"` → `400` |
| `TestUpdateChannel_RejectsBadPathID` | `:id = "../etc"` → `400` (path-guard) |
| `TestPatchChannel_SkipsZeroFields` | `UpdateChannelReq{Status: ptr("ACTIVE")}` → only the `status` column is in `AssignmentColumns`, not name / tz / etc. |
| `TestStartStop_Idempotent` | `StartChannel` twice on the same id → second call returns `200`, both produce the same end-state |
| `TestStartStop_404OnUnknown` | Unknown id → `404` |
| `TestCreateOrUpsertChannel_StillValidatesESPX` | ESPX 404 → `400 Invalid Channel ID or not found in ESPX API` (regression — the rename must not drop the validation) |
| `TestRungPlaylistGuard_*` / `TestSegmentGuard_*` / `TestGinEngine_Smoke` | Unchanged — they live in the same file and must keep passing |

### `docs/specification.md`

| Section | Change |
|---------|--------|
| §2 Database Schema | Note that `playout_channels.is_active` / `status` are now also written by the API (manual `start`/`stop`) in addition to the planner's breaker loop. Mention `soft delete` semantics |
| §3 ESPX API Integration | Add a "REST API Surface" sub-section enumerating the 9 routes + their request/response shapes |
| §5 Module 1 (Planner) | Add a paragraph: "Manual control via `POST /api/channels/:id/start|stop`. The planner reads the same `is_active` column, so manual flips take effect on the next planner tick (default 5m, `PLANNER_INTERVAL`). The `start` endpoint also deletes `planner:failures:{id}` to reset the breaker" |
| §8 Env Vars | No new env vars — table unchanged |
| §9 Error Handling | Add: `start`/`stop` are best-effort on Redis (a Redis blip returns `200` with a logged warning, never `5xx`) |

### `.github/instructions/simlive-summary.instructions.md`

| Section | Change |
|---------|--------|
| Architecture snapshot | Add a one-line bullet: "**API** also exposes `GET/POST/PUT/PATCH/DELETE /api/channels[/...]` for channel CRUD + `start`/`stop` (see `internal/api/handler.go`). The planner reads the same `is_active`/`status` columns, so manual flips take effect on the next planner tick" |
| Operational Details / Resilience | Note that `POST /api/channels/:id/start` clears the breaker counter key `planner:failures:{id}` |

---

## 4. Data Flow

```text
[Operator / Dashboard]
   │
   │  HTTP (Gin) ──── /api/channels[/...]
   ▼
[cmd/api/main.go]
   │
   ▼
[ChannelHandler (internal/api/handler.go)]
   │
   ├─► [ESPXClient.ListESpxChannels] ──HTTP──► [api2.espx.cloud  GET /api/v2/common/channel-playouts]
   │                                                          │
   │                                                          ▼
   │                                              [api2.espx.cloud response]
   │                                                          │
   │       + single SELECT id FROM playout_channels  ◄────────┘
   │                                                          │
   │                                                          ▼
   │                                              [{id,name,base_name,timezone, registered:true|false}, ...]
   │
   ├─► [GORM] playout_channels (insert/update/select) ──► [PostgreSQL]
   │
   └─► [go-redis] DEL planner:failures:{id}     (only on POST /:id/start)
                                  │
                                  ▼
                            [Redis 6371]
                                  ▲
                                  │  (no change to planner; existing auto-disable still works)
                                  │
                            [Planner (cron loop, every PLANNER_INTERVAL)]
                                  │
                                  ▼
                            [Engine + API HLS surface — unchanged]
```

---

## 5. Configuration / Env Vars / Constants

**No new env vars.** All behaviour is driven by the existing
`DB_*`, `REDIS_ADDR` (default `localhost:6371`, **not** 6379 — see
`docs/specification.md` §8), and `ESPX_API_URL` settings.

New in-source constants (all in `internal/api/handler.go`):

| Name | Default | Description |
|------|---------|-------------|
| `defaultListPageSize` | 20 | Default `?page_size=` for `GET /api/channels` |
| `maxListPageSize` | 100 | Hard ceiling; values above are clamped |
| `breakerKeyPrefix` | `"planner:failures:"` | Mirrors `internal/planner/planner.go`'s prefix — single source of truth to be promoted to a `const` if the planner is refactored in a follow-up |

---

## 6. Verification Plan

> Planner rebuild flushes Redis and wipes `./outputs` — do **not** start
> the planner as part of API verification. Only `cmd/api/main.go` + a
> manually-populated `playout_channels` row + Redis are needed.

1. **Build.**
   ```bash
   GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...
   ```
   Clean.

2. **Unit tests.**
   ```bash
   GOCACHE=$PWD/.gocache go test ./internal/api/... ./internal/models/...
   ```
   All pure tests pass; the existing `rungPlaylistGuard` /
   `segmentGuard` / `GinEngine_Smoke` regressions stay green.

3. **Manual DB seed.**
   ```bash
   psql -h localhost -U postgres -d simlive \
     -c "INSERT INTO playout_channels (id, name, base_name, content_timezone, channel_epg_id, status, is_active) VALUES ('test-ch-1','Test 1','test-1','Asia/Saigon','epg-1','ACTIVE',true);"
   ```

4. **Endpoint smoke** (with the API process running on `:8080`):

   | Call | Expected |
   |------|----------|
   | `curl -s :8080/api/channels` | `{"data":[{...test-ch-1...}], "total":1, "page":1, "page_size":20}` |
   | `curl -s :8080/api/channels?status=ACTIVE&page_size=5` | Same shape, `total:1` |
   | `curl -s :8080/api/channels/test-ch-1` | `200 + PlayoutChannel JSON` |
   | `curl -s :8080/api/channels/does-not-exist` | `404` |
   | `curl -s -X PUT :8080/api/channels/test-ch-1 -d '{"name":"Renamed","status":"INACTIVE"}' -H 'Content-Type: application/json'` | `200 + updated row`, then `SELECT status FROM playout_channels WHERE id='test-ch-1'` returns `INACTIVE` |
   | `curl -s -X PUT :8080/api/channels/test-ch-1 -d '{"status":"FOO"}' -H 'Content-Type: application/json'` | `400 Invalid status` |
   | `curl -s -X PATCH :8080/api/channels/test-ch-1 -d '{"status":"ACTIVE"}' -H 'Content-Type: application/json'` | `200 + updated row`, name unchanged |
   | `curl -s -X DELETE :8080/api/channels/test-ch-1` | `204`; `SELECT is_active, status FROM playout_channels WHERE id='test-ch-1'` returns `(false,'INACTIVE')` |
   | `curl -s -X POST :8080/api/channels/test-ch-1/start` | `200 + {is_active:true, status:'ACTIVE'}`; `redis-cli -p 6371 GET planner:failures:test-ch-1` → `(nil)` after the call |
   | `curl -s -X POST :8080/api/channels/test-ch-1/stop` | `200 + {is_active:false, status:'INACTIVE'}`; `redis-cli -p 6371 GET planner:failures:test-ch-1` → still `(nil)` if it was already nil, otherwise **unchanged** (counter preserved) |
   | `curl -s :8080/api/channels/esp` | `200 + ESpxChannelsListResp` with `registered` map keyed by ESPX id |
   | `curl -s :8080/api/channels/../etc` | `400 Invalid channel ID` (path-guard) |

5. **Grep-able logs.** `journalctl -u simlive-api | grep "\[api\]"` —
   every handler logs `[api] <verb> <route> -> <status>` for scraping.
   The grep pattern is the operator's quick "is everything OK" check.

6. **No-op on planner/engine.** Confirm the planner + engine are **not**
   running during this test (the plan is API-only). If they are, the
   planner's breaker auto-disable logic must still win over the manual
   flip after 5 consecutive failures — verify by stubbing a Redis
   `INCR planner:failures:{id}` to 4 and confirming `start` clears it,
   then running the planner until it ticks and seeing the channel picked
   up.

7. **CORS sanity.** The existing `corsMiddleware` (`cmd/api/main.go:L168`)
   already permits `OPTIONS` preflight on any method, so the new `PUT`,
   `PATCH`, `DELETE` verbs work from a browser origin without extra
   configuration. Confirm via `curl -i -X OPTIONS :8080/api/channels/test-ch-1` →
   `204` with `Access-Control-Allow-Methods: …, PUT, PATCH, DELETE, …`.

**Success metrics** (table the implementer fills in):

| Metric | Target |
|--------|--------|
| All 9 endpoints respond | `9/9` |
| Unit tests pass | `>= 14` (existing 7 + new 7) |
| Path-guard rejects `..` and `/` | `100%` |
| Soft-delete preserves the row | yes (no `DELETE FROM playout_channels` issued) |
| `start` clears the breaker counter | yes |
| `stop` does NOT clear the breaker counter | yes |

---

## 7. Known Artifacts / Remaining Work

- **No hard delete.** `DELETE` is a soft flip. A future plan can add
  `DELETE /api/channels/:id?hard=true` with admin-only auth.
- **No auth.** The whole API is open on `:8080`, matching today's
  behaviour (no `gin-contrib/cors` and no middleware). Adding JWT/API-key
  auth is a separate, cross-cutting concern.
- **No bulk endpoints.** `start`/`stop` are per-channel. A bulk
  `POST /api/channels/start-all` is out of scope.
- **No structured audit log.** Each flip is logged via the
  `[api] <verb> <route> -> <status>` line; a future plan can write
  `playout_channel_audit` rows for compliance.
- **`status='ERROR'` is set by the planner breaker today; manual flips
  cannot set it** (only `ACTIVE`/`INACTIVE` accepted by the handler's
  enum validation). Operators must `start` a channel that was auto-disabled
  — that's intentional (the planner may have disabled it for a good
  reason).
- **No `last_started_at` / `last_stopped_at` / `last_error` columns.**
  These would require a `playout_channels` schema migration (see
  `docs/specification.md` §2). Out of scope; tracked here so the next
  plan that touches the schema picks them up cleanly.
- **No pagination on `GET /api/channels/esp`.** The ESPX upstream
  doesn't page; we surface the full list. If the fleet grows past ~hundreds
  of channels, add `?limit=` upstream-side filtering.
- **`POST /api/channels` and `POST /api/channels/:id/start` differ on
  validation.** The first validates against ESPX (so a typo doesn't make
  it into the DB); the second assumes the row already exists. This
  asymmetry is intentional: `create` is the bootstrap, `start` is the
  recovery path.

---

## References

- [docs/specification.md](../docs/specification.md) §2 (DB schema),
  §3 (ESPX API 1), §5 (Planner Module 1 — active-channel query),
  §8 (env vars), §9 (breaker / repeated channel failures).
- [internal/api/handler.go](../internal/api/handler.go) (current
  `RegisterChannel`, `PlayoutChannel` upsert with `clause.OnConflict`).
- [internal/api/espx_client.go](../internal/api/espx_client.go)
  (`GetChannels`, `GetChannelDetailWithEPG`, `GetSchedules`).
- [internal/models/models.go](../internal/models/models.go) (current
  `PlayoutChannel`, `ESPXChannelListResp`, `CreateChannelReq`).
- [cmd/api/main.go](../cmd/api/main.go) (current router,
  `corsMiddleware`, HLS route shape to mirror for path guards).
- [docs/300826_0620-multi-rendition-abr-ladder.md](300826_0620-multi-rendition-abr-ladder.md)
  — the `:rendition`-vs-static-prefix Gin ordering rule that this plan
  re-uses for `/channels/esp` vs `/channels/:id`.
- [.github/instructions/simlive-summary.instructions.md](../.github/instructions/simlive-summary.instructions.md)
  — architecture snapshot + breaker counter key prefix
  `planner:failures:{id}`.