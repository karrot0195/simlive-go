# Research: Dynamic Partner Communication Data Downloader

**Date**: 2026-11-05  
**Feature**: 018-partner-data-downloader

## Decisions

### 1. Package location for Downloader abstraction
- **Decision**: Create `internal/downloader/` package with `downloader.go` (interface + factory), `rclone.go`, `http.go`
- **Rationale**: Library-first per constitution; clean separation of concerns; easier to test/mock; doesn't pollute `storage/` or `partner/` with interface definition if used broadly
- **Alternatives considered**: Put in `internal/storage/` (tightly couples to storage concerns), put in `internal/partner/` (couples config/types to implementation)

### 2. Interface shape
- **Decision**: `type Downloader interface { Fetch(ctx context.Context, source string) ([]byte, error) }` or method name that matches "obtain partner communication data" generically. Also consider `Download`/`Get`/`Fetch`. Need to see existing call sites - spec says "obtain partner communication data" generically.
- **Rationale**: Keep minimal and extensible (FR-014). Callers depend only on interface (FR-012).
- **Alternatives considered**: Multiple methods per data type - would leak specifics, violates unified contract goal.

### 3. Provider selection location
- **Decision**: Add `DataProvider` to `PartnerProfile` (or `PartnerAPI`/top-level). Default `rclone`. Validate at config load time.
- **Rationale**: Per-partner (FR-007), default rclone (FR-006), validated at load (FR-008/FR-009), config read per request (FR-013).
- **Alternatives considered**: Global setting - contradicts FR-007.

### 4. HTTP implementation details
- **Decision**: `ClientDownload` reads endpoint from profile (FR-004), applies headers and timeout (FR-010), returns errors on non-2xx/network failures (FR-011). Use `net/http` stdlib.
- **Rationale**: Matches spec, no new deps.
- **Alternatives considered**: Using existing `PartnerClient` - `PartnerClient` is for specific partner API endpoints (channels/EPG); spec says "endpoint on partner.yaml" for data provider, different concern.

### 5. Rclone implementation
- **Decision**: Wrap existing `storage.RcloneService` or call rclone directly. Preserve current behavior (FR-002).
- **Rationale**: Reuse existing timeouts/error handling.
- **Alternatives considered**: Reimplementing - unnecessary duplication.

### 6. Factory/resolver
- **Decision**: Provide `NewDownloader(profile *partner.PartnerProfile) (Downloader, error)` or similar that selects based on profile config.
- **Rationale**: Centralizes selection logic, validates config.
- **Alternatives considered**: Callers construct directly - would leak provider logic.
