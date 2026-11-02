# LOGS

Append-only change log. `speckit-log` appends one entry per change; never rewrite
or reorder existing entries.

Each entry is a heading line with a timestamp and a short title, followed by a
one-line summary:

```text
YYYY-MM-DD HH:MM: <short title>
<one-line summary>
```

## Entries

2026-11-01 21:25: feat: dynamic auth login
Replaced hardcoded LoginForm with config-driven multi-step auth flow served by Go API; tokens accumulate in localStorage.

2026-11-02 08:27: fix: auth preCall timing, verify call, back navigation (US1 R1)
DynamicAuth runs step preCalls on entry (account select populates from ${accounts}), invokes the config verify block after the final step and on session restore with clear_on_error handling, and supports Back with per-step form snapshots; verified E2E via mock SSO.

2026-11-02 08:27: fix: auth fallback config parity (US3 R1)
Bundled client/public/auth-config.json now mirrors config/auth.yaml (verify block + select account field) so API-unavailable fallback behaves identically; Go loader test extended to cover verify and select contract.
