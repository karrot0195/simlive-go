# SimLive-Go Conventions (for plan authors)

> Pulled from `AGENTS.md`, `docs/specification.md`, and the existing plans
> (`270826-…`, `290826-…`). Read this before drafting a plan so you don't
> re-derive the rules from scratch.

## Invariants — never break these

1. **VOD ↔ ads resolution match.** A mid-stream resolution change inside one
   MSE SourceBuffer fails playback even when PTS is continuous. VOD dir and
   ads dir must resolve to the same `(w, h)`. See `docs/specification.md` §6.1.
2. **Local `PtsOffset` timeline.** Every segment carries
   `PtsOffset = MediaStart − timelineBase` (seconds, local). The engine
   rewrites PTS/DTS to this value. Avoid the 2^33/90000 ≈ 95443.7s epoch
   PTS wrap — keep offsets small and local.
3. **Bounded on-disk active window.** The engine keeps at most
   `SIMLIVE_KEEP_BEHIND + 5 + SIMLIVE_DOWNLOAD_AHEAD` `.ts` files per
   channel (default 15). Cleanup is driven by this set every tick.

## Module ownership

| Module | Owns |
|---|---|
| `internal/planner/planner.go` + `segment_processor.go` | EPG fetch, m3u8 parse, ad insertion, local PtsOffset, quality tier / rendition selection, Redis ZSET writes. |
| `internal/engine/engine.go` | 1s tick, sliding window, async download, PTS/DTS rewrite, `playlist.m3u8` (prepared segs only), per-tick GC. |
| `internal/api/handler.go` + `espx_client.go` | REST endpoints + HLS playlist/segment serving at `:8080/live/{channelID}/`. |
| `internal/storage/rclone.go` | Thin wrapper around `rclone cat` / `rclone copyto`. |
| `internal/models/models.go` | `SegmentItem` (PTS fields), `PlayoutChannel`, `ESPXScheduleItem`. |
| `internal/configs/configs.go` | TZ constants, date formats (compile-time). |
| `internal/utils/envs.go` | `GetEnv`, `GetEnvInt` (engine), `GetEnvFloat`. |
| `internal/utils/times.go` | TZ-aware formatting helpers. |

## Quality tiers (SIMLIVE_QUALITY)

| Tier | Target | VOD dir | Ads dir |
|------|--------|---------|---------|
| 360p  | 640×360  | `L_0` | `L2` |
| 720p  | 1280×720 | `L_1` | `L1` |
| 1080p | 1920×1080| `L_2` | `L0` |
| `auto` (default) | — | highest equal-resolution pair | highest equal-resolution pair |

Hard overrides beat the tier: `SIMLIVE_VOD_RENDITION` + `SIMLIVE_ADS_RENDITION`
(both set).

## Environment variables (full table lives in `docs/specification.md` §8)

Most relevant when planning:
- `SIMLIVE_QUALITY` — `auto` | `360p` | `720p` | `1080p`.
- `SIMLIVE_VOD_RENDITION` / `SIMLIVE_ADS_RENDITION` — hard overrides.
- `TMP_DIR` — engine local segment cache (default `./outputs`).
- `SIMLIVE_KEEP_BEHIND` (5) / `SIMLIVE_DOWNLOAD_AHEAD` (5) — bounded active
  window edges.
- `MAX_WORKERS` / `WORKER_POOL_SIZE` (50) — download concurrency.
- `ENABLE_CLEAN_UP` (`false`) — cache cleanup toggle.
- `REDIS_ADDR` (default `localhost:6371`, **not 6379**).
- `TZ_NUM` — timezone offset for API queries.

## Constants worth naming in plans

| Constant | Value | Module |
|---|---|---|
| `WindowItemSize` | 50 | planner (Redis batch size) |
| `AdInsertionInterval` | 30 s | planner |
| `WindowBufferSeconds` | 120 s | planner (lookback for inclusion) |
| `RedisExpiryHours` | 26 h | planner (timeline key TTL) |
| `DefaultKeepBehind` / `DefaultDownloadAhead` | 5 / 5 | engine |
| Playlist window | 5 segments | engine |
| Lookback / lookahead (Redis query) | −300 s / +120 s | engine |
| Tick interval | 1 s | engine |
| HLS playlist Redis TTL | 10 s | engine |

## Don'ts (re-confirmed)

- **Don't start the planner** in any verification step. Planner rebuild
  flushes Redis (`FLUSHALL`) and wipes `./outputs` — only run on demand.
- **Don't add a test artifact** to `./outputs` — the engine wipes `TMP_DIR`
  on startup.
- **Don't introduce synchronous downloads** inside the engine tick. Use the
  worker pool + `inFlight` dedupe.
- **Don't break the dedupe contract** — never download/rewrite the same
  `seg_{seq}.ts` twice.
- **Don't advertise unprepared segments** in `playlist.m3u8`. The `.pts`
  marker is the contract.
- **Don't run the verifier binary** unless you know what it does — it may
  be empty/stub.

## Plan-to-implementation hand-off

When the plan moves to implementation, the implementer must:
1. Update `docs/specification.md` and `.github/instructions/simlive-summary.instructions.md`
   for every section the change actually affects (post-task rule).
2. Keep edits **English-only** and **minimal** — no drive-by refactors.
3. Re-read the changed files for compile errors and invariant preservation.
4. Ask the user to run
   `GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...` (the agent must
   **not** run terminal commands itself).
5. For planner/engine changes, mentally trace the AGENTS.md verification
   workflow before declaring done.
