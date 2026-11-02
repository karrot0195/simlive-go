# Quickstart: Dynamic Auth Login

## Prerequisites
- Go 1.x, Node 18+, running Postgres/Redis (or `make start-all`).
- SSO service on port 3030 (existing).

## Backend
1. Place/edit `config/auth.yaml` (see `contracts/auth-config.json` for schema).
2. Optionally set `AUTH_CONFIG_PATH` in `.env`.
3. Run API: `make start-api` (serves `GET /api/auth/config`).
4. Verify: `curl http://localhost:8080/api/auth/config | jq` — response includes `steps` and (when configured) top-level `verify`.

## Frontend
1. `cd client && npm install && npm run dev`.
2. Open `http://localhost:5173`; unauthenticated view renders `DynamicAuth` (not `LoginForm`).
3. Complete steps; tokens accumulate in `localStorage` (`token`, `tokens`).

## Validation scenarios
- **SC-001**: Add a new step by editing `config/auth.yaml` only; reload; flow renders new step.
- **SC-002**: Test configs with 1 and 5 steps render correctly.
- **SC-003**: No hardcoded endpoints in client auth code (grep `sso/auth` only in config, not component).
- **SC-004**: 2-step flow passes token from step 1 to step 2 (inspect network headers).
- **Fallback**: Stop API; client loads bundled `public/auth-config.json`.
- **Retry**: Fail a step; retry re-runs preCalls (verify in network tab).

## R1 validation scenarios (flow correctness)
- **R1-SC1 preCall on step entry**: Load the 2-step flow. On entering step 2 (Select Account), the network tab shows `GET .../sso/auth/accounts` **before** the step renders, and the Account select is populated with options (not empty).
- **R1-SC2 verify after final step**: Complete step 2; the network tab shows the configured `verify` call (`POST .../verify-token` with resolved `Authorization: Bearer {{token}}` header). On success the app enters the authenticated view.
- **R1-SC3 verify on session restore**: Reload the page with an existing token in `localStorage`; the verify call fires again; on failure with `clear_on_error: true` the session resets and the login flow reappears.
- **R1-SC4 back navigation**: On step 2, click Back; step 1 re-renders with the previously entered username/password intact. Continue re-runs step 1 (preCalls + main call) and returns to step 2.
- **R1-SC5 fallback parity**: With the API stopped, the bundled `public/auth-config.json` produces the same flow (select account step + verify) as the server-served config.
