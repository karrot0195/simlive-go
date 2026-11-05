# API Contracts: Partner Demo Server

## Auth Endpoints (Simple Token)

### POST /auth/login (or configurable)
Request: `{"username": "...", "password": "..."}`
Response: `{"token": "..."}`

### POST /auth/verify (or configurable)
Request: `{"token": "..."}` or via header
Response: `{"valid": true/false}`

## Partner API Endpoints (Minimal Fixed Paths)

### GET /channels or /api/v2/common/channel-playouts
Returns channel list data from file

### GET /epg or /api/v2/common/channel-epg/*
Returns EPG/schedule data from file

## Data Download Endpoint

### GET /data/* or configurable path
Serves communication data files for ClientDownload
Returns file contents with appropriate headers
