---
project_name: simlive-go
language: Go
domain: simulated-live HLS playout (24/7 linear streaming)
tech_stack:
  - backend: Go 1.26.6 (module `simlive`)
  - framework: gin-gonic/gin v1.12.0 (REST API, HLS routes)
  - database: PostgreSQL (gorm.io/driver/postgres v1.6.2 + gorm v1.31.2)
  - cache: Redis (redis/go-redis/v9 v9.22.0) — per-rung ZSET timeline, master playlist, rendition metadata, breaker counter
  - media: ffmpeg (PTS/DTS rewrite, `-c copy -copyts -output_ts_offset`), `m3u8-parser` (client), `video.js` + `videojs-contrib-hls` (player)
  - scte35: github.com/Comcast/scte35-go v1.8.0 (`SpliceInfoSection`, `buildSpliceInsert`, `buildCueOutLine`, `buildCueInLine`)
  - storage_wrapper: rclone (`rclone cat` manifest parse, `rclone copyto` segment download, `rclone ls` directory listing)
  - client: Vue 3.4.29 + Vite 5 + Vuetify 3.7.2 + TypeScript (`client/src/`)
  - build: `makefile` (Go build), `pnpm` (client build), `docker-compose.yml` (service orchestration)
  - env_config: `joho/godotenv` v1.5.1 (`.env`), `internal/configs/configs.go`
rule:
  title: ARCH-RULE-01 — Canonical spec is single source of truth; architecture docs must reference it
  description: >
    Every module (planner, engine, api, storage, config, client) must treat `docs/specification.md` as the canonical spec. `.github/instructions/simlive-summary.instructions.md` is the concise architecture snapshot (always in sync with code). `io/output.md` (this file) is the machine-readable contract; `wiki/arch/02-arch.md` is the human-readable architecture doc. Both must reference `docs/specification.md` and stay consistent with `.github/instructions/simlive-summary.instructions.md`. Any drift between code, spec, instructions, and architecture docs is a quality failure.
  applies_to:
    - planner (`internal/planner/planner.go`, `segment_processor.go`)
    - engine (`internal/engine/engine.go`, `rendition.go`, `scte35.go`)
    - api (`internal/api/handler.go`, `partner_client.go`)
    - storage (`internal/storage/rclone.go`)
    - config (`internal/configs/configs.go`, `.env`)
    - client (`client/src/` — Vue 3 + Vite + Vuetify)
  enforcement:
    - `io/output.md` regenerated on every `sdlc-arch` run.
    - `wiki/arch/02-arch.md` regenerated on every `sdlc-arch` run.
    - `docs/specification.md` is read (not overwritten) by this skill.
    - `.github/instructions/simlive-summary.instructions.md` is read (not overwritten) by this skill.
feature_doc_path: wiki/arch/02-arch.md
summary_doc_path: wiki/arch/01-summary.md
generated_at: "2026-09-15T00:00:00Z"
source_refs:
  - go.mod
  - docs/specification.md
  - .github/instructions/simlive-summary.instructions.md
  - docs/structure.md
  - docs/300826_0620-multi-rendition-abr-ladder.md
  - docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md
  - client/package.json
  - docker-compose.yml
  - internal/planner/planner.go
  - internal/engine/engine.go
  - internal/api/handler.go
  - internal/storage/rclone.go
  - internal/configs/configs.go
standard_format_version: "1.0"
