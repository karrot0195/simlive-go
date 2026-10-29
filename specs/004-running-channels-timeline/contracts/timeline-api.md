# Contract: Running Channels Timeline API

## GET /api/channels/timeline/running

Returns upcoming timeline entries for all enabled channels.

### Request
- Method: GET
- Query params: none
- Auth: none (matches existing /api surface)

### Response 200
```json
{
  "channels": [
    {
      "channel_id": "ch-1",
      "channel_name": "Channel One",
      "entries": [
        {
          "sequence": 42,
          "running_time": 1730000000.0,
          "duration": 6.0,
          "time_text": "12:34",
          "is_ad": false,
          "ad_type": "",
          "ad_break_id": 0,
          "ad_break_start": 0.0,
          "ad_break_duration": 0.0
        }
      ]
    }
  ],
  "generated_at": 1730000000.0
}
```

### Response 500
```json
{ "error": "..." }
```
Returned when DB or any channel's Redis read fails.

### Invariants
- channels ordered by created_at DESC.
- entries ordered by running_time ASC.
- no entry with running_time < generated_at.
- disabled channels absent.
- empty entries serialized as [].