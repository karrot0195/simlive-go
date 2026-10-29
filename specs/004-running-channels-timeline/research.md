# Research: Running Channels Timeline

## Decision: Endpoint path
- **Decision**: `GET /api/channels/timeline/running` (grouped under /api/channels).
- **Rationale**: Consistent with existing /api/channels surface and avoids shadowing /channels/:id.
- **Alternatives**: /api/timeline/running — rejected to keep channel-related routes together.

## Decision: Time filtering uses Redis ZRangeByScore
- **Decision**: Use `ZRangeByScore(key, now, +inf)` to fetch entries with running_time >= now in one round trip per channel, then JSON-unmarshal each member.
- **Rationale**: Sorted set is already scored by running_time (float Unix). Server `time.Now().Unix()` (float64) is the lower bound.
- **Alternatives**: ZRange + client filter — rejected (transfers past entries).

## Decision: Channel ordering
- **Decision**: Query `playout_channels WHERE is_active = true ORDER BY created_at DESC`.
- **Rationale**: Matches ListChannels default and clarification.

## Decision: Trimmed projection
- **Decision**: Define `TimelineEntry` struct with fields: sequence, running_time (float64), duration, time_text, is_ad, ad_type, ad_break_id, ad_break_start, ad_break_duration. Exclude PTS/media fields.
- **Rationale**: UI needs display fields only; reduces payload.

## Decision: Gap detection
- **Decision**: Compute gaps in the FE from consecutive entries: gap = next.running_time - (curr.running_time + curr.duration). Warn if > 10s. Backend may optionally include a `gap_warning` flag, but FE computes to keep backend simple.
- **Rationale**: YAGNI; FE has the data.

## Decision: Error handling
- **Decision**: If any channel's Redis read fails, abort and return 500 with error message. DB failure also 500.
- **Rationale**: Clarification choice.