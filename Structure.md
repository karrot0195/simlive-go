simlive/
├── cmd/
│   └── main.go                 # Entrypoint khởi chạy API, Planner & Engine
├── config/
│   └── config.go               # Load biến môi trường & config
├── internal/
│   ├── api/                    # REST API endpoints & client ESPX
│   │   ├── espx_client.go      # HTTP Client gọi 3 API ESPX
│   │   └── handler.go          # Handlers phục vụ HLS playlist & channel
│   ├── database/               # Khởi tạo Postgres & Redis
│   │   └── db.go
│   ├── models/                 # Struct Postgres & Redis
│   │   └── models.go
│   ├── planner/                # Module PrepareSimlive
│   │   └── planner.go
│   ├── engine/                 # Module StartSimlive (HLS Packager & Worker Pool)
│   │   └── engine.go
│   └── storage/                # Wrapper thực thi Rclone
│       └── rclone.go
├── docker-compose.yml
└── Dockerfile