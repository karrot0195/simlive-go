# Contract: Fallback State (Redis)

**Key**: `planner:fallback:{channel_id}`
**Type**: hash
**Field**: target day, `YYYY-MM-DD`
**Writer**: planner
**Reader**: planner (recovery check), API (timeline projection)
**TTL**: `RedisExpiryHours` (default 26h), refreshed on every write
**Day boundary**: `day_start`/`day_end` are the host-local target-day boundaries (same instants as the planner horizon).

## Value schema

```json
{
  "target_date": "2026-10-30",
  "source_date": "2026-10-29",
  "source_epg_id": "b6a1f0c2-2222-4c3d-8e4f-000000000002",
  "day_start": 1761843600,
  "day_end": 1761929999,
  "created_at": 1761811200
}
```

## Guarantees

- Written only after fallback planning produced coverage for the target day; removed if that planning fails.
- Never written from provider-sourced planning.
- A field is deleted only after the purge and real-data replan for that day both succeed.
- Missing, malformed, or identity-mismatched fields are treated as "not a fallback day".

## Consumer rules

- Planner: `HGET key <target_date>` to decide whether a day was fallback-filled before probing the provider for recovery.
- API: `HGETALL key`; select the field whose `[day_start, day_end]` contains `now`; set `fallback_active` and `fallback_source_date`. Fields for other days are ignored.

## Example keys

```text
planner:fallback:0f7c2f1e-1111-4a2b-9c3d-000000000001
```
