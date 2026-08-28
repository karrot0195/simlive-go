simlive/
├── cmd/
│   └── main.go                 # API, Planner & Engine entrypoint
├── config/
│   └── config.go               # Environment variables & config loading
├── internal/
│   ├── api/                    # REST API endpoints & ESPX client
│   │   ├── espx_client.go      # HTTP client calling the 3 ESPX APIs
│   │   └── handler.go          # Handlers serving HLS playlists & channels
│   ├── database/               # Postgres & Redis initialization
│   │   └── db.go
│   ├── models/                 # Postgres & Redis structs
│   │   └── models.go
│   ├── planner/                # PrepareSimlive module
│   │   └── planner.go
│   ├── engine/                 # StartSimlive module (HLS Packager & Worker Pool)
│   │   └── engine.go
│   └── storage/                # Rclone execution wrapper
│       └── rclone.go
├── docker-compose.yml
└── Dockerfile