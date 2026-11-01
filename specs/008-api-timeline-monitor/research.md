# Research: API-Level Timeline Monitor (SSE)

## Decisions

- Monitor state persisted in `monitored_channels` table (channel_id, created_at).
- Monitor interval from `MONITOR_INTERVAL` env, default 3s.
- API exposes watch/unwatch endpoints and an SSE stream endpoint.
- Monitor loop reads Redis timeline keys (read-only) for monitored channels and broadcasts to SSE clients.
- Frontend uses EventSource; no polling.