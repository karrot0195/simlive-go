simlive/
├── cmd/
├── internal/
│   ├── api/             # Module 1: REST API (Gin Gonic)
│   ├── prepare/         # Module 2: PrepareSimlive (Worker/Cron)
│   └── start/           # Module 3: StartSimlive (HLS Engine)
├── assets/
│   └── slate.mp4        # Video chờ/fallback khi channel không có dữ liệu
├── Dockerfile
├── docker-compose.yml
├── .env
└── go.mod