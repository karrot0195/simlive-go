# Phase 0 Research: Partner Common XML Source

All spec clarifications are resolved. This document records the technical decisions needed before design.

## D1. Configuration format and loading

**Decision**: Introduce `config/partner.yaml` with a top-level `active` key and a `partners` map. Each profile contains `api`, `auth`, and `xml` sections. Load order is `PARTNER_CONFIG_PATH` env → `./config/partner.yaml` → legacy `./config/auth.yaml` (auth only) → built-in default auth config. The loader lives in `internal/partner` and is read per request (small file, supports live edits without restart).

**Rationale**: One file satisfies FR-009/FR-010 and keeps deployment self-contained. Per-request reads preserve the current dynamic-editing behavior of `auth.yaml` and avoid cache invalidation complexity.

**Alternatives considered**:
- Cache with `sync.Once` — rejected: requires restart or mtime logic to pick up edits.
- Environment variables for each partner field — rejected: XML extension lists are structured and do not map cleanly to env vars.
- Keep `auth.yaml` and add a separate `partner.yaml` — rejected by FR-009/FR-010.

## D2. Auth migration

**Decision**: The active profile's `auth` block is returned verbatim as JSON by `GET /api/auth/config`, preserving the existing response shape (`verify`, `steps`). If `partner.yaml` is missing, unreadable, invalid, or has no usable `auth` block, fall back to legacy `config/auth.yaml`, then to the built-in single-step default. Log every degradation. `config/auth.yaml` is removed from the repository; `config/auth.example.yaml` is replaced by `config/partner.example.yaml`.

**Rationale**: The Vue client (`client/src/libs/auth-config.ts`) needs no change because the endpoint shape is unchanged. The default keeps the system serving a usable login flow (US4 acceptance 3).

**Alternatives considered**:
- Breaking `AUTH_CONFIG_PATH` — rejected: keep it as an optional override for tests and operators.
- Serving the whole partner profile to the client — rejected: leaks API internals and breaks the client contract.

## D3. XML generation

**Decision**: Use `encoding/xml` with a small dynamic node tree (`name`, ordered children, text) and encode with `xml.Encoder`. Base elements are emitted first in fixed order, then declared custom fields in YAML order. Content-Type is `application/xml; charset=utf-8`. Errors return a well-formed `<error>` document with an HTTP status (400/404/502).

**Rationale**: `encoding/xml` is standard-library and deterministic for ordered nodes; a dynamic tree supports partner extensions without code generation. Fixed base order satisfies "follow format XML".

**Alternatives considered**:
- `text/template` XML — rejected: escaping and well-formedness become the operator's responsibility; arbitrary scripting is out of scope.
- Typed structs per partner — rejected: violates config-only onboarding (FR-008, SC-002).

## D4. Channel list source and `img`

**Decision**: The channel list endpoint proxies the profile-configured upstream channel list endpoint (default `/api/v2/common/channel-playouts` with `is_playout_assigned=true`). Decode into raw `map[string]any` items so arbitrary fields are addressable. Base fields resolve as: `id` ← `id`, `name` ← `name`, `img` ← configured source (default candidates `img`, `image`, `logo`, `thumbnail`, `poster`; object values resolve to `url` or `src`). Empty or missing image renders an empty `<img>` element. Upstream failure returns HTTP 502 with `<error>`.

**Rationale**: The partner API is already the source of channel metadata and the clarified authority for `img` (FR-018). Raw decoding lets a new partner map a differently named image field in `partner.yaml` only.

**Alternatives considered**:
- Read channels from local `playout_channels` — rejected: local rows carry no image and exclude channels not yet registered.
- Hardcode the image field name — rejected: partner-specific naming would require code changes.

## D5. EPG source, date, and timezone

**Decision**: `GET /common/channel/epg/:channelId` resolves the guide id from `playout_channels.channel_epg_id`; if the channel is absent locally, it calls the upstream channel-detail endpoint. The schedule is fetched from the profile's schedule endpoint for the requested day. Date selection: `$date` or `date` query (`YYYY-MM-DD`), default host-local today (`utils.LocalLocation()`); the upstream `tz` parameter uses `utils.LocalTZOffset()`. Output times are RFC3339 with the channel's `ContentTimezone` offset; when that is invalid, host-local time is used. No schedule data yields an empty `<epg>`; unknown channel yields 404.

**Rationale**: Matches the existing planner's day/time convention and the project's timezone preference (host-local first, `configs.TZ` fallback). Supports both partner-style and plain query names without extra endpoints.

**Alternatives considered**:
- Return a multi-day window — rejected: spec speaks of a guide/day and one-day requests keep payloads bounded (SC-007).
- Use `configs.TZ` as the primary timezone — rejected: project preference is host-local with `configs.TZ` as fallback.

## D6. Partner XML customisation

**Decision**: Base elements are fixed. `xml.channel.custom_fields` and `xml.epg.custom_fields` are ordered lists of `{name, source|value, when}` where `source` is a dotted path into the normalized item view and `when` is `always` or `metadata`. Custom names may not collide with reserved base names; invalid entries are logged and skipped. No renaming/removal of base fields is supported in v1.

**Rationale**: Satisfies FR-007/FR-008 ("extend … just follow format XML") while keeping the base contract stable for every partner. Ordered lists make output deterministic.

**Alternatives considered**:
- Full XML templates per partner — rejected: over-flexible, hard to validate, and risks breaking the base contract.
- Attributes and mixed content — rejected: not needed for the listed fields; elements keep the format uniform.

## D7. Removing interval ads and marking EPG ads

**Decision**: Delete the interval-based path: `AdInsertionInterval`/`AD_INSERTION_INTERVAL`, `loadAdSegments`, `insertAdsIfNeeded`, `processingVerifyNearingAds`, and the `adSegments`/`lastAdTime`/`isBlockingProcessing` state. In `ProcessScheduleItem`, when `programme.is_ads` is true, mark every produced segment with `IsAd=true`, a stable `AdBreakID` derived from the programme start, `AdBreakStart`, and `AdBreakDuration` (programme end minus start). Emit SCTE-35 boundaries on the first (`out`, 0x34) and last (`in`, 0x35) ad segments, plus the existing 8 s/4 s pre-roll cue on the preceding content segment when available. `UseAdEpgAds` is deprecated but kept in the model/schema so existing rows and clients do not break; the planner ignores it and always uses EPG ads.

**Rationale**: Implements the clarified global removal without disrupting HLS/PMT consumers that already key off `IsAd`, `AdType`, `AdBreakID`, `AdBreakStart`, and `AdBreakDuration`. Stable break identity keeps replans idempotent.

**Alternatives considered**:
- Keep the interval path behind a feature flag — rejected: the requirement is to remove it now; a flag would preserve the incorrect behavior.
- Only omit interval ads from XML — rejected by the clarified answer (global removal).
- Drop the `use_ad_epg_ads` column — rejected: GORM auto-migration does not drop columns and client/API compatibility would regress with no functional gain.

## D8. Error and status contract

**Decision**: Path traversal (`..`, `/`) → 400 `<error>`. Unknown channel → 404 `<error>`. Upstream/partner API failure → 502 `<error>`. Invalid schedule date → 400 `<error>`. Empty but valid result → 200 with an empty root element. All responses use the XML content type.

**Rationale**: Distinguishes "not found" from "empty guide" (FR-014) and gives partners a parseable failure format (SC-001).

**Alternatives considered**:
- JSON errors from an XML endpoint — rejected: breaks parsers and the format contract.
- Always 200 — rejected: hides upstream failures and violates FR-014.

## D9. Listen address

**Decision**: Make the API bind address configurable with `API_ADDR` (default `:8080`) so a deployment can expose the common endpoints on port 3030 as in the example without code changes.

**Rationale**: The spec's example uses `localhost:3030` while the API currently hardcodes `:8080`; an env override keeps deployment flexibility and avoids editing code per host.

**Alternatives considered**:
- Change the default to `:3030` — rejected: would break existing local `:8080` usage and the client proxy assumptions.

## D10. Caching and performance

**Decision**: No new cache for v1. Channel list and EPG requests call the upstream partner API directly; the profile's timeout bounds latency. On upstream failure return 502 rather than stale XML. The retained EPG cache remains planner-only.

**Rationale**: The feature is a read adapter; adding a cache layer is not required to meet SC-007 and would add invalidation complexity (YAGNI). The spec allows a clear failure response when the source is unavailable.

**Alternatives considered**:
- Reuse the retained-schedule file in the API — feasible later, but it lives in planner code and covers one channel/day; extracting it now adds scope without a stated requirement.

## D11. Testing strategy

**Decision**: Table-driven unit tests for config precedence/fallbacks, source-path resolution, XML base+custom output (including escaping and empty image), date/time and metadata gating, and EPG ad marking. Handler tests use `httptest` with a stub partner client and an in-memory/SQLite-free GORM path via interface seams. Planner tests assert no interval ads and that EPG ad programmes produce the expected `IsAd`/break fields. Integration verification follows `quickstart.md` against the running stack.

**Rationale**: Aligns with constitution Principles III and IV; keeps the design testable without adding dependencies.

**Alternatives considered**:
- Golden-file-only XML tests — kept as a supplement, not the sole mechanism, so failures point at a field/source.

## D12. Bucket-inclusive stream identifiers

**Decision**: The XML source's `stream_id` values (and inline ad `path` values) include the storage bucket in rclone `remote:bucket/...` form. The partner client keeps them verbatim (the `STORAGE_BUCKET` prefix strip is removed), and the planner/engine build storage paths directly from them (`<streamId>/{rendition}/<file>`). `STORAGE_BUCKET` and `ADS_STREAM_ID` environment variables and the `Settings.VideoBucket`/`AdsStreamId` fields are removed.

**Rationale**: The XML API is the authoritative source of the full storage path; a separate bucket env var duplicates that knowledge and can drift from what the EPG declares. Ad breaks already worked this way (`ad.Path` used verbatim), so this extends the same rule to content streams.

**Alternatives considered**:
- Keep `STORAGE_BUCKET` and keep stripping/re-adding — rejected: two sources of truth for the same path prefix.
- Derive the bucket from the stream id at runtime — rejected: the id already is the path; parsing it back out adds failure modes for no gain.
