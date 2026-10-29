# Data Model: Running Channels Timeline

## Entities

### TimelineEntry (API projection)
Display-relevant fields derived from `models.SegmentItem`:
- `sequence` int64
- `running_time` float64 (Unix seconds)
- `duration` float64 (seconds)
- `time_text` string (HH:mm)
- `is_ad` bool
- `ad_type` string (optional)
- `ad_break_id` uint32 (optional)
- `ad_break_start` float64 (optional)
- `ad_break_duration` float64 (optional)

### ChannelTimeline (API group)
- `channel_id` string
- `channel_name` string
- `entries` []TimelineEntry (empty array, not null, when none)

### TimelineResponse (API body)
- `channels` []ChannelTimeline (ordered by created_at DESC)
- `generated_at` float64 (Unix seconds, request reference time)

## Source entities (existing, unchanged)
- `models.PlayoutChannel`: id, name, is_active, created_at
- `models.SegmentItem`: stored in Redis sorted set; score = running_time

## Validation rules
- Only channels with `is_active = true` included.
- Entries with `running_time < now` excluded.
- Entries within a channel sorted by running_time ascending.
- Empty entries serialized as `[]`, not null.