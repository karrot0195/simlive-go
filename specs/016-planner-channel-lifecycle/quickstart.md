# Quickstart Validation: Planner & Engine Channel Lifecycle Reactivity

**Feature**: `013-planner-channel-lifecycle` | **Date**: 2026-11-03

This is the stack-level validation required by the constitution for playout changes. It proves the outcomes in [spec.md](./spec.md); implementation details and task breakdown belong in `tasks.md` and the code.

## Prerequisites

- Go 1.26+, `make`, `curl`, `psql`, `redis-cli`
- Postgres and Redis from `docker-compose.yml` running
- `.env` configured for the local stack (`DB_*`, `REDIS_ADDR`, `PARTNER_API_URL`, `TMP_DIR`)
- At least two valid PARTNER channels available for registration
- Optional for latency tuning: set `CHANNEL_SYNC_INTERVAL=2s` (default) and `PLANNER_INTERVAL=1m`

## Setup

```bash
docker compose up -d
make build
make start-all          # api + planner + engines, logs under logs/
make status
curl -s http://localhost:8080/api/channels | jq '.total'
```

Register two channels (replace ids with real PARTNER playout ids):

```bash
curl -s -X POST http://localhost:8080/api/channels \
  -H 'Content-Type: application/json' \
  -d '{"channel_id":"<CHANNEL_A_ID>"}'
curl -s -X POST http://localhost:8080/api/channels \
  -H 'Content-Type: application/json' \
  -d '{"channel_id":"<CHANNEL_B_ID>"}'
```

Confirm both are active and preparing:

```bash
psql "$DATABASE_URL" -c \
  "SELECT id, status, is_active FROM playout_channels ORDER BY id;"
redis-cli ZCARD simlive:timeline:<CHANNEL_A_ID>
redis-cli ZCARD simlive:timeline:<CHANNEL_B_ID>
```

## Scenario A — Stop cancels all work (US1, SC-001, SC-005)

1. Record `date +%s.%N` and the planner/engine log offsets.
2. `curl -s -X POST http://localhost:8080/api/channels/<CHANNEL_A_ID>/stop`
3. Watch for the stop effects:

```bash
psql "$DATABASE_URL" -c \
  "SELECT id, status, is_active FROM playout_channels WHERE id='<CHANNEL_A_ID>';"
redis-cli EXISTS simlive:hls:<CHANNEL_A_ID>
redis-cli EXISTS simlive:timeline:<CHANNEL_A_ID>
redis-cli EXISTS planner:cursor:<CHANNEL_A_ID>
ls "$TMP_DIR/<CHANNEL_A_ID>" 2>&1 | head
grep -E "lifecycle_stop|context done|cancelled" logs/planner.log logs/engines.log | tail -20
```

**Expected within 10s (p95) / 30s (max)**: `status='INACTIVE'`, `is_active=false`, HLS master/rendition keys and timeline/cursor absent, disk directory removed, planner/engine log a stop/cancel. No later log line shows A preparing or playing.

## Scenario B — Reactivate with `/start` (US2, SC-002)

1. Record a timestamp, then `POST /api/channels/<CHANNEL_A_ID>/start`.
2. Confirm within the target latency:
   - row is `PREPARE`/`is_active=true`
   - planner log shows preparation starting for A
   - `simlive:timeline:<CHANNEL_A_ID>` grows again
   - engine log shows a playout loop starting for A
3. Confirm `status` becomes `LIVE` only after preparation succeeds.

## Scenario C — Reactivate with a status change (US2, the "need verify" path)

Channel A is currently inactive.

```bash
curl -s -X PATCH http://localhost:8080/api/channels/<CHANNEL_A_ID> \
  -H 'Content-Type: application/json' \
  -d '{"status":"LIVE"}'
```

**Expected**: row becomes `status='LIVE'`, `is_active=true`; planner begins work (it will downgrade to `PREPARE` only through the normal system path if needed) and engine starts/keeps the playout loop within the target latency.

Then verify the equivalent `INACTIVE` path stops work:

```bash
curl -s -X PATCH http://localhost:8080/api/channels/<CHANNEL_A_ID> \
  -H 'Content-Type: application/json' -d '{"status":"INACTIVE"}'
```

## Scenario D — Delete cancels and purges (US1, FR-002, FR-009)

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE \
  http://localhost:8080/api/channels/<CHANNEL_A_ID>
psql "$DATABASE_URL" -c \
  "SELECT count(*) FROM playout_channels WHERE id='<CHANNEL_A_ID>';"
redis-cli --scan --pattern '*<CHANNEL_A_ID>*'
```

**Expected**: HTTP `204`, row count `0`, no channel-specific Redis keys, no disk directory, and no planner/engine work for A after the target latency.

## Scenario E — Engine parity (US4)

With channel B active and playing:

1. Verify `redis-cli GET simlive:hls:<CHANNEL_B_ID>` returns a playlist and `logs/engines.log` shows `channel playout` lines.
2. Stop B (`POST /stop`).
3. Within the target latency confirm the `channel playout` lines stop, the HLS master/rendition keys are gone, and the disk directory is removed.
4. Start B again and confirm `channel playout` lines resume without restarting the engine process.

**Failure-disabled parity check**: simulate the planner failure path (or directly `UPDATE playout_channels SET is_active=false WHERE id='<CHANNEL_B_ID>'`) and confirm the engine stops playout even if `status` is still `LIVE`.

## Scenario F — Activation is not blocked by other work (US2, SC-006)

1. Keep B active and force a long preparation (for example, a channel with a cold EPG cache).
2. While B's preparation is in progress, add channel A.
3. **Expected**: A's preparation starts within the target latency; B's output is not interrupted.

## Scenario G — No resurrection (US1, FR-003, SC-003)

Run 20 stop/start cycles on one channel. After each stop:

- confirm the row stays `INACTIVE`/`false` until the next explicit start
- confirm no `status='LIVE'` transition occurs for the stopped channel
- confirm the planner never logs a prepare for a stopped channel

## Scenario H — Lifecycle logging (US5, SC-007)

```bash
grep -E "lifecycle_(start|stop|restart|cancel)" logs/planner.log logs/engines.log | tail -40
grep -E "\[api\] channel=.* (start|stop):" logs/api.log | tail -20
```

**Expected**: each decision shows channel id, previous/new state, reason, and (where measurable) latency. An operator can explain why a channel did or did not start.

## Scenario I — Configuration responsiveness (FR-014, SC-008)

1. Stop all services.
2. Set `CHANNEL_SYNC_INTERVAL=10s` in `.env`.
3. Rebuild/restart (`make build && make start-all`) and repeat Scenario B.
4. **Expected**: detection latency visibly follows the configured interval; no code change is required.
5. Restore `CHANNEL_SYNC_INTERVAL=2s` afterwards.

## Scenario J — Startup convergence (FR-013)

1. Leave channel A active and channel B inactive.
2. Restart planner and engines.
3. **Expected**: within the target latency, A is being prepared/played and B has no work, without waiting for a full `PLANNER_INTERVAL` cycle.

## Overall pass criteria

- All scenarios meet the latency targets from [spec.md](./spec.md) (SC-001, SC-002).
- No stopped/deleted channel produces output or has residual keys/work units after 60s.
- Active channels are never interrupted by lifecycle operations on other channels.
- `make test`, `make vet`, and `make fmt` pass.

## Teardown

```bash
make stop
docker compose down
```
