# Feature Specification: Partner Demo Server

**Feature Branch**: `019-partner-demo-server`

**Created**: 2026-11-05

**Status**: Draft

**Input**: User description: "create ./cmd/partner/main.go

support provide simple for demo if partner still dont have API yet 

@partner.example.yaml  <-- just need simple 

AUTH, use http for dowload, provide some simple API for support."

## Clarifications

### Session 2026-11-05

- Q: What auth flow should the demo server implement? → A: Simple token (Recommended)
- Q: Which HTTP endpoints should the demo server expose? → A: Minimal fixed paths
- Q: Where should demo data come from? → A: From files

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run a simple partner demo server (Priority: P1)

An operator can start a lightweight partner demo server that serves the minimal API surface needed for local/demo integration without needing a full partner backend.

**Why this priority**: Enables demos and local testing when partners don't have an API yet.

**Independent Test**: Start the demo server and call its endpoints with sample data; verify it returns expected responses.

**Acceptance Scenarios**:

1. **Given** a simple partner config, **When** `./cmd/partner/main.go` starts, **Then** it serves HTTP endpoints on a configurable port.
2. **Given** the demo server is running, **When** a client requests channel/playlists data via minimal fixed paths, **Then** it returns valid sample responses.
3. **Given** the demo server is running, **When** a client requests EPG/schedule data via minimal fixed paths, **Then** it returns valid sample responses.

---

### User Story 2 - Simple auth support (Priority: P1)

The demo server provides simple token-based auth flows so that simlive's auth integration can be tested end-to-end.

**Why this priority**: Auth is required for integration; demo needs to support basic auth flows.

**Independent Test**: Exercise auth endpoints and verify token issuance/verification behave as expected by the client.

**Acceptance Scenarios**:

1. **Given** auth is configured simply, **When** credentials are submitted, **Then** the demo server returns a simple token.
2. **Given** a token is presented, **When** verified via the auth verification endpoint, **Then** it is accepted or rejected as configured.

---

### User Story 3 - HTTP download support for partner data (Priority: P1)

The demo server serves partner communication data over HTTP from files so that `ClientDownload` can be used in demo mode.

**Why this priority**: Works with the existing Downloader interface (feature 018) to allow HTTP-based data retrieval in demos.

**Independent Test**: Configure a partner profile to use HTTP data provider pointing to the demo server; verify data is downloaded successfully.

**Acceptance Scenarios**:

1. **Given** the demo server is serving communication data files over HTTP, **When** a client requests it, **Then** it returns the expected data payload.
2. **Given** the demo server serves data files from disk, **When** requested, **Then** it returns correct content with appropriate headers.

---

### Edge Cases

- Demo server handles missing/invalid auth gracefully with clear errors
- Demo server serves minimal valid data when sample files missing
- Configurable via simple partner.yaml configuration
- Works without external dependencies

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide `./cmd/partner/main.go` as a standalone demo server binary
- **FR-002**: Demo server MUST read configuration from `partner.yaml` (simple form based on `partner.example.yaml`)
- **FR-003**: Demo server MUST support simple token-based auth flows (login/token issuance and verification)
- **FR-004**: Demo server MUST serve partner API endpoints needed for channel list and EPG/schedules via minimal fixed paths
- **FR-005**: Demo server MUST serve partner communication data files over HTTP for download by `ClientDownload`
- **FR-006**: Demo server configuration MUST be simple (minimal fields needed for demo)
- **FR-007**: Demo server MUST work independently without requiring full partner infrastructure
- **FR-008**: Demo server MUST load demo data from files on disk

### Key Entities *(include if feature involves data)*

- **Demo Partner Server**: Lightweight HTTP server providing mock partner APIs for testing/demo
- **Demo Auth**: Simple token-based auth mechanism for token issuance and verification
- **Demo Data Provider**: HTTP endpoint serving communication data files from disk

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Demo server can be started with a simple `partner.yaml` config
- **SC-002**: All required partner endpoints respond successfully in demo mode via minimal fixed paths
- **SC-003**: Simple token-based auth flow works end-to-end with simlive auth integration
- **SC-004**: HTTP data download works with `ClientDownload` from feature 018 using files from disk
- **SC-005**: Setup requires minimal configuration changes from `partner.example.yaml`

## Assumptions

- Demo server is for local/demo use only, not production
- Demo data is stored as files on disk (e.g., in a data/ directory)
- Configuration follows the existing `partner.yaml` structure where possible
- Compatible with the Downloader interface from feature 018
- Uses standard library HTTP server or lightweight framework consistent with project style
