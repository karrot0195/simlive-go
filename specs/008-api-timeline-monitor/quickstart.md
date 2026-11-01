# Quickstart: API-Level Timeline Monitor

## Prerequisites

- API running with Postgres + Redis.
- `MONITOR_INTERVAL=3s` (optional).

## Validation

1. Watch a channel from UI; verify row in `monitored_channels`.
2. Open timeline screen; verify SSE connection and updates every interval.
3. Unwatch; verify no further updates for that channel.
4. Confirm engine/planner logs unchanged.