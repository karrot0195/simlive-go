# Data Model: Dynamic Auth Login

## Entities

### AuthConfig
Top-level config served by `GET /api/auth/config`.

| Field | Type | Required | Description |
|---|---|---|---|
| `steps` | `AuthStep[]` | yes | Ordered list of steps (1–5). |
| `verify` | `AuthVerify` | no | Post-flow token verification call (see D9). |
| `style` | object | no | Global UI styling defaults. |

### AuthStep
One screen/API exchange in the flow.

| Field | Type | Required | Description |
|---|---|---|---|
| `title` | string | yes | Step heading shown to user. |
| `fields` | `AuthField[]` | yes (may be empty) | Form fields rendered; empty => display-only step with a continue button. |
| `api` | string | yes | Main API URL (supports `{{...}}` placeholders). |
| `method` | string | yes | HTTP method (`GET`/`POST`/...). |
| `response_field` | string | yes | JSON path (dot notation) in the response whose value becomes the accumulated token for subsequent steps. |
| `headers` | `map[string]string` | no | Configurable headers (supports placeholders). |
| `body` | object/string | no | Request body template (supports placeholders). |
| `preCalls` | `PreCall[]` | no | Calls executed **on step entry and before `api`**; results available via placeholders. |
| `validation` | object | no | Field validation rules (required, pattern, min/max). |
| `style` | object | no | UI styling (card, title, button) applied to step/component. |

### AuthField
| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Field key submitted in body. |
| `type` | string | yes | `text`/`password`/`email`/`select`. |
| `label` | string | yes | Display label. |
| `required` | bool | no (default false) | |
| `default` | string | no | Prefill value. |
| `options` | string/array | no | Static array or reference `${key}` resolved from preCall/context results (must run preCalls before resolving). |
| `optionValueField` / `optionLabelField` | string | no | Object fields used for select value/label (fallbacks: `id`/`value`/`accountId`/`name` and `name`/`label`/`title`/`email`). |

### PreCall
| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | Call URL (supports placeholders). |
| `method` | string | yes | HTTP method. |
| `headers` | `map[string]string` | no | Supports placeholders. |
| `body` | object/string | no | Supports placeholders. |
| `response_mapping` | `map[string]string` | no | JSONPath-ish (`$.path`) extraction into context keys. |

### AuthVerify
| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | Verification endpoint (e.g. `https://api2.espx.cloud/api/v2/accounts/auth/verify-token`). |
| `method` | string | no (default POST) | HTTP method. |
| `headers` | `map[string]string` | no | Headers; supports placeholders (`{{token}}`). |
| `body` | object/string | no | Request body template; supports placeholders. |
| `response_ok` | string | no | JSON path indicating success (default: HTTP 2xx). |
| `clear_on_error` | bool | no (default true) | On verify failure: `true` → reset auth state and return to login; `false` → keep session. |

## Client Runtime State (Pinia auth store)

| Key | Type | Storage | Description |
|---|---|---|---|
| `token` | string | `localStorage` | Accumulated current token. |
| `data` | object | `localStorage` | Verified user info (from verify response). |
| `tokens` | `map[string,string]` | `localStorage` | All tokens keyed by step/response_field for placeholder resolution. |
| `isLoading` | bool | memory | |

## Client UI State (DynamicAuth component)

| Key | Type | Description |
|---|---|---|
| `stepIndex` | number | Current step (0-based); `> 0` enables Back. |
| `form` | object | Current step field values. |
| `forms[i]` | object | Per-step form snapshot; restored when navigating Back. |
| `resolvedOptions` | object | Select options resolved from context after preCalls. |
| `loading` / `error` | bool / string | Submit/entry in-flight flag and error message. |

## Validation Rules
- `steps` must be non-empty (1–5).
- Each step must have `title`, `api`, `method`, `response_field`.
- Placeholders must match `{{identifier}}` or `{{stepN.identifier}}`.
- `method` must be a valid HTTP method.
- `AuthField.type` supports `text`, `password`, `email`, `select`.
- `AuthField.options` may be an array or a reference `${key}` resolved from preCall/context **after preCalls run** (step entry).
- `verify.url` required when `verify` present; `clear_on_error` defaults to `true`.

## State Transitions
Login flow: `idle` → step entry (run `preCalls`; resolve options; on preCall failure → `step[i] entry-error` → retry re-runs from scratch) → `step[i] submitting` → (success) `step[i+1]` or (last step) `verify` → `authenticated`; on verify failure with `clear_on_error: true` → `reset()` → `idle` (with `false` → `authenticated`). On step API failure → `step[i] error` (retry re-runs preCalls + main call). `Back` → `step[i-1]` (snapshot restored, preCalls re-run). `reset()` → `idle` + cleared storage. Session restore (boot with existing token) → `verify` → `authenticated` or reset per `clear_on_error`.
