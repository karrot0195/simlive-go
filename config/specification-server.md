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
- nginx config: proxy `/` and `/live` to `127.0.0.1:8080`, SSL cert configured
- `config/rclone` → `/root/.config/rclone`

## Layout on server

```
/root/simlive-go/
  bin/            api, planner, engines
  client/dist/    Vue admin UI (served by nginx or api)
  .env
  outputs/        TMP_DIR for segments
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