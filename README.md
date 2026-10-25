# simlive

Simulated live streaming platform: turns VOD assets into HLS live streams with SCTE-35 ad insertion, driven by a planner/engine pipeline and a Vue admin client.

## Structure

```
cmd/        Go binaries: api, planner, engines, plus dev tools (pmt-verify, scte35-check, hls-watch, testredis)
internal/   Core packages: models, storage, api, planner, engine, utils, configs, verify
client/     Vue 3 + Vite + Vuetify admin UI
config/     Docker compose, systemd units, build/deploy scripts
docs/       Project documentation
requests/   Partner/feature request specs
specs/      Feature specifications
```

## Services

- `api` — HTTP API (gin) for channels, assets, playlists; serves HLS segments.
- `planner` — schedules and plans simulated-live sessions.
- `engines` — runs engine instances that produce HLS segments with SCTE-35 markers.

## Binaries / tools

- `pmt-verify` — watches downloaded segments and verifies PMT/SCTE-35 consistency.
- `scte35-check` — HTTP-based SCTE-35/PMT verifier for a live playlist.
- `hls-watch` — monitors an HLS playlist, analyzes segments (PTS, SCTE-35, PMT).

## Requirements

- Go 1.26+
- Node.js + pnpm (client)
- PostgreSQL, Redis
- tsduck (`tsp`, `tstables`), ffmpeg/ffprobe
- rclone (storage sync)

## Build & run

```bash
make build        # build api, planner, engines -> config/bin/
make build-client # build Vue client
make start-all    # build + start all services
make stop         # stop all services
make status       # show running services
make test         # go test ./...
make fmt / make vet
```

## Configuration

Environment variables via `.env` (see `internal/utils/envs.go`, `internal/configs`). Docker compose and systemd units under `config/`.

## Dependencies

See `go.mod` (gin, gorm/postgres, redis, scte35-go, godotenv, lumberjack, term). Client deps in `client/package.json`.