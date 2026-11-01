# Data Model: 24/7 Playout EPG Cache Fallback

## RetainedSchedule (on-disk JSON, one file per channel)

Location: `{EPG_CACHE_DIR}/<channel_id>.json`
Default `EPG_CACHE_DIR`: `{TMP_DIR}/epg-cache`

**Canonical day boundary**: all target-day boundaries are the host-local day (`utils.StartOfDay`/`EndOfDay` in `utils.LocalLocation()`); `configs.TZ` is used only if the host location cannot be resolved. The `timezone` field below records the channel content timezone used to format item timestamps, not a separate day boundary.

| Field | Type | Notes |
|-------|------|-------|
| `version` | integer | Schema version, currently `1`. Unknown versions are ignored. |
| `channel_id` | string | Must equal the requesting channel; mismatch ⇒ ignore. |
| `channel_epg_id` | string | Provider schedule identity at fetch time; mismatch ⇒ ignore. |
| `source_date` | string | Requested provider day, `YYYY-MM-DD`. |
| `fetched_at` | integer | Unix seconds when the provider response was received. |
| `timezone` | string | Channel content timezone used to format item timestamps (informational; not a day-boundary source). |
| `items` | array | Non-empty `ESPXScheduleItem` list exactly as returned by the provider. |

**Validation rules**

- `version` must be `1`.
- `channel_id` must equal the channel being planned.
- `channel_epg_id` must equal the channel's current `ChannelEpgID`.
- `items` must be non-empty.
- Unreadable or malformed JSON ⇒ treat as "no retained schedule"; never fail the planner.
- No age-based expiry (FR-016).

**Write rules**

- Written only after a successful, non-empty provider response (FR-003).
- Never written from fallback-generated items (FR-009).
- Never written or deleted by an empty/error response (FR-004).
- Atomic: write to a temp file in the cache directory, then rename over the target.

## FallbackState (Redis hash `planner:fallback:{channel_id}`)

One field per target day; field name is the target day `YYYY-MM-DD`; value is JSON:

| Field | Type | Notes |
|-------|------|-------|
| `target_date` | string | Day the fallback covers, `YYYY-MM-DD`. |
| `source_date` | string | Day of the retained schedule used. |
| `source_epg_id` | string | Provider identity of the retained schedule. |
| `day_start` | integer | Unix seconds, host-local start of the target day. |
| `day_end` | integer | Unix seconds, host-local end of the target day (also the planner horizon). |
| `created_at` | integer | Unix seconds when the fallback was planned. |

**Rules**

- Written only after fallback planning produced coverage; removed if planning fails.
- TTL refreshed to `RedisExpiryHours` (default 26h) on every write.
- Field deleted only after a successful real-data purge and replan for that day.
- Multiple fields may coexist during a multi-day outage.
- Missing/unparseable state ⇒ treated as "not a fallback day".

## Timeline API projection

`GET /api/channels/timeline/running` and `GET /api/channels/timeline/stream` `channels[]` entries gain:

| Field | Type | Notes |
|-------|------|-------|
| `fallback_active` | boolean | True when now is within a fallback day's `[day_start, day_end]`. Omitted when false. |
| `fallback_source_date` | string | Source day of the retained schedule used. Omitted when not active. |

The planner's `simlive:timeline:{channel_id}` ZSET and `SegmentItem` schema are unchanged.

## State transitions

```text
provider fetch ──non-empty──▶ retain schedule ──▶ plan real coverage ──▶ normal
provider fetch ──empty/error─▶ retained exists? ──yes──▶ mark fallback day ──▶ plan shifted coverage
                                        └──no───▶ ErrNoScheduleData ──▶ failure counter / channel status

fallback day + provider recovers
  └─▶ save retained schedule
      └─▶ remove timeline scores ≥ max(now − WindowBufferSeconds, day_start)
          └─▶ clear fallback field for the day
              └─▶ plan real coverage from surviving tail
```
