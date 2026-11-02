# Implementation Plan: Partner Common XML Source

**Branch**: `013-partner-common-xml` | **Date**: 2026-11-02 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/013-partner-common-xml/spec.md`

## Summary

Expose a reusable partner-facing XML source on the SimLive API: a channel list (`GET /common/channel-playouts-xml`) and a per-channel EPG (`GET /common/channel/epg/:channelId`). The channel list is fetched directly from the upstream partner API configured in `config/partner.yaml` (no local `/api/channels` or `playout_channels` lookup for the list); the Go API no longer serves the channel list through its own channel endpoint for this flow. Both endpoints are thin adapters over the config-driven partner API client, converted to XML by a config-driven builder. A new `config/partner.yaml` becomes the single source of partner configuration, absorbing the auth flow currently in `config/auth.yaml`; the active partner profile also declares optional XML fields that extend the fixed base contract. Interval-based ad insertion is removed from the planner, and ad markers now come only from EPG programmes flagged as ads.

## Technical Context

**Language/Version**: Go 1.26.6 (module `simlive`)

**Primary Dependencies**: Existing only — Gin, GORM/PostgreSQL, go-redis/v9, `gopkg.in/yaml.v3` (already in the module graph), `encoding/xml` from the standard library. No new external dependencies.

**Storage**: PostgreSQL `playout_channels` (channel → EPG id lookup); Redis timeline ZSET (`simlive:timeline:{channel}`); retained EPG cache unchanged. `config/partner.yaml` is the single partner configuration file.

**Testing**: `go test ./...`; new unit tests for partner config loading, XML building, field mapping, date/time rendering, and EPG ad marking; handler tests with `httptest`; planner tests for removal of interval ads; integration verification against the planner + Redis + PostgreSQL + API stack per constitution Principle IV.

**Target Platform**: Linux server (systemd units under `config/system/`) + web browser admin client.

**Project Type**: Web application — Go backend services (`cmd/api`, `cmd/planner`, `cmd/engines`) and a Vue 3 admin client.

**Performance Goals**: Channel list and one-day EPG responses complete in under 5 s under normal upstream conditions (SC-007); XML generation is linear in returned items and adds no new round trips beyond the existing upstream calls.

**Constraints**: No new dependencies; base XML fields (`id`, `name`, `img`; `start`, `stop`, `streamId`, `ads`) are fixed and partner extensions may only add fields; partner selection is one active profile per deployment; all day/time anchoring follows the host-local timezone with `configs.TZ` only as a fallback; interval-based ad insertion is removed globally.

**Scale/Scope**: One active partner profile per deployment; channel lists bounded by the upstream `is_playout_assigned=true` query; EPG bounded to one day per request; two new HTTP routes; no client UI changes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Library-First**: PASS — partner configuration, mapping, and XML building live in a standalone, testable `internal/partner` package; `internal/api` handlers stay thin; `cmd/api` only wires routes. No organizational-only packages are introduced.
- **II. CLI Interface**: PASS (not directly applicable) — the feature adds HTTP endpoints, not a binary; existing binaries keep their CLI surface unchanged.
- **III. Test-First (NON-NEGOTIABLE)**: PASS with obligation — implementation tasks begin with failing tests for config migration, XML base/extended output, field resolution, date/time rendering, 404/path guard behavior, and EPG ad marking; `go test ./...` gates the work.
- **IV. Integration Testing**: PASS with obligation — new package contract tests plus integration checks for API → partner client → PostgreSQL channel lookup → XML, and planner → Redis timeline → HLS ad boundary behavior. The existing `make test` workflow is used.
- **V. Observability & Simplicity**: PASS — configuration and upstream failures are logged through the existing logger; YAGNI applied (one active profile, fixed base XML, declarative extra fields only, no scripting or template engine).

**Post-design re-check**: PASS — Phase 0/1 resolve all technical unknowns without adding dependencies or violating a principle. No entries in Complexity Tracking.

## Project Structure

### Documentation (this feature)

```text
specs/013-partner-common-xml/
├── plan.md              # This file
├── research.md          # Phase 0 decisions
├── data-model.md        # Phase 1 entities and mapping rules
├── quickstart.md        # Phase 1 validation guide
├── contracts/
│   ├── common-xml-api.md     # HTTP + XML contract
│   └── partner-config.md     # partner.yaml schema and examples
└── tasks.md             # /speckit-tasks output
```

### Source Code (repository root)

```text
internal/
├── partner/                      # NEW: library-first partner domain
│   ├── config.go                 # partner.yaml models + loader (active profile, auth, api, xml)
│   ├── config_test.go            # NEW
│   ├── client.go                 # config-driven partner API client (absorb/replace partner_client.go)
│   ├── client_test.go            # NEW
│   ├── catalog.go                # normalized channel + EPG item views and source resolution
│   ├── catalog_test.go           # NEW
│   ├── xml.go                    # base XML + declarative extension builder
│   └── xml_test.go               # NEW
├── api/
│   ├── partner_xml_channels.go   # NEW: GET /common/channel-playouts-xml handler
│   ├── partner_xml_epg.go        # NEW: GET /common/channel/epg/:channelId handler
│   ├── partner_xml_errors.go     # NEW: shared XML error/response helpers
│   ├── partner_xml_test.go       # NEW: httptest channel-list handler tests
│   ├── partner_epg_test.go       # NEW: httptest EPG handler tests
│   ├── partner_client.go         # MOD: keep admin-facing methods; delegate to internal/partner
│   ├── auth_config.go            # MOD: serve auth section from partner.yaml, legacy fallback
│   └── auth_config_test.go       # MOD: partner.yaml cases + legacy fallback
├── planner/
│   ├── planner.go                # MOD: remove loadAdSegments / UseAdEpgAds gating
│   ├── segment_processor.go      # MOD: remove interval ads; mark EPG ad programmes
│   └── segment_processor_test.go # NEW/MOD: EPG ad marking, no interval ads
├── models/
│   └── models.go                 # MOD: deprecate UseAdEpgAds; keep fields for schema compatibility
└── utils/
    └── envs.go                   # MOD: remove AdInsertionInterval; keep PARTNER_API_URL fallback

cmd/api/
└── main.go                       # MOD: wire partner config; mount /common routes; API_ADDR env

config/
├── partner.yaml                  # NEW (gitignored): active profile + API + auth + XML
├── partner.example.yaml          # NEW: documented sample
├── auth.yaml                     # REMOVE from repo (absorbed into partner.yaml)
├── auth.example.yaml             # REPLACE with partner.example.yaml
├── .gitignore                    # MOD: ignore partner.yaml
└── bin/*, system/*               # unchanged

client/
└── public/auth-config.json       # unchanged fallback; server still serves /api/auth/config
```

**Structure Decision**: Single Go backend with library-first packages under `internal/`. Partner configuration/mapping/XML logic is new package `internal/partner`; HTTP wiring stays in `internal/api`; planner behavior changes stay in `internal/planner`. The Vue client is untouched — the existing `/api/auth/config` route keeps its shape.

## Implementation Outline

1. **Config foundation** — `internal/partner/config.go` models and loader; `config/partner.yaml` / `partner.example.yaml`; migrate auth block; update `auth_config.go` and tests. Fallback order: `PARTNER_CONFIG_PATH` → `./config/partner.yaml` → legacy `./config/auth.yaml` (read-only) → built-in default.
2. **Partner client** — config-driven base URL, timeout, headers, endpoint paths, and query templates; raw JSON channel decoding for arbitrary image fields; keep typed methods used by admin routes.
3. **Catalog + mapper** — resolve active-profile field sources (dotted paths), normalize channels and EPG items, primary stream selection, date/time rendering, `$include_metadata` gating.
4. **XML builder** — fixed base structure plus ordered custom fields; `encoding/xml` encoder; XML content type; error XML; deterministic element order.
5. **HTTP handlers/routes** — `/common` group, path guard, channel list sourced from the upstream partner API via `partner.yaml` settings (no `/api/channels` or local `playout_channels` lookup for the list), channel → EPG resolution via `playout_channels`/upstream detail only for the EPG endpoint, 404 vs empty guide, `$include_metadata`, optional date.
6. **Ads removal** — delete interval insertion path and setting; mark EPG ad programmes on timeline segments with stable break identity and SCTE-35 boundaries; keep HLS/PMT behavior consistent; update tests and `.env.example`.
7. **Validation** — `make fmt`, `make vet`, `make test`, `make build`; manual curl checks from `quickstart.md`.

## Complexity Tracking

> No Constitution Check violations. No entries required.
