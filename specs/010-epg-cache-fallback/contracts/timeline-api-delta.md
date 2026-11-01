# Contract: Timeline API Delta

**Endpoints affected**:
- `GET /api/channels/timeline/running`
- `GET /api/channels/timeline/stream` (SSE `data:` payload)

## Additive fields on `channels[]`

```json
{
  "channel_id": "0f7c2f1e-1111-4a2b-9c3d-000000000001",
  "channel_name": "News 24",
  "fallback_active": true,
  "fallback_source_date": "2026-10-29",
  "entries": []
}
```

| Field | Type | Presence | Meaning |
|-------|------|----------|---------|
| `fallback_active` | boolean | omitted when false | Set only when the channel has a committed fallback marker for the current host-local day (now within `[day_start, day_end]`). |
| `fallback_source_date` | string | omitted when not active | Day of the retained schedule used to produce that coverage. |

## Compatibility

- Additive only; existing consumers ignore unknown fields.
- `entries[]` and `generated_at` are unchanged.
- The UI renders a label/chip when `fallback_active` is true; the source date is shown alongside it.
- Because the flag is marker-driven, a failed fallback attempt never produces a label.
- No new endpoint is introduced.
