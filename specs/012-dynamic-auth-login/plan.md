# Implementation Plan: Dynamic Auth Login

**Branch**: `012-dynamic-auth-login` | **Date**: 2026-11-01 | **Spec**: `/specs/012-dynamic-auth-login/spec.md`
**Revision**: R1 (2026-11-02) — flow correctness fixes: preCall timing, verify invocation, back navigation

**Input**: Feature specification from `/specs/012-dynamic-auth-login/spec.md`

## Summary

Replace the hardcoded `client/src/components/LoginForm.vue` two-step login with a config-driven, multi-step auth flow. A new Vue component renders steps dynamically from an `AuthConfig` (JSON) fetched at runtime from a new Go API endpoint `GET /api/auth/config`, which parses a server-side `auth.yaml`. The client resolves Mustache-style placeholders (`{{token}}`, `{{stepN.field}}`) across preCalls and main calls, accumulates tokens in `localStorage` via the existing Pinia auth store, and falls back to a bundled config when the API is unavailable. v1 flow is strictly sequential.

R1 adds three correctness fixes reported against the initial implementation: (1) step `preCalls` execute when a step is entered (so dependent select options are populated before submit), (2) the top-level `verify` block is honored after the final step and on session restore, and (3) users can navigate back to a previous step with form state preserved.

## Revision R1: Flow Correctness (2026-11-02)

Reported issues (from user testing with the 2-step SSO config):
- "preCall does not call when visit step2"
- "verify token does not call"
- "can not switch back prev step"

### R1.1 preCalls run on step entry
- **Problem**: `DynamicAuth.vue` runs `runPreCalls` only inside `submit()`. Select fields whose `options` reference preCall results (`${accounts}`) resolve to `[]` when the step renders, so step 2 shows an empty Account select and cannot be completed; the preCall firing only after submit is too late to populate the UI.
- **Fix**: On entering a step (initial mount, advance, back), run the step's `preCalls` with the accumulated context (`auth.tokens` + prior form values), merge `response_mapping` results into the context, then resolve select options. On preCall failure: abort step entry, show the error, allow retry (retry re-runs preCalls from scratch, per existing spec clarification). Submit re-runs preCalls as before (idempotent GETs make this safe).
- **Affected**: `client/src/components/DynamicAuth.vue`, `client/src/libs/auth-config.ts` (extract a reusable step-entry preCall runner).

### R1.2 Honor the top-level `verify` block
- **Problem**: After the final step, `DynamicAuth.vue` calls `auth.setToken(token)`, which hits a hardcoded `accounts/auth/verify-token` endpoint. The config's top-level `verify` object (`url`, `method`, `headers`, `body`, `clear_on_error`) is never used: the store's `verifyToken()` — which reads `cfg.verify` — is defined but called nowhere. Same gap on session restore: `auth-provider.vue` calls `setToken()` on boot.
- **Fix**:
  - After the final step succeeds, invoke the config's `verify` call (placeholders resolved against the accumulated context) instead of the hardcoded path; persist userInfo/`is_super_admin` as `setToken` does today.
  - On app boot with an existing token, run the same config-driven verify. On failure with `clear_on_error: true` (default), reset auth state and return to the login flow; with `clear_on_error: false`, keep the session (see D9).
  - Reuse the existing `verifyToken()` store action (extended to resolve all verify placeholders — including `body` — via `resolvePlaceholders`, and to honor `clear_on_error`) from both call sites; `setToken` remains the fallback when no `verify` is configured.
  - Sync `client/public/auth-config.json` (bundled fallback) with `config/auth.yaml` so fallback mode includes `verify` and the `select` account field.
- **Affected**: `client/src/components/DynamicAuth.vue`, `client/src/providers/auth-provider.vue`, `client/src/stores/auth.ts`, `client/public/auth-config.json`.

### R1.3 Back navigation between steps
- **Problem**: The flow is forward-only; a user who mis-selects an account on step 2 must restart the whole flow.
- **Fix**: Render a Back button when `stepIndex > 0`. Clicking decrements `stepIndex` and restores that step's previously entered form values (per-step form snapshot kept in component state). Back is pure client-side navigation — no main API calls; the target step's preCalls re-run on entry per R1.1. Final-step submit remains the only path to verify.
- **Affected**: `client/src/components/DynamicAuth.vue`.

### Spec deltas required (apply before `/speckit-tasks`)
- Refine **FR-003**: preCalls MUST execute when a step is entered (not only at submit) and re-execute on retry.
- Add **FR-011**: config MAY declare a top-level `verify` block; the client MUST call it after the final step and when restoring an existing session; on failure honor `clear_on_error` (default `true` → reset and return to login).
- Add **FR-012**: the client MUST support navigating back to prior steps with form values preserved (sequential flow integrity unchanged).
- Edge cases: preCall failure on step entry aborts entry with error + retry; verify failure with `clear_on_error` resets the session.

## Technical Context

**Language/Version**: Go 1.x (module `simlive`); TypeScript + Vue 3 (client)
**Primary Dependencies**: Gin, GORM, go-redis, godotenv (Go); Vue 3, Pinia, Vuetify, axios (client); `gopkg.in/yaml.v3` (Go)
**Storage**: PostgreSQL + Redis (existing, unchanged); browser `localStorage` for accumulated tokens
**Testing**: Go `testing` for backend handler (exists: `internal/api/auth_config_test.go`); client validation manual per `quickstart.md` (client tests optional per tasks.md header)
**Target Platform**: Linux server (Go API on :8080) + browser SPA
**Project Type**: Web application (backend + frontend)
**Performance Goals**: Config endpoint lightweight and cacheable; login flow interactive (<200ms per step excluding SSO)
**Constraints**: Sequential steps only (no conditionals/branching) in v1; Mustache placeholder resolution client-side; JSON (not YAML) over the wire; bundled fallback config
**Scale/Scope**: 1–5 steps per config; single auth config per deployment
**Unknowns**: None remaining — all resolved in `research.md` (D1–D11).

## Constitution Check

Constitution: `.specific/constitution.yaml` v1.0.0 (ratified 2026-11-01).

| Principle | Assessment |
|---|---|
| I. Library-First | PASS — no new Go packages; the Go surface remains the existing `internal/api` handler. R1 changes are client-only. |
| II. CLI Interface | N/A — no new binary; `GET /api/auth/config` is an endpoint of the existing api binary. |
| III. Test-First (NON-NEGOTIABLE) | GATE — Go handler tests exist and pass (`make test`). R1 is client-only; per the tasks.md header, client tests are optional, so R1 validation is via the mandatory quickstart scenarios. Any Go-side change must follow Red-Green-Refactor (tests first). |
| IV. Integration Testing | PASS — extend handler/contract verification so served JSON is validated against `contracts/auth-config.json`, including the `verify` block and `select` field type. |
| V. Observability & Simplicity | PASS — no new dependencies; reuses existing fetch/axios paths and store actions; logging unchanged. |

**Gate evaluation**: No violations. The Test-First gate is satisfied by defining the validation scenarios (quickstart) and tests before the implementation tasks run; ordering is enforced by `/speckit-tasks`.

## Project Structure

### Documentation (this feature)

```text
specs/012-dynamic-auth-login/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
└── contracts/
    └── auth-config.json
```

### Source Code (repository root)

R1 edits existing files only — no new files.

```text
config/auth.yaml                              # source of truth (already has verify + select)
client/public/auth-config.json                # EDIT: sync verify + select field with server yaml
internal/api/auth_config.go                   # unchanged in R1 (verify passes through as JSON)
client/src/components/DynamicAuth.vue         # EDIT: preCall-on-entry, verify call, Back button
client/src/libs/auth-config.ts                # EDIT: reusable step-entry preCall runner
client/src/stores/auth.ts                     # EDIT: verifyToken honors full verify block + clear_on_error
client/src/providers/auth-provider.vue        # EDIT: boot restore uses config-driven verify
```

**Structure Decision**: Web application option. Backend adds one read-only handler + yaml loader (done); frontend replaces `LoginForm.vue` with `DynamicAuth.vue` and a small auth-config client module (done). R1 is corrective client-side work plus fallback-config parity.

## Complexity Tracking

No constitution violations; no complexity justifications required. R1 intentionally reuses existing mechanisms (store actions, fetch, placeholder resolver) instead of adding libraries.
