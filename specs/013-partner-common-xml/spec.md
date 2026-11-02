# Feature Specification: Partner Common XML Source

**Feature Branch**: `013-partner-common-xml`

**Created**: 2026-11-02

**Status**: Draft

**Input**: User description: "common purpose source — all config for PARTNER API will define on @partner.yaml (config/auth.yaml will move inside); 2 new endpoint — Channel LIST: /common/channel-playouts-xml, EPG LIST: /common/channel/epg/:channelId?$include_metadata=true; extend can custom inside XML define for every new partner, just follow format XML; CHANNEL: id, name, img; epgitem: programme start, stop, streamId, ads — remove interval default ads now"

## Clarifications

### Session 2026-11-02

- Q: How does an incoming request select which partner profile applies when `partner.yaml` can define multiple partners? → A: One active partner profile per deployment.
- Q: What is the authoritative source of the channel `img` value? → A: The image/logo field provided by the partner API channel source.
- Q: How far should removal of interval-based default ads go? → A: Remove globally; all outputs (timeline, HLS, and partner XML) use only EPG-declared ad programmes.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Fetch the channel list as partner-facing XML (Priority: P1)

A partner system calls a single common endpoint and receives the list of playout channels in XML. Each channel carries the identifier, display name, and image reference the partner needs to present the channel lineup.

**Why this priority**: Without the channel list there is nothing for a partner to browse or map to its own lineup; it is the entry point of the integration.

**Independent Test**: Call the channel list endpoint and confirm the response is well-formed XML containing one entry per available channel with `id`, `name`, and `img`.

**Acceptance Scenarios**:

1. **Given** at least one channel exists, **When** a partner requests the channel list, **Then** the response is well-formed XML with one channel entry per channel containing `id`, `name`, and `img`.
2. **Given** no channels are available, **When** a partner requests the channel list, **Then** the response is well-formed XML with an empty channel collection and no error.
3. **Given** the partner API source is unavailable, **When** a partner requests the channel list, **Then** the system returns a clear failure response instead of malformed XML.

---

### User Story 2 - Fetch a channel's EPG as partner-facing XML (Priority: P1)

A partner system requests the programme guide for one channel and receives XML items describing each programme's start, stop, stream identifier, and ad marker. Optional metadata can be requested when the partner needs richer descriptions.

**Why this priority**: The EPG is the payload partners use to schedule and play content; without it the channel list alone has no value.

**Independent Test**: Call the EPG endpoint for a known channel and confirm each item has `start`, `stop`, `streamId`, and `ads`, with optional metadata appearing only when requested.

**Acceptance Scenarios**:

1. **Given** a channel with a schedule for the requested period, **When** a partner requests its EPG, **Then** the response is well-formed XML containing one item per programme with `start`, `stop`, `streamId`, and `ads`.
2. **Given** the request includes `$include_metadata=true`, **When** the EPG is returned, **Then** each item additionally carries the optional metadata defined for that partner.
3. **Given** the request omits `$include_metadata` or sets it to false, **When** the EPG is returned, **Then** the optional metadata elements are absent.
4. **Given** a channel identifier that does not exist, **When** the EPG is requested, **Then** the system returns a not-found response instead of an empty or malformed guide.

---

### User Story 3 - Onboard a new partner through configuration only (Priority: P2)

An operator adds a new partner by editing `partner.yaml`. The base XML structure is preserved, while partner-specific fields, names, and optional metadata are added declaratively. No application code changes or client rebuilds are required to onboard the partner.

**Why this priority**: The core purpose is a reusable "common" source so each new partner is a configuration exercise rather than a development project.

**Independent Test**: Add a second partner profile to `partner.yaml` that extends the base XML with one extra field and confirm the endpoint serves both the base contract and the extra field for that partner without code changes.

**Acceptance Scenarios**:

1. **Given** a partner profile that declares extra XML fields, **When** that partner requests the channel list or EPG, **Then** the response contains the base fields plus the partner's declared fields in the documented XML format.
2. **Given** a partner profile with no customisation, **When** it requests either endpoint, **Then** the response matches the base XML format exactly.
3. **Given** an invalid or missing customisation entry, **When** the endpoints are called, **Then** the base XML contract is still served and the configuration problem is reported to operators.

---

### User Story 4 - Consolidate all partner API configuration in one file (Priority: P2)

An operator manages every partner-facing integration detail in `partner.yaml`: the partner API connection details and the authentication flow that previously lived in `config/auth.yaml`. The existing auth configuration endpoint continues to work by serving the auth section from the consolidated file.

**Why this priority**: Duplicate configuration files drift; one file makes each partner deployment self-contained.

**Independent Test**: Remove `config/auth.yaml`, start the system with an equivalent `partner.yaml`, and confirm the auth configuration endpoint still returns the same login flow.

**Acceptance Scenarios**:

1. **Given** a `partner.yaml` containing both partner API and auth sections, **When** the auth configuration is requested, **Then** the response reflects the auth section of `partner.yaml`.
2. **Given** `config/auth.yaml` no longer exists, **When** the system starts and serves auth configuration, **Then** it operates from `partner.yaml` without loss of behavior.
3. **Given** an invalid `partner.yaml`, **When** the system starts, **Then** it falls back to a usable default auth configuration and reports the configuration error rather than failing to serve.

---

### User Story 5 - Ads come from the EPG, not a default interval (Priority: P2)

An operator expects the ad markers partners receive to match the ad programmes actually declared in the EPG. The automatic "insert an ad every fixed interval" behavior is removed so that no interval-generated ads appear in the timeline or the partner XML.

**Why this priority**: Interval-based ads add breaks the partner did not schedule, breaking the EPG contract and ad reconciliation.

**Independent Test**: Prepare a channel whose EPG contains both normal programmes and explicit ad programmes; confirm the partner EPG XML marks only the declared ad programmes and that no intervals of default ads appear.

**Acceptance Scenarios**:

1. **Given** an EPG with explicit ad programmes, **When** the EPG XML is generated, **Then** those items carry the ad marker and the correct stream identifier.
2. **Given** an EPG with no declared ad programmes, **When** the EPG XML is generated, **Then** no item carries an ad marker, regardless of elapsed playback time.
3. **Given** a long-running channel, **When** its timeline is inspected after the change, **Then** no ad break appears solely because a fixed interval elapsed.

---

### Edge Cases

- The requested channel exists but has no schedule for the requested day: return a well-formed XML guide with zero items, not an error.
- The upstream partner API is slow or unavailable: return a clear error response for live requests, or the retained last-known-good EPG where that fallback already exists, rather than partial XML.
- A programme has no primary playout stream: omit the programme from the EPG rather than emit an item with an empty `streamId`.
- A programme has multiple streams: use the same primary-stream selection rule the playout pipeline already applies.
- A channel has no image reference: emit an empty `img` element/attribute consistently; do not omit the field from the channel element.
- A custom partner field references a value that is absent for an item: omit that field for that item without invalidating the XML.
- The `$include_metadata` value is unrecognised or repeated: treat unrecognised values as not requested and return the base format.
- Programme times cross a day boundary or a daylight-saving transition: `start` and `stop` remain unambiguous and ordered.
- A channel identifier contains path-traversal characters: reject it before any lookup.
- Concurrent requests for the same channel: responses remain independent and deterministic.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST expose a partner-facing channel list endpoint at `/common/channel-playouts-xml`.
- **FR-002**: Each channel in the channel list response MUST include `id`, `name`, and `img`.
- **FR-003**: System MUST expose a partner-facing EPG endpoint at `/common/channel/epg/:channelId`.
- **FR-004**: The EPG endpoint MUST accept an optional `$include_metadata` query parameter and include the partner's optional metadata only when the value is true.
- **FR-005**: Each EPG item MUST include `start`, `stop`, `streamId`, and `ads`.
- **FR-006**: Both endpoints MUST return well-formed XML with the documented root structure and a partner-compatible content type.
- **FR-007**: System MUST support partner-specific XML customisation declared in `partner.yaml`, extending the base format with additional fields without changing the base fields.
- **FR-008**: Adding or changing a partner's XML customisation MUST NOT require application code changes or client rebuilds.
- **FR-009**: `partner.yaml` MUST hold all partner API configuration, including the partner API connection details, endpoint paths, headers, and authentication flow currently held in `config/auth.yaml`.
- **FR-010**: The existing auth configuration served to the client MUST be sourced from the auth section of `partner.yaml`, with `config/auth.yaml` no longer required.
- **FR-011**: System MUST remove interval-based default ad insertion; ads MUST be derived from ad markers declared in the EPG.
- **FR-012**: The EPG XML `ads` value MUST reflect the EPG-declared ad status of each programme.
- **FR-013**: The EPG endpoint MUST accept the playout channel identifier and resolve the corresponding guide internally.
- **FR-014**: Requests for unknown channels MUST return a not-found response that is distinguishable from an empty guide.
- **FR-015**: Requests for channel identifiers containing path-traversal characters MUST be rejected.
- **FR-016**: Configuration errors or missing customisation MUST degrade to the base XML format while being reported to operators.
- **FR-017**: Each deployment MUST apply exactly one active partner profile from `partner.yaml` to all requests on the common endpoints.
- **FR-018**: The `img` value MUST come from the image/logo field exposed by the partner API channel source, and MUST be empty when that source has no image.

### Key Entities *(include if data involved)*

- **Partner Profile**: A named configuration describing one partner's API connection, authentication flow, and XML customisations; extends the shared base XML format.
- **Channel Entry**: A partner-facing representation of a playout channel with `id`, `name`, `img`, and any partner-declared extensions.
- **EPG Item**: A programme slot with `start`, `stop`, `streamId`, `ads`, and optional partner metadata.
- **Ad Marker**: The flag and associated stream reference that identifies a programme as advertising rather than regular content.
- **Auth Flow**: The ordered login steps and verification rules formerly held in `config/auth.yaml`, now part of the partner profile.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of channel list and EPG responses parse successfully with a standard XML parser.
- **SC-002**: Onboarding a new partner requires editing only `partner.yaml`; zero code or client changes are needed.
- **SC-003**: Every channel entry contains all three required fields (`id`, `name`, `img`) in 100% of responses.
- **SC-004**: Every EPG item contains all four required fields (`start`, `stop`, `streamId`, `ads`) in 100% of responses.
- **SC-005**: The partner EPG contains zero interval-generated ad items, and 100% of declared ad programmes are marked as ads.
- **SC-006**: Removing `config/auth.yaml` produces no observable change in the login flow served to the client.
- **SC-007**: A partner can retrieve a complete guide for one day for a known channel in a single request and in under 5 seconds under normal operating conditions.

## Assumptions

- The example base address `http://localhost:3030` is deployment-specific; the endpoints are served under `/common` on the host that exposes the SimLive API.
- `partner.yaml` may describe several partners, but each deployment activates one profile; the active profile supplies the XML customisations for every request.
- `$include_metadata` defaults to not requested; any value other than true excludes optional metadata.
- `:channelId` is the playout channel identifier returned by the channel list; the system resolves the partner guide identifier internally.
- `streamId` uses the same primary-stream selection rule as the playout planner.
- The `img` reference is the image/logo field from the partner API channel source; channels without a source image return an empty `img`.
- Interval-based ad insertion is removed globally. Timeline, HLS, and partner XML all use only the ad programmes declared in the EPG.
- The existing admin JSON endpoints remain unchanged and are out of scope.
- Partner XML customisation is declarative and limited to the documented XML format; arbitrary scripting is out of scope.
- Authentication and access control for the new common endpoints follow the existing partner API exposure model.
- Time values in the EPG are rendered in the channel's content timezone unless a partner profile overrides the format.
- Retained last-known-good EPG fallback behavior already present in the planner continues unchanged; the common EPG endpoint uses the live partner source in v1.
