# Interface Contract: Channel Lifecycle

**Feature**: `013-planner-channel-lifecycle` | **Date**: 2026-11-03

This contract is internal-facing plus the public REST surface. It defines the state predicate, endpoint effects, environment knobs, log events, and Redis purge behavior that planner, engines, API, and operators can rely on.

## 1. Active predicate

```
active(channel) = channel.is_active = true
                  AND channel.status NOT IN ('INACTIVE', 'FAILURE')
```

- The planner creates work only for active channels and cancels work for channels that stop being active.
- The engine creates playout loops only for active channels and cancels them otherwise.
- Any write that would make a channel active again must be an explicit operator request.
- Failure-driven disable (`is_active=false`) is durable: no automatic path re-enables the channel.

## 2. REST endpoints

All routes remain under `/api/channels`.

### POST `/api/channels` (create/upsert)

- Validates the channel against PARTNER.
- Writes `status='PREPARE'`, `is_active=true`, `updated_at`.
- Upsert on an existing id follows the same transition (reactivation).
- Response shape unchanged: `200 {"message": ..., "data": {...}}`.

### GET `/api/channels`

- Unchanged response shape.
- `is_active` is now a real model field, so the serialized channel includes the current value.
- Optional `is_active` query filter is honored (was previously documented but not parsed).

### GET `/api/channels/:id`

- Unchanged; includes `is_active`.

### PUT/PATCH `/api/channels/:id`

Body fields: `name`, `base_name`, `content_timezone`, `channel_epg_id`, `status`, `is_active` (all optional pointers).

Transition table (applies to PUT and PATCH):

| Input | Resulting `status` | Resulting `is_active` | Error |
|---|---|---|---|
| `status=PREPARE` | `PREPARE` | `true` | — |
| `status=LIVE` | `LIVE` | `true` | — |
| `status=INACTIVE` | `INACTIVE` | `false` | — |
| `is_active=true` only | `PREPARE` | `true` | — |
| `is_active=false` only | `INACTIVE` | `false` | — |
| `status=INACTIVE` + `is_active=true` | — | — | `400` contradiction |
| `status=PREPARE|LIVE` + `is_active=false` | — | — | `400` contradiction |

- `updated_at` always bumps.
- `404` on missing id; `400` on no updatable fields or invalid status.
- Effect is observed by planner and engines within `CHANNEL_SYNC_INTERVAL` (default 2s).

### POST `/api/channels/:id/start`

- Writes `status='PREPARE'`, `is_active=true`, `updated_at`.
- Best-effort: clears the failure counter and segment-track state.
- Idempotent: `200` with the row when already active, `404` when missing.
- Planner begins work within `CHANNEL_SYNC_INTERVAL`; engine starts a playout loop for `PREPARE` within the same bound.

### POST `/api/channels/:id/stop`

- Writes `status='INACTIVE'`, `is_active=false`, `updated_at`.
- Best-effort purge immediately: `simlive:hls:{id}`, `simlive:hls:{id}:rendition:*`, `simlive:renditions:{id}`, `simlive:timeline:{id}`, `planner:cursor:{id}`, fallback state; clears the segment-track log. Preserves `planner:failures:{id}`.
- Idempotent: `200` with the row when already stopped, `404` when missing.
- Planner cancels and cleans the work unit within the target latency; engine cancels and purges within the target latency.

### DELETE `/api/channels/:id`

- Remains a **hard delete** of the row (documentation will be corrected to match).
- Triggers the same purge as Stop.
- Idempotent: `204` whether or not the row existed.
- Planner detects the missing channel, cancels in-flight work, and cleans state; engine cancels and purges.

### Reactivation paths (both supported)

1. Re-add the channel (`POST /api/channels`) → `PREPARE`, active.
2. Change state (`POST /start`, or PUT/PATCH with `status=PREPARE|LIVE` or `is_active=true`) → active.
3. Externally flipping the DB columns to an active combination is also observed by polling.

## 3. External write visibility

If rows are changed directly in Postgres (insert, update, delete):

- Planner and engines must observe the change within `CHANNEL_SYNC_INTERVAL`.
- A direct `UPDATE` that sets an inactive combination stops work; the conditional `LIVE` write cannot undo it.
- A direct delete removes the channel from the roster; work is cancelled and output purged.

## 4. Environment contract

| Variable | Default | Used by | Meaning |
|---|---|---|---|
| `CHANNEL_SYNC_INTERVAL` | `2s` | planner, engines | Roster poll and maximum detection latency; invalid/≤0 falls back to default |
| `PLANNER_INTERVAL` | `5m` | planner | Per-channel re-plan cadence while active (unchanged meaning) |
| `PLANNER_WORKERS` | `4` | planner | Cap on concurrent preparation work |
| `MAX_WORKERS` | `4` | engines | Download worker pool size (`.env` currently sets 20) |

Success targets (not enforced timeouts): p95 ≤10s, max ≤30s for stop/start effect, measured from the API call or direct DB change to the observable effect.

## 5. Log contract

Every lifecycle decision emits a structured log line containing:

| Key | Value |
|---|---|
| `event` | `lifecycle_start`, `lifecycle_stop`, `lifecycle_restart`, `lifecycle_cancel` |
| `channel` | channel id |
| `previous_state` | prior `status`/active combination |
| `new_state` | resulting `status`/active combination |
| `reason` | `operator_start`, `operator_stop`, `operator_delete`, `activated`, `deactivated`, `definition_changed`, `failure_threshold`, `unsupported_schema` |
| `latency_ms` | time since the reconciler observed the change (where measurable) |
| `cause` | cancellation cause for supervisor exit |

Per-channel loggers remain `PLANNER:{id}` and `ENGINE:{id}`; API lifecycle logs keep the `[api] channel=...` prefix.

## 6. Cleanup contract

After a channel stops, within the target latency:

- No planner supervisor, engine playout loop, or download/prepare work remains for the channel.
- The HLS master, all rendition media, rendition metadata, timeline, cursor, and fallback keys are absent.
- The channel disk directory is removed.
- The failure counter is removed by the planner's cleanup; the API preserves it on stop/delete by design so an operator restart starts clean only through the Start path.
- Starting the channel again rebuilds state; no output from the previous session is served.
