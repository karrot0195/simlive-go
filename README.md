# simlive-go

Simulated-live (24/7 linear) HLS playout system. Module: `simlive` (Go 1.26.6).

## Stack
- **Backend**: Go 1.26.6, gin-gonic/gin v1.12.0
- **DB**: PostgreSQL (gorm.io/driver/postgres v1.6.2 + gorm v1.31.2)
- **Cache**: Redis (redis/go-redis/v9 v9.22.0)
- **Media**: ffmpeg (PTS/DTS rewrite), rclone CLI, SCTE-35 (`github.com/Comcast/scte35-go` v1.8.0)
- **Client**: Vue 3.4.29 + Vite 5 + Vuetify 3.7.2 + TypeScript (`./client/`)
- **Config**: `joho/godotenv` v1.5.1, `internal/configs/`

## Directory Layout
```
cmd/              # Entry points: api, planner, engines, scte35-verifier, verifier
internal/         # api, planner, engine, models, storage, configs, utils
client/           # Vue 3 frontend (swagger.yaml, src/)
docs/             # Workflow docs (e.g., ingest-ads-simlive-phase1.md)
config/           # .env, rclone conf, docker-compose.yml
wiki/             # arch/, tasks/, workstreams/
```

## Build
```bash
make build          # builds cmd/api, cmd/planner, cmd/engines -> config/bin/
make build-client   # pnpm build in ./client
make start-all      # runs api + planner + engines
```

## Entry Points
- `cmd/api/main.go`
- `cmd/planner/main.go`
- `cmd/engines/main.go`
