# Feature Specification: Dynamic Partner Communication Data Downloader

**Feature Branch**: `018-partner-data-downloader`

**Created**: 2026-11-05

**Status**: Draft

**Input**: User description: "implement dynamic partner communication data tool. now -> use rclone. create interface for it as Downloader. RcloneDownload will extend from it. ClientDownload new extend from it via http. should define endpoint on partner.yaml. inside partner.yaml will define which provider tool will use. (default = rclone)"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Choose a data provider per partner (Priority: P1)

An operator configures a partner profile and selects which tool the platform uses to obtain partner communication data. By default the platform uses rclone, so existing deployments keep working without changes.

**Why this priority**: The platform currently depends on a single rclone-based mechanism. Making the provider selectable per partner is the core change that unlocks HTTP-based partners.

**Independent Test**: Load a partner profile with no provider specified and confirm rclone is selected. Load a profile with an HTTP provider specified and confirm the HTTP client is selected instead.

**Acceptance Scenarios**:

1. **Given** a partner profile with no data provider declared, **When** the platform starts serving that profile, **Then** it uses rclone to obtain partner communication data.
2. **Given** a partner profile that declares an HTTP data provider, **When** the platform serves that profile, **Then** it uses the HTTP client to obtain partner communication data.
3. **Given** a partner profile that declares an unknown provider, **When** the platform loads it, **Then** it rejects the configuration with a clear error rather than silently falling back.
4. **Given** an existing deployment that only uses rclone, **When** this feature is enabled, **Then** no configuration change is required and behavior is unchanged.

---

### User Story 2 - Download partner data over HTTP (Priority: P1)

A partner that exposes its communication data over HTTP (instead of rclone-accessible storage) can be supported by declaring an endpoint in partner.yaml. The platform fetches the data through that endpoint using the same Downloader contract as rclone.

**Why this priority**: This is the new capability being added; without it only rclone-backed partners can be served.

**Independent Test**: Point a profile at an HTTP endpoint serving a known payload and confirm the platform retrieves the same bytes as the rclone path for an equivalent source.

**Acceptance Scenarios**:

1. **Given** a partner profile with an HTTP data provider and a valid endpoint, **When** the platform requests data, **Then** it issues an HTTP request to that endpoint and returns the response body.
2. **Given** an HTTP endpoint that returns an error status, **When** the platform requests data, **Then** it surfaces a clear error and does not treat the response as valid data.
3. **Given** an HTTP endpoint that is unreachable, **When** the platform requests data, **Then** it fails within the configured timeout rather than hanging.
4. **Given** configured headers and timeout on the partner profile, **When** the HTTP request is made, **Then** those headers and timeout are applied.

---

### User Story 3 - Unified Downloader contract (Priority: P2)

All data acquisition paths go through one shared Downloader interface, so callers do not need to know which underlying tool is in use. Switching providers requires no change to callers.

**Why this priority**: Keeps the feature maintainable and prevents provider-specific logic from leaking into business code.

**Independent Test**: Implement a fake Downloader in tests and confirm callers accept it interchangeably with the rclone and HTTP implementations.

**Acceptance Scenarios**:

1. **Given** two Downloader implementations (rclone and HTTP), **When** a caller uses either, **Then** the observable behavior and return types are identical.
2. **Given** a new provider added later, **When** it implements the Downloader interface, **Then** it can be wired in without changing callers.

---

### Edge Cases

- A partner profile declares both rclone and HTTP provider settings: the explicit provider selection wins; the other settings are ignored.
- An HTTP endpoint URL is missing while HTTP provider is selected: configuration is rejected at load time.
- rclone is selected but the remote path is unreachable: the existing rclone error behavior applies unchanged.
- Provider selection changes between requests are reflected because partner.yaml is read per request.
- Concurrent requests using different providers do not share state.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST expose a single `Downloader` interface that abstracts partner communication data acquisition, independent of the underlying tool.
- **FR-002**: The system MUST provide an `RcloneDownload` implementation of `Downloader` that obtains data using rclone, preserving current behavior.
- **FR-003**: The system MUST provide a `ClientDownload` implementation of `Downloader` that obtains data over HTTP.
- **FR-004**: `ClientDownload` MUST read its endpoint from the partner profile in `partner.yaml`.
- **FR-005**: Each partner profile in `partner.yaml` MUST be able to declare which data provider to use.
- **FR-006**: The default data provider MUST be rclone when none is declared.
- **FR-007**: The selected provider MUST be resolved per partner profile, so different partners may use different providers.
- **FR-008**: An invalid or unknown provider value MUST be rejected with a clear error at configuration load time.
- **FR-009**: When HTTP is selected, a missing endpoint MUST be rejected at configuration load time.
- **FR-010**: `ClientDownload` MUST apply the profile's configured headers and timeout to HTTP requests.
- **FR-011**: `ClientDownload` MUST surface non-success HTTP responses and network failures as errors.
- **FR-012**: Callers of data acquisition MUST depend only on the `Downloader` interface, not on a concrete provider.
- **FR-013**: Provider selection MUST take effect without a process restart, because `partner.yaml` is read per request.
- **FR-014**: The set of supported providers MUST be extensible without modifying callers.

### Key Entities *(include if feature involves data)*

- **Downloader**: The shared contract for obtaining partner communication data; hides the underlying tool.
- **RcloneDownload**: A Downloader backed by rclone subprocess calls.
- **ClientDownload**: A Downloader backed by HTTP requests to a configured endpoint.
- **Partner Profile (partner.yaml)**: Per-partner configuration that now also declares the data provider and, for HTTP, the endpoint.
- **Data Provider Selection**: The field in a partner profile that names which Downloader implementation to use.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A partner profile with no provider declared uses rclone in 100% of data requests.
- **SC-002**: A partner profile declaring the HTTP provider completes data requests against its configured endpoint with the same success rate as rclone-backed partners.
- **SC-003**: Switching a partner's provider in `partner.yaml` takes effect on the next request without a restart.
- **SC-004**: 100% of invalid provider or missing-endpoint configurations are rejected at load time with a descriptive error.
- **SC-005**: Callers require zero changes when a new provider is added, verified by adding a test-only provider.
- **SC-006**: Existing rclone-only deployments experience no behavior change after enabling this feature.

## Assumptions

- rclone remains the default and existing behavior is preserved for profiles that do not opt in.
- HTTP endpoints return the same logical data that rclone paths provide; format/semantics of the data itself are unchanged.
- Provider selection is per partner profile, not global.
- The Downloader interface covers data acquisition only; it does not change auth, XML, or EPG flows.
- `partner.yaml` continues to be read per request, so provider changes are dynamic.
- No new external dependencies are introduced; HTTP uses the standard library.