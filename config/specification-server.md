# Server Specification

Canonical spec for a SimLive server. Used by the `server-deploy` skill to prepare and verify a target host.

## Prerequisites (TOOLS)

- docker (postgres + redis via `config/docker-compose.yml`)
- nginx (reverse proxy to API on :8080, SSL termination)
- Go toolchain (>= 1.26, see `config/scripts/install-go.sh`)
- tsduck (`tsp`, `tstables`), ffmpeg/ffprobe, rclone

## Services (systemd)

- `simlive-api.service` — HTTP API + HLS segment serving
- `simlive-planner.service` — session planner
- `simlive-engines.service` — segment production with SCTE-35

All units under `config/system/`, installed to `/etc/systemd/system/`.

## Configuration

- `config/docker-compose.yml` → postgres (5432) + redis (6371)
- `config/.env` → DB, Redis, ESPX, TMP_DIR, renditions, workers
- `API_ADDR` → API bind address (default `:8080`; e.g. `API_ADDR=:3030` to expose the
  `/common` XML endpoints on the partner example port without a code change)
- `config/partner.yaml` → single partner configuration (active profile, partner API
  connection, auth flow, XML customisation); `config/partner.example.yaml` documents the schema
- nginx config: proxy `/` and `/live` to `127.0.0.1:8080`, SSL cert configured
- `config/rclone` → `/root/.config/rclone`
- `EPG_CACHE_DIR` (default `{TMP_DIR}/epg-cache`) → retained EPG schedule per channel, used when the provider returns empty. Preserved across `ENABLE_CLEAN_UP=true` restarts; delete the directory to reset the fallback.

## EPG cache fallback

The planner writes the last non-empty provider schedule for each channel to
`{EPG_CACHE_DIR}/{channel_id}.json` (atomic temp-file + rename). When a provider
fetch returns an empty payload or errors, the planner repeats that retained
schedule, time-shifted to cover the host-local target day, instead of going dark.
A successful fetch replaces the file; empty/error responses never touch it.
Fallback usage is recorded in Redis (`planner:fallback:{channel_id}`) and shown as
a `FALLBACK: <source_date>` chip on the admin timeline.

- File schema: `version` (1), `channel_id`, `channel_epg_id`, `source_date`, `fetched_at`, `timezone`, `items`.
- A file whose version/channel/EPG id does not match, or that is unreadable/corrupt, is ignored (never fatal).
- No age-based expiry: retained schedules are reused indefinitely.

## Layout on server

```
/root/simlive-go/
  bin/            api, planner, engines
  client/dist/    Vue admin UI (served by nginx or api)
  .env
  outputs/        TMP_DIR for segments
  outputs/epg-cache/  EPG_CACHE_DIR (retained schedules; survives ENABLE_CLEAN_UP)
  logs/           (journald via stdout; no file logs)
```

## Verification checklist

- [ ] docker compose up -d; postgres + redis healthy
- [ ] nginx running, SSL valid, proxy to :8080
- [ ] systemctl enable+start simlive-{api,planner,engines}
- [ ] all three services active
- [ ] `curl https://<host>/live/<channel>/playlist.m3u8` returns 200
- [ ] Go version matches go.mod
- [ ] rclone configured and storage sync works

## Source requirements

See `config/server-requiremnt.md` for the original tool/config checklist.