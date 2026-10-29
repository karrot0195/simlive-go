# Quickstart: Running Channels Timeline

## Prerequisites
- Postgres + Redis running; at least one channel with `is_active = true` and populated `simlive:timeline:{id}`.

## Run API
```bash
make build && ./config/bin/api
```

## Validate endpoint
```bash
curl -s http://localhost:8080/api/channels/timeline/running | jq .
```
Expected: channels array ordered by created_at DESC, entries running_time >= now, no disabled channels.

## Validate empty state
- Stop all channels; response `channels: []`.

## Validate UI
- Open admin UI, navigate to Running Channels Timeline view; verify graph renders and gaps >10s show warning.