# Tasks: Partner Common XML Source

**Input**: Design documents from `/specs/013-partner-common-xml/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: Required. Constitution Principle III (Test-First, NON-NEGOTIABLE) mandates failing tests before implementation. Write each test task first and confirm it fails before starting the paired implementation task.

**Organization**: Tasks are grouped by user story so each can be implemented, tested, and verified independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 from spec.md (setup, foundational, and polish tasks have no story label)
- Every task names its exact file path

## Path Conventions

- Go backend packages: `internal/<package>/`
- HTTP wiring: `cmd/api/main.go`
- Deployment/config: `config/`
- Feature docs: `specs/013-partner-common-xml/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish the new configuration surface and repository hygiene before any code depends on it.

- [X] T001 Create `config/partner.example.yaml` with the full schema and placeholders from `contracts/partner-config.md` (`active`, `partners.<id>.api`, `partners.<id>.auth`, `partners.<id>.xml`)
- [X] T002 [P] Update `config/.gitignore` to ignore `partner.yaml` (keep the existing `auth.yaml` line until T035)
- [X] T003 [P] Add `API_ADDR=:8080` to `config/.env` and document the override in `config/specification-server.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Library-first partner domain (config, mapping, XML, client) plus API bind configuration. Every user story depends on this phase.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T004 [P] Write failing tests for `internal/partner/config.go` in `internal/partner/config_test.go`: `PARTNER_CONFIG_PATH` → `./config/partner.yaml` → built-in fallback precedence, unknown `active`, missing file, and profile lookup
- [X] T005 [P] Write failing tests for `internal/partner/xml.go` in `internal/partner/xml_test.go`: deterministic element order, XML escaping, empty elements, and `<error>` output
- [X] T006 [P] Write failing tests for `internal/partner/catalog.go` in `internal/partner/catalog_test.go`: dotted-path resolution, `img` candidate order (`img`, `image`, `logo`, `thumbnail`, `poster`, object → `url`/`src`), primary stream selection (`is_vod && !is_trailer`, or `name == "Default Stream"`), RFC3339 rendering, and host-local fallback
- [X] T007 [P] Write failing tests for `internal/partner/client.go` in `internal/partner/client_test.go`: configurable base URL, timeout, headers, `{channel_id}`/`{epg_id}` placeholders, and query-parameter merging
- [X] T008 Implement `PartnerConfig`, `PartnerProfile`, `PartnerAPI`, `PartnerXML`, `XMLField`, and the loader in `internal/partner/config.go` (make T004 pass)
- [X] T009 Implement the dynamic ordered XML node builder and encoder in `internal/partner/xml.go` (make T005 pass)
- [X] T010 Implement normalized channel/EPG views, source-path resolution, image candidates, stream selection, and time formatting in `internal/partner/catalog.go` (make T006 pass)
- [X] T011 Implement the config-driven upstream client in `internal/partner/client.go` (make T007 pass)
- [X] T012 Refactor `internal/api/partner_client.go` to delegate base URL, timeout, headers, and endpoint paths to `internal/partner` while preserving `GetChannels`, `GetChannelEPGID`, `GetEPGScheduleItems`, and `GetChannelDetailWithEPG` behavior
- [X] T013 Add `APIAddr` to `Settings` in `internal/utils/envs.go` (`API_ADDR`, default `:8080`) and use it for `r.Run(...)` in `cmd/api/main.go` instead of the hardcoded `:8080`

**Checkpoint**: Foundation ready — user stories can start independently.

---

## Phase 3: User Story 1 - Channel list XML (Priority: P1) 🎯 MVP

**Goal**: `GET /common/channel-playouts-xml` returns well-formed XML with `<id>`, `<name>`, `<img>` per channel.

**Independent Test**: With the API running, the endpoint returns 200 + XML whose every `<channel>` contains all three base elements in order, including an empty `<img>` for channels without an image.

### Tests for User Story 1

- [X] T014 [P] [US1] Write failing tests for channel normalization and channel XML in `internal/partner/channel_test.go` (base order, empty image, extra upstream fields addressable, upstream 502 path)
- [X] T015 [P] [US1] Write failing handler tests in `internal/api/partner_xml_test.go` (200 content type `application/xml; charset=utf-8`, empty list, XML error on upstream failure)

### Implementation for User Story 1

- [X] T016 [US1] Add the channel-list fetch and raw `map[string]any` decoding to `internal/partner/client.go`
- [X] T017 [US1] Add shared XML response/error helpers in `internal/api/partner_xml_errors.go` (well-formed `<error><code>…</code><message>…</message></error>`)
- [X] T018 [US1] Implement the channel list handler in `internal/api/partner_xml_channels.go` using the active profile, base fields, and empty-image rule
- [X] T019 [US1] Mount `GET /common/channel-playouts-xml` (and create the `/common` group) in `cmd/api/main.go`; log configuration/upstream failures without leaking payloads
- [X] T020 [US1] Verify channel list XML via skill `start api`: run `make start-api`, then `curl -s http://localhost:8080/common/channel-playouts-xml` and confirm 200, `application/xml`, and every `<channel>` has `<id>`, `<name>`, `<img>` (empty allowed)

**Checkpoint**: US1 is independently deployable as the MVP.

---

## Phase 4: User Story 2 - EPG XML (Priority: P1)

**Goal**: `GET /common/channel/epg/:channelId` returns one day of XML `<epgitem>` entries with `<start>`, `<stop>`, `<streamId>`, `<ads>` and metadata gated by `$include_metadata`.

**Independent Test**: For a known channel, the endpoint returns 200 + XML items with all four base elements; metadata-only fields appear only with `$include_metadata=true`; unknown channels return 404 + `<error>`.

### Tests for User Story 2

- [X] T021 [P] [US2] Write failing EPG normalization tests in `internal/partner/epg_test.go` (date parsing `YYYY-MM-DD`, RFC3339 with channel offset, omitted item when no primary stream, `ads` lowercase, metadata gating)
- [X] T022 [P] [US2] Write failing handler tests in `internal/api/partner_epg_test.go` (200 guide, empty guide, 404 unknown channel, 400 `..`//`/` path, 400 invalid date, 502 upstream)

### Implementation for User Story 2

- [X] T023 [US2] Add the schedule fetch (`{epg_id}`, `$date`, `tz`) and channel-detail fallback to `internal/partner/client.go`
- [X] T024 [US2] Implement channel → guide-id resolution in `internal/api/partner_xml_epg.go`: local `playout_channels.channel_epg_id` first, upstream detail second, 404 when neither resolves
- [X] T025 [US2] Implement date selection (`date`/`$date`, default host-local today via `utils.LocalLocation()`), schedule retrieval, item mapping, and `$include_metadata` gating in `internal/api/partner_xml_epg.go`
- [X] T026 [US2] Implement 400/404/502 responses, path guard (`..`, `/`), and the empty-guide case in `internal/api/partner_xml_epg.go`
- [X] T027 [US2] Mount `GET /common/channel/epg/:channelId` in `cmd/api/main.go`
- [X] T028 [US2] Verify EPG XML via skill `start api`: run `make start-api`, then `curl -s "http://localhost:8080/common/channel/epg/<channelId>"` and `curl -s "http://localhost:8080/common/channel/epg/<channelId>?\$include_metadata=true"`; confirm 200, four base elements per item, and metadata only in the second response

**Checkpoint**: US1 and US2 both work independently and together.

---

## Phase 5: User Story 3 - Config-only partner onboarding (Priority: P2)

**Goal**: Adding a partner (or extra XML fields) requires editing only `config/partner.yaml`; base elements stay fixed.

**Independent Test**: Add a custom channel field and a metadata-only EPG field to the active profile, re-request both endpoints without restarting, and confirm the extra elements appear while base elements are unchanged.

### Tests for User Story 3

- [X] T029 [P] [US3] Write failing custom-field validation tests in `internal/partner/extension_test.go` (valid name, reserved-name collision against `id`, `name`, `img`, `start`, `stop`, `streamId`, `ads`, `channelId`; exactly one of `source`/`value`; `when` is `always` or `metadata`)
- [X] T030 [P] [US3] Write failing XML extension tests in `internal/partner/xml_extension_test.go` (ordered custom fields, literal `value`, `default`, metadata gating, invalid field skipped without breaking base XML)

### Implementation for User Story 3

- [X] T031 [US3] Implement custom-field validation, reserved-name checks, and ordered parsing in `internal/partner/config.go`
- [X] T032 [US3] Implement custom-field emission (source lookup, literal value, default, `when` gating) in `internal/partner/xml.go`
- [X] T033 [US3] Log skipped invalid fields and serve base XML on config errors in `internal/api/partner_xml_errors.go` (or the calling handlers)
- [X] T034 [US3] Extend `config/partner.example.yaml` with a documented custom-field example for channel and EPG
- [X] T035 [US3] Verify config-only onboarding via skill `start api`: edit the active profile in `config/partner.yaml` to add `<provider>` (literal) and an EPG `title` (`when: metadata`), run `make start-api`, and confirm `/common/channel-playouts-xml` shows `<provider>` and the EPG shows `<title>` only with `$include_metadata=true`

**Checkpoint**: A new partner is onboarded without code changes.

---

## Phase 6: User Story 4 - Consolidated partner.yaml / auth migration (Priority: P2)

**Goal**: `config/partner.yaml` holds the auth flow; `config/auth.yaml` is no longer required, and `GET /api/auth/config` keeps its existing shape.

**Independent Test**: Remove `config/auth.yaml`, start with an equivalent `partner.yaml`, and confirm `/api/auth/config` returns the same `verify`/`steps` JSON; then remove `partner.yaml` and confirm the fallback still serves a usable login flow.

### Tests for User Story 4

- [X] T036 [P] [US4] Update failing auth loader tests in `internal/api/auth_config_test.go` for: active-profile `auth` block served, missing `partner.yaml` → legacy `config/auth.yaml`, invalid YAML → built-in default, and unusable auth block (no `steps`)

### Implementation for User Story 4

- [X] T037 [US4] Move the existing `verify` and `steps` blocks into `config/partner.example.yaml` under `partners.<id>.auth`; delete `config/auth.yaml` and `config/auth.example.yaml`; remove the `auth.yaml` ignore line and keep `partner.yaml` in `config/.gitignore`
- [X] T038 [US4] Update `internal/api/auth_config.go` to load the active profile's `auth` block, keep `AUTH_CONFIG_PATH` as an override, and fall back to legacy `config/auth.yaml` then the built-in default while logging degradations
- [X] T039 [US4] Update `config/DEPLOY.md` with partner.yaml setup, auth migration steps, and `PARTNER_CONFIG_PATH`/`API_ADDR` notes
- [X] T040 [US4] Verify auth migration via skill `start api`: run `make start-api` with `config/auth.yaml` removed and `partner.yaml` present, `curl -s http://localhost:8080/api/auth/config` to confirm `verify`/`steps`; temporarily rename `partner.yaml` and confirm the fallback login flow is still returned

**Checkpoint**: One configuration file drives both the common XML endpoints and the client auth flow.

---

## Phase 7: User Story 5 - EPG-driven ads, no interval default (Priority: P2)

**Goal**: Interval-based ad insertion is gone; timeline/HLS/XML ads come only from EPG programmes with `programme.is_ads = true`.

**Independent Test**: Prepare a channel whose EPG has explicit ad programmes and confirm only those programmes get ad markers, with stable break fields and SCTE-35 boundaries; a channel with no ad programmes produces no ads regardless of elapsed time.

### Tests for User Story 5

- [X] T041 [P] [US5] Write failing planner tests in `internal/planner/segment_processor_test.go`: EPG ad programme sets `IsAd=true`, `AdBreakID`, `AdBreakStart`, `AdBreakDuration`, `AdType=out` on first / `in` on last, and splice inserts; no ad appears without an EPG ad programme regardless of elapsed time

### Implementation for User Story 5

- [X] T042 [US5] Remove interval ad state and functions (`insertAdsIfNeeded`, `processingVerifyNearingAds`, `adSegments`, `lastAdTime`, `isBlockingProcessing`) from `internal/planner/segment_processor.go`
- [X] T043 [US5] Remove `loadAdSegments` and the `UseAdEpgAds` gating from `internal/planner/planner.go`; remove the `UseAdEpgAds` change-detection branch from `cmd/engines/main.go`
- [X] T044 [US5] Mark EPG ad programmes in `internal/planner/segment_processor.go`: stable break id from start time, `AdBreakStart`/`AdBreakDuration` from programme start/end, `out`/`in` boundaries, and pre-roll cues on the preceding content segment when available
- [X] T045 [US5] Remove `AdInsertionInterval` from `internal/utils/envs.go` and remove `AD_INSERTION_INTERVAL` from `.env.example` and `config/.env`
- [X] T046 [US5] Mark `UseAdEpgAds` deprecated in `internal/models/models.go` (keep the column/field for schema and API compatibility; planner ignores it)
- [X] T047 [US5] Verify EPG-only ads via skill `start planner`: run `make start-planner`, prepare a channel with EPG ad programmes, then check the timeline (`curl -s "http://localhost:8080/api/channels/<channelId>/segment-track?limit=200"`) for ad markers only on EPG ad items; use skill `db query` (`docker exec simlive_postgres psql -U postgres -d simlive -c "select id,channel_epg_id from playout_channels where id='<channelId>'"`) to confirm the channel/guide mapping used

**Checkpoint**: All user stories are independently functional.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Full validation and documentation.

- [X] T048 [P] Run `make fmt` and `make vet`; fix all findings in changed Go files
- [X] T049 [P] Run `make test` and `make build`; fix failures before proceeding
- [X] T050 Update `config/specification-server.md` with the `/common/channel-playouts-xml` and `/common/channel/epg/:channelId` endpoints, `API_ADDR`, and `partner.yaml`
- [X] T051 Validate `specs/013-partner-common-xml/quickstart.md` end-to-end (config migration, both endpoints, metadata, 404/400, custom field, auth fallback, ads check) and record any deviations
- [X] T052 [P] Confirm `client/public/auth-config.json` still matches the fallback contract and that no client rebuild is required for US3/US4

---

## Phase 9: Extension - Bucket-inclusive `streamId` (STORAGE_BUCKET removal)

**Purpose**: The XML EPG source returns `stream_id` values that already include the storage bucket (rclone `remote:bucket/...` form, same as inline ad `path` values). Use them verbatim so `STORAGE_BUCKET`/`ADS_STREAM_ID` configuration is no longer needed (FR-019, research D12).

- [X] T053 [P] Write failing tests: partner client XML parsing keeps the bucket prefix in `stream_id` even when `STORAGE_BUCKET` is set; planner builds `StoragePath` as `<streamId>/{rendition}/<file>` without prepending a bucket
- [X] T054 Remove the `STORAGE_BUCKET` prefix strip from `GetEPGScheduleItemsFromXML` in `internal/api/partner_client.go`
- [X] T055 Use the bucket-inclusive stream id directly in `internal/planner/segment_processor.go`; drop `bucketName`/`adsBucket` from `Planner` in `internal/planner/planner.go`
- [X] T056 Remove `VideoBucket`/`AdsStreamId` from `internal/utils/envs.go`; remove `STORAGE_BUCKET`/`ADS_STREAM_ID` from `.env.example` and `config/.env`
- [X] T057 Extend `specs/013-partner-common-xml/` docs (spec, data-model, contracts, research, plan) with the bucket-inclusive `streamId` contract
- [X] T058 Verify `make fmt`, `make vet`, `make test`, `make build`; record the change in `LOGS.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Setup; BLOCKS all user stories.
- **US1 (Phase 3)**: Starts after Phase 2.
- **US2 (Phase 4)**: Starts after Phase 2. Shares the partner config/catalog/XML library with US1 but uses separate handler/test files; can run in parallel after T011.
- **US3 (Phase 5)**: Starts after US1 and US2 because it modifies the shared `internal/partner/config.go` and `internal/partner/xml.go`.
- **US4 (Phase 6)**: Starts after US3 because both edit `config/partner.example.yaml`; otherwise independent of US1/US2.
- **US5 (Phase 7)**: Starts after Phase 2 and is independent of US1–US4 (planner/utils/models/engines files); can run in parallel.
- **Polish (Phase 8)**: Depends on all desired stories.

### User Story Dependencies

- **US1 (P1)**: No story dependencies — MVP.
- **US2 (P1)**: No story dependencies; recommended after US1 for a single developer.
- **US3 (P2)**: Depends on US1 + US2 (shared config/XML library).
- **US4 (P2)**: Depends on US3 (shared config example file); auth loader work itself is independent.
- **US5 (P2)**: Independent of all XML stories.

### Within Each User Story

- Test tasks MUST be written and FAIL before the paired implementation tasks.
- Foundational config → catalog → client → handler → route → verification.
- A story is complete only after its verification task passes.

### Parallel Opportunities

- T002/T003 (setup), T004–T007 (foundational tests) can run in parallel.
- After T011, T014/T015 (US1) and T021/T022 (US2) can run in parallel.
- US5 (T041–T047) can run in parallel with US1–US4.
- T048/T049 and T052 can run in parallel during polish.

---

## Parallel Example: User Story 1

```bash
# Tests first (different files):
Task: "T014 Write failing tests for channel normalization and channel XML in internal/partner/channel_test.go"
Task: "T015 Write failing handler tests in internal/api/partner_xml_test.go"

# After tests fail, implement:
Task: "T016 Add channel-list fetch in internal/partner/client.go"
Task: "T017 Add shared XML error helpers in internal/api/partner_xml_errors.go"
```

## Parallel Example: US1 and US2 after foundation

```bash
# Developer A (US1):
Task: "T018 Implement channel list handler in internal/api/partner_xml_channels.go"
Task: "T019 Mount GET /common/channel-playouts-xml in cmd/api/main.go"

# Developer B (US2):
Task: "T024 Implement channel → guide-id resolution in internal/api/partner_xml_epg.go"
Task: "T027 Mount GET /common/channel/epg/:channelId in cmd/api/main.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 and Phase 2.
2. Complete US1 (T014–T020).
3. **STOP and VALIDATE**: `make start-api` + `curl /common/channel-playouts-xml`; parse the XML and confirm all three base fields.
4. Deploy/demo the channel list as the first partner-facing source.

### Incremental Delivery

1. Setup + Foundational → partner config/library and API bind ready.
2. US1 → verify channel list → deploy (MVP).
3. US2 → verify EPG (base + metadata) → deploy.
4. US3 → verify config-only custom fields → deploy.
5. US4 → verify auth served from `partner.yaml` after removing `auth.yaml` → deploy.
6. US5 → verify EPG-only ads in timeline/HLS/XML → deploy.
7. Polish: `make fmt`, `make vet`, `make test`, `make build`, quickstart validation.

### Parallel Team Strategy

- Team completes Phase 1 + Phase 2 together.
- Then: Developer A takes US1, Developer B takes US2, Developer C takes US5.
- Bring in US3 after US1/US2; US4 after US3 (shared `partner.example.yaml`).

---

## Notes

- [P] tasks touch different files and have no incomplete dependency.
- Reserved base names for custom fields: `id`, `name`, `img`, `start`, `stop`, `streamId`, `ads`, `channelId`.
- `$include_metadata` accepts `true`/`1`; any other value excludes `when: metadata` fields.
- Path guard: reject `..` and `/` in `:channelId` before any lookup.
- Empty valid result is 200 with an empty root; unknown channel is 404; upstream failure is 502.
- Commit after each task or logical group; keep `config/partner.yaml` out of commits.
