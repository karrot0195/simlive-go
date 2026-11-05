# Contract: Downloader Interface

**Package**: `internal/downloader`  
**Version**: 1.0.0

## Interface

```go
package downloader

import "context"

// Downloader obtains partner communication data.
type Downloader interface {
    // Fetch retrieves data from the given source.
    Fetch(ctx context.Context, source string) ([]byte, error)
}
```

## Factory

```go
// New creates a Downloader for the given partner profile.
func New(p *partner.PartnerProfile) (Downloader, error)
```

## Implementations

### RcloneDownload
- **Constructor**: `NewRclone(svc *storage.RcloneService) *RcloneDownload` or similar
- **Behavior**: Uses rclone to fetch data; preserves existing behavior and timeouts
- **Errors**: Returns errors from rclone operations

### ClientDownload  
- **Constructor**: `NewClient(endpoint string, headers map[string]string, timeout time.Duration) *ClientDownload`
- **Behavior**: HTTP GET to endpoint with headers and timeout applied
- **Errors**: Returns errors for non-2xx status codes and network failures; times out per configured timeout

## Configuration (partner.yaml)

```yaml
partners:
  example:
    data_provider: http  # or rclone (default)
    data:
      endpoint: https://partner.example.com/data
      headers:
        Authorization: Bearer token
      timeout: 30s
    # or inline under api/data depending on design
```

**Note**: Exact YAML structure to be finalized in implementation to match existing patterns and pass validation.
