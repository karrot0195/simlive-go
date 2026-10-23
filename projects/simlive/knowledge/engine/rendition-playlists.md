---
type: project-knowledge
schema_version: 1
project: simlive
---

# Rendition Playlists

The engine generates per-rung HLS media playlists (360p, 720p, 1080p) and a master playlist with one `#EXT-X-STREAM-INF` per active rung.

Parent: [[engine]]

## Rendition ladder

- `360p` — default/fallback rendition
- `720p` — standard HD
- `1080p` — full HD

## Behavior

- The engine writes one media playlist per active rung in `{TMP_DIR}/{ch}/{rend}/playlist.m3u8`.
- Each rung's playlist references its own `seg_N.ts` files in the same directory.
- The master playlist is written to Redis (`simlive:hls:{ch}`) on every tick.
- Legacy segments without a `Renditions` tag are rescued to `DefaultRendition` ("360p").

## Related

- [[pts-offset]] — PTS rewriting applies per rendition
- [[hls-streaming]] — the API serves these playlists and segments