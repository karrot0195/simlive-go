# Feature Specification: Dynamic Auth Login

**Feature Branch**: `012-dynamic-auth-login`

**Created**: 2026-11-01

**Status**: Draft

**Input**: User description: "support dynamic auth login — remove current login PAGE, use via espx ui component; allow flexible client logic defined by auth.yaml; support multiple steps via yaml; Ui follows yaml; YAML can be received from API GO"

## Clarifications

### Session 2026-11-01

- Q: How should auth.yaml reference values from prior steps (tokens, preCall results) in later steps' headers, URLs, or bodies? → A: Mustache-style placeholders like {{token}} or {{step1.response_field}}, resolved by the client before each call.
- Q: Where should the client persist accumulated tokens/credentials across login steps? → A: localStorage, so tokens survive page reloads mid-flow.
- Q: Should auth.yaml support conditional steps (skip/branch based on prior results) in v1, or is a strictly sequential flow sufficient? → A: Strictly sequential steps only for v1; no conditionals.
- Q: On a failed step, when the user retries, should the step's preCalls re-execute as well? → A: Yes — retry re-runs preCalls and the main API call from scratch.
- Q: Should the Go API serve the auth config as parsed JSON or as raw YAML text? → A: Parsed JSON from a server-side auth.yaml, so the client needs no YAML parser.
- Q: Does the espx UI library provide a reusable auth/login form component, or should DynamicAuth be built on Vuetify following espx styling? → A: No espx auth component; build on Vuetify following espx styling.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Dynamic multi-step login driven by auth config (Priority: P1)

As an admin user, I want the login experience to be driven by a declarative auth configuration (auth.yaml) so that the steps, fields, and API calls can change without rebuilding the client.

**Why this priority**: Replaces the hardcoded login page with a configurable flow; core value of the feature.

**Independent Test**: Load the client with a sample auth.yaml defining two steps (login + account verify); complete both steps and reach the authenticated state.

**Acceptance Scenarios**:

1. **Given** a valid auth.yaml with N steps, **When** the client loads, **Then** it renders step 1's fields and progresses through each step in order.
2. **Given** step 1 succeeds and returns a token, **When** moving to step 2, **Then** the preCall/API calls use the token from the prior step.
3. **Given** any step fails, **When** the API returns an error, **Then** the user sees an error message and can retry the current step.

### User Story 2 - Remove hardcoded login page, use espx UI component (Priority: P1)

As a developer, I want the existing LoginForm.vue replaced by a reusable espx UI auth component driven by the yaml config.

**Why this priority**: Removes duplicated login UI; aligns with espx design system.

**Independent Test**: The old LoginForm.vue is no longer the entry point; AuthProvider renders the espx-based dynamic auth component.

**Acceptance Scenarios**:

1. **Given** an unauthenticated user, **When** the app loads, **Then** the dynamic auth component (not the old LoginForm) is shown.
2. **Given** the espx component, **When** rendered, **Then** it matches the styling of other espx components.

### User Story 3 - Auth config delivered from Go API (Priority: P2)

As an operator, I want the Go API to serve the auth.yaml (or its parsed form) so the client can fetch it at runtime instead of bundling it.

**Why this priority**: Enables server-side control of auth flow without client redeploy.

**Independent Test**: GET the auth config endpoint returns a valid config; client fetches and applies it.

**Acceptance Scenarios**:

1. **Given** the Go API is running, **When** the client requests the auth config, **Then** it receives a valid multi-step definition.
2. **Given** the config changes on the server, **When** the client reloads, **Then** it uses the new config.

### Edge Cases

- What happens when auth.yaml is missing or invalid? Client falls back to a default single-step login or shows an error.
- How does system handle a step with no fields (display-only)? Render continue button.
- What happens when a preCall fails? Abort the step and show error.
- What happens when a preCall fails on step entry? Step entry aborts with an error; retry re-runs preCalls from scratch.
- What happens when the verify call fails? Honor `clear_on_error`: `true` (default) resets the session and returns to login; `false` keeps the session.
- On a failed step, retry re-runs preCalls and the main API call from scratch.
- How does token storage work across steps? Accumulate tokens in localStorage (per FR-009).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST support an auth configuration (auth.yaml) defining an ordered list of steps.
- **FR-002**: Each step MUST declare a title, a list of fields (name, type, label, required), an API endpoint, and a response_field indicating which response value becomes the token/credential for subsequent steps.
- **FR-003**: Each step MAY declare preCall(s): prior HTTP calls (url, method, headers, body) executed before the main API call, whose results can be referenced via Mustache-style placeholders (`{{token}}`, `{{stepN.field}}`) resolved client-side before each call. PreCalls MUST also execute when the step is entered (before dependent UI such as select options renders), and re-execute on retry.
- **FR-004**: The client MUST render UI dynamically from the step definition (fields, labels, buttons) without hardcoded login form.
- **FR-005**: The client MUST remove the existing hardcoded LoginForm.vue login page and use a new Vuetify-based auth component following espx styling.
- **FR-006**: The client MUST allow flexible logic: field validation rules and token passing between steps as defined in yaml. Steps are strictly sequential in v1 (no conditional/branching steps).
- **FR-007**: The Go API MUST expose an endpoint serving the auth configuration as parsed JSON (server reads auth.yaml and returns JSON) to the client.
- **FR-008**: The client MUST fetch the auth config (JSON) from the Go API at runtime, with a bundled fallback auth config if the API is unavailable.
- **FR-009**: The auth store MUST accumulate and persist credentials/tokens across steps in localStorage and support reset.
- **FR-010**: All API calls in steps MUST support configurable headers, including Authorization from prior step tokens.
- **FR-011**: The config MAY declare a top-level `verify` block (url, method, headers, body, response_ok, clear_on_error). The client MUST call it after the final step succeeds and when restoring an existing session on boot; on failure it MUST honor `clear_on_error` (default `true`: reset auth state and return to the login flow; `false`: keep the session).
- **FR-012**: The client MUST allow navigating back to a previous step, restoring that step's previously entered values, without re-submitting any step (sequential flow integrity preserved).

### Key Entities

- **AuthConfig**: top-level config with list of steps.
- **AuthStep**: title, fields[], api, method, response_field, preCalls[], validation rules.
- **AuthField**: name, type, label, required, default.
- **PreCall**: url, method, headers, body, response_mapping.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer can add a new login step by editing auth.yaml only, with no client code changes.
- **SC-002**: The client renders the login flow correctly for configs with 1 to 5 steps.
- **SC-003**: 100% of auth API calls are configurable via yaml (no hardcoded endpoints in client auth code).
- **SC-004**: Token is successfully passed between steps in a 2-step flow in manual testing.

## Assumptions

- espx UI library does not provide a reusable auth component; DynamicAuth is built on Vuetify following espx styling.
- The SSO service (port 3030) remains the underlying identity provider; yaml steps call SSO endpoints.
- Auth config is non-secret (endpoints/field definitions only); secrets remain in server .env.
- v1 flow is strictly sequential: no conditional or branching steps.
- Backward compatibility: if no auth config available, client falls back to current behavior.