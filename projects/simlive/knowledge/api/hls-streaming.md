---
type: project-knowledge
schema_version: 1
project: simlive
---

# HLS Live Streaming

The API server exposes HLS streaming routes under `/live/`. The master playlist is served from Redis (with an on-disk fallback), and per-rung media playlists serve segments from `{TMP_DIR}/{ch}/{rend}/`.

Parent: [[api]]

## Routes

- `GET /live/:channel_id/playlist.m3u8` — master playlist (one `#EXT-X-STREAM-INF` per active rung)
- `GET /live/:channel_id/:rendition/playlist.m3u8` — per-rung media playlist (360p, 720p, 1080p)
- `GET /live/:channel_id/:rendition/:segment` — individual `.ts` segment files
- `GET /legacy/:segment` — legacy single-segment route

## Design notes

- Route ordering in Gin matters: the media playlist route must be registered before the segment route to avoid radix tree shadowing.
- The master playlist is written by the engine to Redis on every tick, so a player reload never gets a 404.
- CORS is enabled for any origin (reflect Origin header) since this is a local simulated-live server.

## Related

- [[engine]] — the engine writes the playlists and segments the API serves
- [[channel-management]] — channel lifecycle endpoints that gate playout