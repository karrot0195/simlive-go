# Tasks: Dynamic Auth Login

**Input**: Design documents from `/specs/012-dynamic-auth-login/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Go unit test for the config handler included in polish phase (extended in R1 for the `verify` block + contract validation); client tests optional.

**Organization**: Tasks grouped by user story (US1, US2, US3) for independent implementation. Revision R1 (2026-11-02) adds flow-correctness tasks: preCall-on-entry (plan R1.1), config-driven verify (R1.2), back navigation (R1.3). Tasks T001–T028-equivalent from R0 are complete and checked.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [X] T001 Add `gopkg.in/yaml.v3` dependency and run `go mod tidy` in `go.mod`
- [X] T002 [P] Create server-side auth config at `config/auth.yaml` with a sample 2-step flow (login + select account, incl. top-level `verify` block)
- [X] T003 [P] Create bundled fallback config at `client/public/auth-config.json` (JSON mirror of auth.yaml)
- [X] T004 [P] Create placeholder resolver + fetch module skeleton at `client/src/libs/auth-config.ts`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before any user story

- [X] T005 Create Go auth config loader and handler in `internal/api/auth_config.go` (read `AUTH_CONFIG_PATH` or `./config/auth.yaml`, parse YAML, serve JSON, built-in default fallback)
- [X] T006 Register `GET /api/auth/config` route in `cmd/api/main.go`
- [X] T007 [P] Extend Pinia auth store in `client/src/stores/auth.ts` to accumulate `tokens` map in `localStorage` and support reset
- [X] T008 [P] Create `DynamicAuth.vue` component skeleton in `client/src/components/DynamicAuth.vue`
- [X] T009 Update `client/src/providers/auth-provider.vue` to render `DynamicAuth` instead of `LoginForm`

**Checkpoint**: Foundation ready.

---

## Phase 3: User Story 1 - Dynamic multi-step login (Priority: P1) 🎯 MVP

**Goal**: Render and execute a multi-step login flow driven by auth config — including preCalls on step entry, config-driven token verification, and back navigation (R1).

**Independent Test**: Load client with a 2-step auth config; step 2's preCall (accounts fetch) fires on entry and populates the select; complete both steps; verify call fires with resolved `{{token}}`; Back returns to step 1 with values intact.

### Implementation for User Story 1 (R0 — done)

- [X] T010 [US1] Implement step rendering and field binding in `client/src/components/DynamicAuth.vue`
- [X] T011 [US1] Implement preCall execution and Mustache placeholder resolution in `client/src/libs/auth-config.ts`
- [X] T012 [US1] Implement main API call, response_field token extraction, and accumulation in auth store
- [X] T013 [US1] Implement retry (re-run preCalls + main call) and error display in `client/src/components/DynamicAuth.vue`
- [X] T014 [US1] Implement display-only step (empty fields) with continue button in `client/src/components/DynamicAuth.vue`
- [X] T015 [US1] Verify via skill `build client`: run `make build-client` (expect success) and `start api` (`make start-api`) with manual 2-step flow reaching authenticated state

### Implementation for User Story 1 (R1 — flow correctness)

- [X] T016 [US1] Add step-entry orchestration in `client/src/components/DynamicAuth.vue`: new `enterStep(index)` that (a) initializes form from per-step snapshot or field `default`, (b) runs the step's `preCalls` via `runPreCalls` with ctx = `{...auth.tokens, token: auth.token, ...prior step form values}` (per data-model.md: "options must be resolved from preCall/context **after preCalls run**"), (c) resolves `select` options from the enriched ctx, (d) on preCall failure aborts entry, shows the error, and offers retry (retry re-runs preCalls from scratch per research D4); call `enterStep` from `onMounted`, after submit-advance, and from Back; remove the preCalls/options logic currently inline in `submit()`
- [X] T017 [P] [US1] Extend `verifyToken()` in `client/src/stores/auth.ts`: resolve **all** `verify.headers` and `verify.body` placeholders via `resolvePlaceholders` (not just `Authorization` string replace), honor `response_ok` JSON path (default success = HTTP 2xx), and honor `clear_on_error` (default `true` → `reset()` + return to login flow; `false` → keep session); keep `setToken` as fallback when config has no `verify` block
- [X] T018 [US1] In `client/src/components/DynamicAuth.vue` `submit()` final-step branch: replace `auth.setToken(auth.token)` with `await auth.verifyToken()` (config-driven verify per research D9); show error and stay on final step when verification fails
- [X] T019 [US1] In `client/src/providers/auth-provider.vue` `onMounted`: replace `setToken(token)` with `verifyToken()` so session restore re-verifies the existing token against the config `verify` block (clear_on_error governs reset on failure)
- [X] T020 [US1] Add Back navigation in `client/src/components/DynamicAuth.vue`: render a Back button (Vuetify/espx styling per US2) when `stepIndex > 0`; on click store current form into the per-step `forms` snapshot map, decrement `stepIndex`, and call `enterStep` (restores prior values, re-runs target step preCalls per research D10; no main API call on Back)

### Verification for User Story 1

- [X] T021 [US1] Verify R1 flow via skills `build client` and `start api`: run `make build-client` (expect success), then `make start-api`; manually validate quickstart scenarios R1-SC1 (accounts GET fires on entering step 2, select populated), R1-SC2 (verify POST with `Authorization: Bearer {{token}}` after final step), R1-SC3 (reload with existing token re-fires verify; failure with `clear_on_error: true` resets to login), R1-SC4 (Back restores step 1 values; Continue re-runs step 1 and returns to step 2)

**Checkpoint**: US1 fully functional and independently testable, incl. R1 corrections.

---

## Phase 4: User Story 2 - Remove hardcoded login page, use espx UI component (Priority: P1)

**Goal**: Replace LoginForm with espx-styled dynamic auth component.

**Independent Test**: Unauthenticated user sees DynamicAuth (not LoginForm); styling matches espx components.

### Implementation for User Story 2

- [X] T022 [US2] Restyle `DynamicAuth.vue` with Vuetify/espx styling consistent with other components
- [X] T023 [US2] Remove `client/src/components/LoginForm.vue` and any references
- [X] T024 [US2] Verify `auth-provider.vue` no longer references LoginForm

### Verification for User Story 2

- [X] T025 [US2] Verify via skill `build client`: run `make build-client` (expect success, no LoginForm references)

**Checkpoint**: US1 + US2 both work.

---

## Phase 5: User Story 3 - Auth config delivered from Go API (Priority: P2)

**Goal**: Client fetches auth config from Go API at runtime with bundled fallback that mirrors the server config (R1 parity).

**Independent Test**: `GET /api/auth/config` returns valid config incl. `verify`; client fetches and applies it; config change on server reflected after reload; fallback mode behaves identically.

### Implementation for User Story 3 (R0 — done)

- [X] T026 [US3] Implement client fetch of `/api/auth/config` at runtime in `client/src/libs/auth-config.ts`
- [X] T027 [US3] Implement fallback to bundled `public/auth-config.json` when API unavailable or invalid

### Implementation for User Story 3 (R1 — fallback parity)

- [X] T028 [P] [US3] Sync bundled fallback `client/public/auth-config.json` with `config/auth.yaml` (research D11): add top-level `verify` block (url/method/headers/clear_on_error) and change step-2 `accountId` field to `type: select` with `options: "${accounts}"` so fallback mode matches server-served behavior

### Verification for User Story 3

- [X] T029 [US3] Verify via skills `start api` and `build client`: run `make start-api`, then `curl http://localhost:8080/api/auth/config | jq` — response matches `contracts/auth-config.json` and includes the `verify` block and `select` account field; stop the API and confirm the client still renders the full 2-step flow with verify from the bundled fallback (quickstart R1-SC5)

**Checkpoint**: All stories functional.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Hardening, validation, and doc/spec sync

- [X] T030 [P] Add Go unit test for auth config handler in `internal/api/auth_config_test.go`
- [X] T031 Documentation updates (verify `quickstart.md` accuracy)
- [X] T032 Run quickstart.md validation end-to-end
- [X] T033 [P] Extend Go handler test in `internal/api/auth_config_test.go`: serve a temp yaml containing a `verify` block and a `select` field step, assert the JSON response validates against `contracts/auth-config.json` (verify required field `url`, `clear_on_error` default true; field type enum includes `select`)
- [X] T034 [P] Sync `specs/012-dynamic-auth-login/spec.md` with R1 deltas listed in plan.md: refine FR-003 (preCalls execute on step entry and re-execute on retry), add FR-011 (top-level `verify` block: invoked after final step and on session restore; `clear_on_error` default true resets to login), add FR-012 (back navigation with preserved form values); add matching edge cases
- [X] T035 Validate `quickstart.md` R1 scenarios R1-SC1..R1-SC5 against the implementation (preCall-on-entry, verify after final step, verify on restore, back nav, fallback parity)
- [X] T036 Run quickstart.md validation end-to-end incl. all R1 scenarios (`make build`, `make build-client`, `make start-api`, manual flow)

---

## Phase 7: Convergence

- [X] T037 Implement field validation rules (pattern, min/max, custom) declared via step.validation in DynamicAuth.vue per FR-006 (partial)
- [X] T038 Run `/speckit-converge` after R1 tasks (T016–T036) complete to verify the codebase matches spec/plan/tasks and capture any remaining gaps as new tasks

---

## Dependencies & Execution Order

- Setup (Phase 1) → Foundational (Phase 2) → User Stories (Phase 3+) → Polish (Phase 6) → Convergence (Phase 7)
- R1 within US1: T016 (enterStep) → T018 (final-step verify call) and T020 (Back, reuses enterStep); T017 (store verifyToken) → T018, T019
- US3 R1: T028 (fallback sync) before T029 verification
- Polish: T033–T035 parallelizable; T036 after all implementation; T038 last

## Parallel Opportunities

- T016 (DynamicAuth.vue) with T017 (stores/auth.ts) — different files
- T028 (client/public/auth-config.json), T033 (internal/api test), T034 (spec.md), T035 (quickstart validation prep) — all different files
- R0 parallel sets (T002–T004, T007–T008) already executed

## Implementation Strategy

### MVP First (US1)
Complete Phases 1–3 (incl. R1 tasks T016–T021), validate 2-step flow with preCall-on-entry + verify + back nav.

### Incremental
Add US2 (already done), then US3 + fallback parity (T028–T029), then polish (T033–T036), then convergence (T038).

### R1 Focus (bug fixes)
T016 preCall-on-entry, T017–T019 config-driven verify, T020 back navigation, T028 fallback parity — each independently verifiable via quickstart R1 scenarios.

## Phase 8: Convergence

- [ ] T039 Remove the hardcoded verify endpoint from the no-verify fallback path in `client/src/stores/auth.ts` `setToken` (currently POSTs `accounts/auth/verify-token`), so 100% of auth API calls are config-driven per SC-003 — either route the fallback through the config's `verify` block or drop the hardcoded call (partial, LOW, SC-003)
- [ ] T040 Fix dev-mode silent config fallback: without `VITE_API`, the Vite dev server answers `/api/auth/config` with its SPA index.html (observed 200 text/html during E2E), so the dev client silently uses the bundled config — document `VITE_API` in `specs/012-dynamic-auth-login/quickstart.md` dev setup and/or add a Vite dev proxy for `/api` to `http://localhost:8080` (partial, LOW, FR-008)
- [ ] T041 Reconcile `AuthField` option-mapping properties (`optionValueField`, `optionLabelField`, `itemValue`, `itemTitle`, `valueField`, `labelField` in `client/src/libs/auth-config.ts`) with `specs/012-dynamic-auth-login/contracts/auth-config.json` — either document them in the contract or trim to the contract fields (unrequested, LOW, contracts/auth-config.json)
