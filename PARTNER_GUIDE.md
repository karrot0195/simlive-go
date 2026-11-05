# Partner Guide

## Quick Start (Demo Partner)

1. Copy the example config:
   ```bash
   cp partner.example.yaml partner.yaml
   ```

2. Start the demo partner server:
   ```bash
   make start-partner
   # or
   ./bin/partner
   ```

3. Start simlive services:
   ```bash
   make start-all
   ```

## Default Credentials

For the demo partner server (`cmd/partner`), any username/password will work. The server returns a fixed demo token.

- **Username:** `demo` (or any value)
- **Password:** `demo` (or any value)
- **Auth Token:** `demo-token` (configurable via `PARTNER_AUTH_TOKEN` env var)

## Auth Configuration

The partner.yaml uses a simple 1-step auth flow:

```yaml
auth:
  verify:
    url: "http://localhost:3030/auth/verify"
    method: POST
    headers:
      Authorization: "{{token}}"
    clear_on_error: true
  steps:
    - title: "Login"
      fields:
        - name: username
          type: text
          label: Username
          required: true
        - name: password
          type: password
          label: Password
          required: true
      api: "http://localhost:3030/auth/login"
      method: POST
      headers:
        Content-Type: "application/json"
      body:
        username: "{{username}}"
        password: "{{password}}"
      response_field: "token"
```

## HTTP Data Download

Configure HTTP data provider to download communication data:

```yaml
data_provider: http
data:
  endpoint: "http://localhost:3030/data/sample.dat"
  timeout: "30s"
```

Place data files in `./data/partner/` directory. The demo server serves files from there via `/data/<filename>`.

## Partner XML Configuration

Configure XML extensions in `partner.yaml`:

```yaml
xml:
  channel:
    custom_fields:
      - name: "base_name"
        source: "base_name"
      - name: "provider"
        value: "partner-a"
  epg:
    custom_fields:
      - name: "title"
        source: "programme.name"
        when: "metadata"
      - name: "episode"
        source: "programme.episode"
        when: "metadata"
        default: ""
```

- `source`: Pull value from upstream data
- `value`: Set fixed value
- `when`: "always" or "metadata" (controls when field appears)
- `default`: Default value if source is missing

## API Endpoints (Demo Server)

- `POST /auth/login` - Login, returns `{"token": "..."}`
- `POST /auth/verify` - Verify token
- `GET /channels` or `/api/v2/common/channel-playouts` - Channel list
- `GET /epg` or `/api/v2/common/channel-epg/` - EPG data
- `GET /data/*` - Serve communication data files

## Environment Variables

- `PARTNER_PORT` - Demo server port (default: 3030)
- `PARTNER_DATA_DIR` - Data directory (default: ./data/partner)
- `PARTNER_AUTH_TOKEN` - Auth token (default: demo-token)
