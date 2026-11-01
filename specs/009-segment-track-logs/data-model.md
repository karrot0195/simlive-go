# Data Model: Segment Track Log

## SegmentTrackEntry (JSONL line)

- channel_id: string
- sequence: number
- status: string (prepared | failed | downloaded | ...)
- timestamp: number (unix)
- message: string (optional)