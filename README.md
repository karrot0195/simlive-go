# simlive

Simulated live streaming platform: turns VOD assets into HLS live streams with SCTE-35 ad insertion, driven by a planner/engine pipeline and a Vue admin client.

## Quick Start (5 minutes)

### 1. Prerequisites

- Go 1.26+
- Node.js + pnpm (for the client)
- Docker (for Postgres + Redis)
- `tsduck` (`tsp`, `tstables`), `ffmpeg`/`ffprobe`, `rclone`

### 2. Configure environment

```bash
cp .env.example .env
# edit .env as needed (DB, Redis, partner API, etc.)
```

### 3. Start dependencies

```bash
cd config && docker compose up -d
```

This starts Postgres (port 5432) and Redis (port 6371).

### 4. Build & run

```bash
make build        # builds api, planner, engines -> config/bin/
make start-all    # builds (if needed) and starts all services
```

Services run in the background; logs go to `logs/*.log`.

### 5. Verify

```bash
make status       # check running services
# API available at http://localhost:8080 (default)
```

Stop everything with `make stop`.

## Architecture

simlive is split into three cooperating services plus a web client.

```
┌────────────┐     ┌────────────┐     ┌────────────┐
│  planner   │────▶│  engines   │────▶│    api     │
│ (schedules)│     │ (segments) │     │ (HLS/HTTP) │
└────────────┘     └────────────┘     └────────────┘
       │                  │                  │
       └──────────────────┴──────────────────┘
                          │
                  ┌───────┴───────┐
                  │ Postgres/Redis│
                  └───────────────┘
```

- **planner** — schedules simulated-live sessions and produces a plan of what to stream and when.
- **engines** — execute the plan, downloading/transmuxing VOD into HLS segments with SCTE-35 markers injected.
- **api** — Gin HTTP API for channels, assets, playlists; serves HLS segments to players.
- **client** — Vue 3 + Vite + Vuetify admin UI for managing channels/assets.

Data flow: planner decides the timeline → engines materialize segments (with ad breaks) → api exposes them as a live HLS playlist. Postgres holds channels/assets/playlists; Redis caches hot state.

## Partner Integration

See [PARTNER_GUIDE.md](PARTNER_GUIDE.md) for partner XML configuration, auth setup, and using the demo partner server.

## Configuration

Environment variables via `.env` (see `.env.example`). Key groups:

| Group | Variables |
|---|---|
| Database | `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` |
| Redis | `REDIS_ADDR` |
| Partner/Source | `PARTNER_CONFIG`, `SIMLIVE_QUALITY` |
| Engine | `TMP_DIR`, `ENABLE_CLEAN_UP`, `MAX_WORKERS`, `SIMLIVE_KEEP_BEHIND`, `SIMLIVE_DOWNLOAD_AHEAD`, `ACTIVE_RENDITIONS`, `PLANNER_INTERVAL` |

Docker compose and systemd units live under `config/`.

## Build & Run

```bash
make build        # build api, planner, engines -> config/bin/
make build-client # build Vue client
make start-all    # build + start all services
make stop         # stop all services
make status       # show running services
make logs         # tail service logs
make test         # go test ./...
make fmt          # go fmt ./...
make vet          # go vet ./...
make tidy         # go mod tidy
```

## Project Structure

```
cmd/        Go binaries: api, planner, engines, plus dev tools (pmt-verify, scte35-check, hls-watch, testredis)
internal/   Core packages: models, storage, api, planner, engine, utils, configs, verify
client/     Vue 3 + Vite + Vuetify admin UI
config/     Docker compose, systemd units, build/deploy scripts
docs/       Project documentation
requests/   Partner/feature request specs
specs/      Feature specifications
```

## Verification Tools

- `pmt-verify` — watches downloaded segments and verifies PMT/SCTE-35 consistency.
- `scte35-check` — HTTP-based SCTE-35/PMT verifier for a live playlist.
- `hls-watch` — monitors an HLS playlist, analyzes segments (PTS, SCTE-35, PMT).
- `testredis` — Redis test/debug utility.

## Requirements

- Go 1.26+
- Node.js + pnpm (client)
- PostgreSQL, Redis
- tsduck (`tsp`, `tstables`), ffmpeg/ffprobe
- rclone (storage sync)

## Development

```bash
make test   # run all Go tests
make fmt    # format
make vet    # vet
```

TDD is enforced: tests written → approved → fail → implement (Red-Green-Refactor).

## Dependencies

See `go.mod` (gin, gorm/postgres, redis, scte35-go, godotenv, lumberjack, term). Client deps in `client/package.json`.