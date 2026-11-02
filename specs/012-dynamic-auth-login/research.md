# Research: Dynamic Auth Login

## Decisions

### D1: Placeholder syntax for cross-step references
- **Decision**: Mustache-style placeholders `{{token}}` and `{{stepN.response_field}}` resolved client-side before each call.
- **Rationale**: Readable in YAML, trivial to validate with a regex, and avoids a JSONPath dependency. Matches common config-driven flow patterns.
- **Alternatives considered**: JSONPath (`$.steps[0].token`) — more powerful but heavier and harder to author; single-token-only references — too limited for preCalls.

### D2: Token persistence
- **Decision**: Accumulate tokens in `localStorage` (existing auth store keys) so the flow survives reloads.
- **Rationale**: Aligns with existing `useAuthStore` behavior (`setToken` already writes `token`, `userInfo`, `is_super_admin` to `localStorage`); user explicitly chose localStorage.
- **Alternatives considered**: in-memory only (loses progress on reload); sessionStorage (cleared on tab close).

### D3: Step conditionality
- **Decision**: Strictly sequential steps in v1; no conditional/branching steps.
- **Rationale**: Keeps scope bounded and the YAML schema simple; conditional logic can be added later without breaking the sequential core.
- **Alternatives considered**: per-step `condition` field; full branching by outcome — deferred.

### D4: Retry semantics
- **Decision**: Retry re-runs preCalls and the main API call from scratch.
- **Rationale**: PreCalls may fetch fresh nonces/tokens; re-running guarantees a clean slate and avoids stale intermediate state.
- **Alternatives considered**: cache preCall results per step entry — rejected (stale risk).

### D5: Config transport format
- **Decision**: Go API parses server-side `auth.yaml` and serves parsed JSON; client needs no YAML parser. Bundled fallback is JSON.
- **Rationale**: Removes a client dependency, centralizes parsing/validation on the server, and keeps the client lean.
- **Alternatives considered**: raw YAML over the wire (requires client YAML lib).

### D6: YAML library (Go)
- **Decision**: `gopkg.in/yaml.v3`.
- **Rationale**: Standard, maintained, struct-tag friendly; already common in Go projects.
- **Alternatives considered**: `github.com/goccy/go-yaml` — equivalent; stick with yaml.v3 for familiarity.

### D7: Config file location & override
- **Decision**: Default `./config/auth.yaml` (or `AUTH_CONFIG_PATH` env override). If missing, handler returns a built-in default single-step login config (JSON) so the system never fails to serve config.
- **Rationale**: Matches existing config dir convention (`config/`); env override follows `.env` pattern used by `utils.GetSettings()`.
- **Alternatives considered**: embed via `go:embed` — considered for fallback only; keep file-based for operator editability.

### D8: preCall execution timing (R1)
- **Decision**: preCalls run when a step is entered (initial mount, advance, back) — before the step's UI resolves dependent options — and again on submit (retry re-runs from scratch).
- **Rationale**: Select fields reference preCall results (`${accounts}`); the options must exist before the user can submit. The spec clarification already mandates retry re-runs preCalls; running them at entry makes the step usable at all. Idempotent GET preCalls make re-entry safe.
- **Alternatives considered**: run only at submit (initial behavior — broken for dependent selects; reported as bug); cache preCall results per step entry (rejected — stale risk, per D4).

### D9: verify invocation points and clear_on_error (R1)
- **Decision**: The top-level `verify` block is invoked (a) after the final step succeeds, and (b) on app boot when a token exists in localStorage. On verify failure with `clear_on_error: true` (default), reset auth state and return to the login flow; with `clear_on_error: false`, keep the session. If no `verify` is configured, fall back to existing `setToken` behavior.
- **Rationale**: Matches operator intent (config-driven verify endpoint), keeps session restore honest (restored tokens are re-verified), and gives operators an escape hatch (`clear_on_error: false`) for flaky verify endpoints.
- **Alternatives considered**: hardcoded verify endpoint (violates SC-003 — no hardcoded endpoints in client auth code); verify only at login (leaves restored sessions unverified).

### D10: Back navigation is client-side with state snapshots (R1)
- **Decision**: Back decrements the step index and restores the target step's form snapshot; no main API calls on back; the target step's preCalls re-run on entry (idempotent GETs).
- **Rationale**: Sequential flow integrity is preserved (no skipping forward); users can correct mistakes without restarting the flow.
- **Alternatives considered**: disabling back (reported as bug — rejected); server-side step state (over-engineering for v1).

### D11: Bundled fallback mirrors server config (R1)
- **Decision**: `client/public/auth-config.json` stays in sync with `config/auth.yaml` (same steps, fields including the `select` account field, and the `verify` block).
- **Rationale**: The fallback exists so behavior is identical when the API is unavailable; divergence (current fallback lacks `verify` and uses a text field instead of select) breaks the flow in fallback mode.
- **Alternatives considered**: minimal fallback (single hardcoded step) — rejected: silent behavior change between modes.
