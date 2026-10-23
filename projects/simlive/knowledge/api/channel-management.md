---
type: project-knowledge
schema_version: 1
project: simlive
---

# Channel Management

The API exposes full CRUD for playout channels plus start/stop lifecycle endpoints. Channels are stored in Postgres via GORM and synced with ESPX upstream.

Parent: [[api]]

## Endpoints

- `POST /api/channels` — create/upsert (validates against ESPX)
- `GET /api/channels` — list local playout channels (paginated, default 20, max 100)
- `GET /api/channels/esp` — list ESPX upstream channels + registration map
- `GET /api/channels/:id` — get channel
- `PUT /api/channels/:id` — full update
- `PATCH /api/channels/:id` — partial update (pointer fields, `clause.AssignmentColumns`)
- `DELETE /api/channels/:id` — delete channel
- `POST /api/channels/:id/start` — start playout
- `POST /api/channels/:id/stop` — stop playout

## Related

- [[hls-streaming]] — HLS routes that serve active channels
- [[planner]] — the planner polls active channels and processes their timelines