# Data Model: Dynamic Partner Communication Data Downloader

**Date**: 2026-11-05  
**Feature**: 018-partner-data-downloader

## Entities

### Downloader (interface)
- **Type**: Interface
- **Package**: `internal/downloader`
- **Purpose**: Shared contract for obtaining partner communication data
- **Methods**: 
  - `Fetch(ctx context.Context, source string) ([]byte, error)` or similar - exact signature to be determined by call sites; must be generic enough for both rclone and HTTP

### RcloneDownload
- **Type**: Struct implementing Downloader
- **Package**: `internal/downloader`
- **Purpose**: Obtains data using rclone (preserves current behavior)
- **Dependencies**: May wrap `*storage.RcloneService`

### ClientDownload
- **Type**: Struct implementing Downloader
- **Package**: `internal/downloader`
- **Purpose**: Obtains data over HTTP
- **Configuration** (from partner profile):
  - Endpoint URL
  - Headers (map[string]string)
  - Timeout (time.Duration)
- **Behavior**: Applies headers/timeout, surfaces non-2xx and network errors

### PartnerProfile (extended)
- **Type**: Struct (existing in `internal/partner`)
- **New fields**:
  - `DataProvider` or `Data`/`Download` config section with provider selection
- **Validation**: Unknown provider rejected; HTTP requires endpoint

### DataProviderConfig / Provider Selection
- **Type**: Configuration
- **Fields**:
  - `Provider` string (e.g., "rclone", "http") - default "rclone"
  - For HTTP provider: `Endpoint` (required), `Headers` (optional), `Timeout` (optional)

## Validation Rules
- Default provider is `rclone` if not specified (FR-006)
- Unknown provider values rejected at load time with clear error (FR-008)
- When HTTP selected, missing endpoint rejected at load time (FR-009)
- Explicit provider selection wins if both rclone and HTTP settings declared (edge case)

## Relationships
- `PartnerProfile` contains provider configuration
- `NewDownloader(profile)` factory selects implementation based on profile
- All callers depend on `Downloader` interface only
