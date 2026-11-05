# Quickstart: Partner Demo Server

## Prerequisites
- Go 1.26+
- Simple partner.yaml config (based on partner.example.yaml)

## Setup

1. Create a simple partner.yaml for the demo server
2. Create demo data files (channels, epg, communication data)
3. Build and run the demo server

## Running

```bash
go run ./cmd/partner/main.go
```

Or build:
```bash
go build -o bin/partner ./cmd/partner/main.go
./bin/partner
```

## Testing

1. Test auth endpoint
2. Test channel list endpoint
3. Test EPG endpoint  
4. Test data download endpoint (for ClientDownload)
5. Configure simlive to use HTTP data provider pointing to demo server
