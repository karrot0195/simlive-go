# Data Model: Monitor

## MonitoredChannel

- channel_id: string (PK)
- created_at: timestamp

## SSE Event Payload

- channels: array of ChannelTimeline
- generated_at: number