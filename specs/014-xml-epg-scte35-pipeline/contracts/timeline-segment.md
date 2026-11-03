# Contract: Timeline Segment JSON

Each planner-produced segment is stored in the Redis sorted set `simlive:timeline:{channelId}` scored by `running_time`. The engine reads these members and turns them into HLS segments and playlists.

## Member shape

```json
{
  "sequence": 142,
  "storage_path": "espx-video-lib:espx-video-lib/ads-v3/break-1/{rendition}/seg_00002.ts",
  "duration": 4,
  "start_time": 1793691000,
  "time_text": "10:30",
  "running_time": 1793691000,
  "pts_start": 126000,
  "pts_end": 486000,
  "media_start": 1793691000,
  "media_end": 1793691004,
  "pts_offset": 5400,
  "rendition": ["L0", "L1", "L2"],
  "is_ad": true,
  "ad_type": "out",
  "ad_break_id": 7,
  "ad_break_start": 1793691000,
  "ad_break_duration": 30,
  "splice_inserts": [
    {
      "type": "out",
      "eventId": 7,
      "spliceTimePts": 161432190000000,
      "spliceTimeSeconds": 1793691000,
      "duration": 30,
      "uniqueProgramId": 7,
      "segmentationTypeId": 52
    }
  ]
}
```

## Field guarantees

| Field | Guarantee |
|-------|-----------|
| `sequence` | Strictly increasing across a plan and across days. |
| `running_time` | ZSET score; non-decreasing. |
| `media_start` / `media_end` | Continuous across content-to-ad and ad-to-content boundaries. |
| `pts_offset` | Continues from the previous segment; used for output PTS rewriting. |
| `is_ad` | True only for ad asset segments. |
| `ad_type` | `out` on the first ad segment; `in` on the first content segment after a break; empty otherwise. |
| `ad_break_id` | Present on all ad segments and on the cue-in content segment of the same break. |
| `ad_break_start` / `ad_break_duration` | Present wherever `ad_break_id` is present; describe the played break. |
| `splice_inserts` | At most one cue-out and one cue-in per break; up to two placement-start markers on content; at most one placement-end marker on ad segments. |
| Omitted fields | `is_ad`, `ad_type`, `ad_break_*`, and `splice_inserts` are omitted when empty so content members stay compact. |

## Invariants

1. All segments of one break share one `ad_break_id`.
2. `splice_inserts[].eventId` equals `ad_break_id` for every marker of the break.
3. The cue-in member is a content segment: `is_ad` is false, `ad_type` is `in`, and `splice_inserts` contains the `0x35` event.
4. No member's `media_start` is earlier than the previous member's `media_end` minus floating-point tolerance.
5. Placement-start markers appear only on content segments; placement-end markers appear only on ad segments.
