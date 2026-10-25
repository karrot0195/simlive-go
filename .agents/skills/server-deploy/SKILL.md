---
name: "server-deploy"
description: "Set up, deploy, or update a SimLive server from ./config: collect and persist SSH target, sync config/binaries/client, run systemd services, and record the deployment in ./config/DEPLOY.md."
metadata:
  author: "simlive"
  source: "config/"
---

# Server Deploy Skill

End-to-end flow for provisioning or updating a SimLive server using the assets under `./config`.

## When to Use

- User asks to set up, deploy, or update a server.
- User provides a server IP and SSH credentials.
- User wants to re-sync code/config to an existing server.

## Inputs

- Common tools allowed during execution: `ssh`, `scp`, `read_file`, and other standard CLI/file tools for inspection and sync.

- `server_ip`: target server IP or hostname.
- `ssh_key`: path to SSH private key (default `~/.ssh/espx_vultr`).
- `ssh_user`: SSH user (default `root`).
- Persisted target file: `./config/deploy-targets.json` (created by this skill).

## Persistence

Store targets in `./config/deploy-targets.json`:

```json
{
  "targets": [
    { "name": "prod", "ip": "1.2.3.4", "user": "root", "key": "~/.ssh/espx_vultr", "last_used": "2026-..." }
  ],
  "default": "prod"
}
```

On each run:
1. If `deploy-targets.json` exists and has targets, list them and ask the user: reuse an existing target or add a new one.
2. If new, prompt for IP, user, key; validate SSH connectivity (`ssh ... echo ok`).
3. Save/update the file and mark the chosen target as default + update `last_used`.

## Flow

### Step 1 — Select target (GATE)

1. Read `./config/deploy-targets.json` if present.
2. Present existing targets; ask reuse vs new.
3. If no IP is provided and `vultr-cli` is available, run `vultr-cli instance list` and suggest picking one of the listed instances (show label, IP, region, status). Allow manual IP entry as fallback.
4. Collect missing fields (IP, user, key); test SSH.
5. Persist. **GATE**: confirm target before proceeding.

### Step 2 — Assess server state

Check the server against `config/specification-server.md` (tools, services, layout, nginx, docker). Flag any missing prerequisite.

Run over SSH:
- `ls $PROJECT_PATH` to detect existing install.
- `systemctl is-active simlive-api simlive-planner simlive-engines`.
- Check installed Go version vs `go.mod`.
- Fetch server `.env` (`cat $PROJECT_PATH/.env`) and diff against local `.env`.
- Check firewall (`ufw status`) and open `80/tcp`, `443/tcp` when nginx is used.

Report: fresh install vs update. Suggest actions based on diff (new binaries, config changes, client build).

#### Fresh-server provisioning (if missing)
- Install base packages: `docker.io`, `nginx`, `ffmpeg`, `rclone`.
- Install Docker Compose plugin (`docker-compose-plugin`) if `docker compose` is unavailable.
- Install tsduck from the matching Ubuntu `.deb` (see https://tsduck.io/tsduck-binaries/), then `apt-get -f install`.
- Install Go from the official tarball matching `go.mod` (e.g. `go1.26.6.linux-amd64.tar.gz`) into `/usr/local/go` and add to PATH.
- Start Docker and bring up `docker compose up -d` in `$PROJECT_PATH`.
- Ensure `$PROJECT_PATH` exists before syncing.

### Step 2b — Generate / sync .env (GATE)

1. Show a unified diff between local `.env` and server `.env` (if server has one).
2. Suggest changes:
   - New keys present locally but missing on server.
   - Values that differ (e.g., `TMP_DIR`, `MAX_WORKERS`, `ACTIVE_RENDITIONS`).
   - Keys on server not in local (keep or remove).
3. Generate a merged `.env` proposal (local as base, apply server-specific overrides the user confirms).
4. **GATE**: user approves the final `.env` content before upload. Never overwrite server `.env` without confirmation.

### Step 2c — Prepare local config (before sync)

- The deploy scripts read `./config/.env`. Update `config/.env` only; do not modify the repo-root `.env`. If `config/.env` is missing, copy/merge from root `.env` into `config/.env`, then edit `config/.env`.
- Keep `config/rclone/` present (even if empty) so `deploy-server.sh` does not fail on missing directory.
- Keep `config/ssl/` with a wildcard cert/key for the server hostname when nginx SSL is required.

### Step 3 — Choose what to sync (GATE)

Offer checkboxes (multi-select):
- Config + systemd units (`deploy-server.sh`)
- App binaries (`deploy-app.sh` / `make build`)
- Client bundle (`deploy-client.sh` / `make build-client`)
- Environment file (`.env`)
- Go toolchain (if version mismatch)

Suggest defaults based on Step 2 findings (e.g., if only Go code changed → app; if service files changed → config). **GATE**: user confirms selection.

### Step 4 — Execute

Run from the correct working directories and pass the server IP where required:
1. `make build` from repo root (if app selected)
2. `make build-client` from repo root (if client selected)
3. `bash config/scripts/deploy-server.sh <ip>` from repo root (config + units + env + scripts)
4. `bash config/scripts/deploy-app.sh <ip>` only if binaries were not already synced by deploy-server
5. `bash config/scripts/deploy-client.sh <ip>` from repo root (if client selected)
6. For fresh installs, run `enable-services.sh` on the server; otherwise `restart-services.sh`

Important:
- `deploy-server.sh` must run from `config/` (or via `bash config/scripts/...` from repo root) and uses relative `./bin`, `./.env`, `./rclone`.
- `deploy-client.sh` requires the server IP argument; it rebuilds the client and tars `client/dist`.
- `deploy-server.sh` copies `./bin` into `$PROJECT_PATH/bin`; avoid creating nested `bin/bin` (clean target or use `cp`/`rsync` contents).
- Stream output with a generous timeout (large binaries); stop on first failure and report.

### Step 5 — Verify

- `ssh ... systemctl status simlive-*`
- `curl http://<ip>:8080/` and `https://<host>/` when nginx is configured
- `curl http://<ip>:8080/live/.../playlist.m3u8` (if channel exists)
- `rclone lsd <remote>:` on the server
- Report success/failure per service.

### Step 6 — Record

Append/update `./config/DEPLOY.md` with:
- Date, target (name/ip), action (fresh/update), synced components, versions (git commit hash, Go version), verification result, notes.

## Done Criteria

- SSH connectivity verified.
- Selected components synced without errors.
- Services active on server.
- `./config/DEPLOY.md` updated.

## Notes

- Prefer reusing persisted targets; always confirm before overwriting.
- Never store private key contents; only paths.
- For fresh servers, provision prerequisites first (Docker, Compose plugin, nginx, ffmpeg, tsduck, rclone, Go).
- Configure nginx with `config/ssl` certs and open firewall ports 80/443.
- Sync rclone config from `~/.config/rclone/rclone.conf` to `/root/.config/rclone/rclone.conf`.
- Ask before destructive steps (rm, overwrite .env).
- Capture the git commit hash (`git rev-parse HEAD`) in DEPLOY.md.
- If `deploy-server.sh` fails due to missing `config/rclone`, create an empty directory and retry.