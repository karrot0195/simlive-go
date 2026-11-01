# Research: Persistent Segment Track Logs

## Decisions

- Store segment track entries in append-only JSONL files per channel under `TMPDir/logs/<channel>.jsonl`.
- Cap at 1000 entries per channel by trimming oldest lines on append.
- Expose `GET /api/channels/:id/segment-track?limit=N` to fetch last N entries.
- FE adds a button per channel to open a dialog listing entries.