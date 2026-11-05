# Quickstart: Dynamic Partner Communication Data Downloader

**Feature**: 018-partner-data-downloader

## Prerequisites
- Go 1.26+
- Existing partner.yaml setup
- rclone installed (for rclone provider)

## Validation Scenarios

### 1. Default provider (backward compatible)
```yaml
# partner.yaml
active: default
partners:
  default:
    api:
      base_url: http://localhost:3030
      timeout: 10s
```
**Expected**: Uses rclone (no `data_provider` specified). Behavior unchanged.

### 2. Explicit HTTP provider
```yaml
# partner.yaml
active: partner-http
partners:
  partner-http:
    api:
      base_url: http://localhost:3030
      timeout: 10s
    data_provider: http
    data:
      endpoint: http://partner.example.com/communication-data
      headers:
        X-API-Key: secret
      timeout: 30s
```
**Expected**: Uses HTTP client, applies headers and timeout.

### 3. Invalid provider rejected
```yaml
partners:
  bad:
    data_provider: ftp  # unknown
```
**Expected**: Config load fails with clear error.

### 4. HTTP without endpoint rejected
```yaml
partners:
  bad:
    data_provider: http
    # missing data.endpoint
```
**Expected**: Config load fails with clear error.

### 5. Per-request config reloading
Change `partner.yaml` and make request - provider/endpoint changes take effect without restart.

## Testing
```bash
go test ./internal/partner/... ./internal/downloader/...
go test ./...
```
