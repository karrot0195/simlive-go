---
type: hub
hub: true
schema_version: 1
project: simlive
---

# API

The REST API layer of SimLive. Built with Gin, it exposes channel management endpoints, HLS live streaming routes, and an PARTNER integration client for upstream channel schedules.

## What belongs here

- Channel CRUD and lifecycle (start/stop) endpoints
- HLS master playlist and per-rung media playlist routes
- Segment serving routes
- PARTNER client for upstream channel/schedule data

## Related hubs

- [[planner]] — the planner drives channel state that the API exposes
- [[engine]] — the engine serves segments that the API routes deliver
- [[models]] — shared data models used by API handlers