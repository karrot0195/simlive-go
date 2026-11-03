# Data Model: Timeline UI Optimization

No backend or persisted data changes. State is client-side only.

## Client-side state

- **LoadedPageState** (per channel, in-memory):
  - `channel_id`: string
  - `loadedCount`: number (default `TIMELINE_PAGE_SIZE` = 50)
  - Reset to default on SSE/manual refresh.

- **GapFlagSet** (per channel, derived):
  - Set of entry sequence numbers whose predecessor gap > 10s.
  - Recomputed when `ch.entries` changes.

## Existing entities (unchanged)

- **Timeline Entry**: sequence, running_time, duration, time_text, is_ad, ad-break info.
- **ChannelTimeline**: channel_id, channel_name, entries[], fallback_active, fallback_source_date.