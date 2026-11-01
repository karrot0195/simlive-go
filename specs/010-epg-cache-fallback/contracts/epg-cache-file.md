# Contract: Retained Schedule File

**Path**: `{EPG_CACHE_DIR}/<channel_id>.json`
**Writer**: planner only
**Reader**: planner only

## Schema

```json
{
  "version": 1,
  "channel_id": "0f7c2f1e-1111-4a2b-9c3d-000000000001",
  "channel_epg_id": "b6a1f0c2-2222-4c3d-8e4f-000000000002",
  "source_date": "2026-10-29",
  "fetched_at": 1761790800,
  "timezone": "Asia/Jakarta",
  "items": [
    {
      "id": "sched-1",
      "type": "programme",
      "duration": "3600",
      "local_date_start": "2026-10-29 00:00:00",
      "local_date_end": "2026-10-29 01:00:00",
      "programme": {
        "id": "prog-1",
        "name": "Programme 1",
        "short_url": "prog-1",
        "programmeStreams": [
          {
            "stream_id": "stream-1",
            "stream_path": "path/to/stream",
            "schema_ver": 3,
            "is_vod": true,
            "is_trailer": false,
            "name": "Default Stream"
          }
        ],
        "is_ads": false
      }
    }
  ]
}
```

## Guarantees

- The file is only written from a successful, non-empty provider response.
- Writes are atomic (temp file + rename in the same directory).
- A read failure or schema/identity mismatch is treated as "no retained schedule".
- There is no age-based expiry.
- `timezone` records how item timestamps were formatted; it is not a day-boundary source. Target-day coverage is always computed on the host-local day.

## Compatibility

- Bumping `version` requires a reader fallback that ignores unknown versions.
- `items` must remain round-trippable through `models.ESPXScheduleItem`.
