# Quickstart: Timeline UI Optimization

## Prerequisites

- Node.js + pnpm; client deps installed.
- Running API with at least one enabled channel whose timeline spans midnight.

## Run

```bash
cd client && pnpm dev
```

Open the Running Channels Timeline view.

## Validate

1. Multi-day summary: channel with entries from ~16:26 to ~00:00 shows "Data fills from MM-dd HH:MM:SS to MM-dd HH:MM:SS".
2. Single-day summary stays time-only.
3. Per-entry date appears only when the calendar day changes from the previous entry.
4. Large timeline (500 entries): first paint renders 50 entries; "Load more" appends next 50; control hides when exhausted.
5. SSE refresh resets paging to first 50.
6. Gap >10s straddling a page boundary is still flagged after "Load more".