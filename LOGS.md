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

2026-11-02 11:09: feat: partner common XML source (US1)
GET /common/channel-playouts-xml returns well-formed XML with id/name/img per channel from the active partner profile; verified live (12 channels, application/xml) including empty <img> and the base_name/provider custom fields.

2026-11-02 11:09: feat: partner EPG XML endpoint (US2)
GET /common/channel/epg/:channelId resolves the guide locally then upstream and returns one day of <epgitem> with start/stop/streamId/ads, gated metadata and 400/404/502 XML errors; verified live (86 items, host-local date, RFC3339 offsets).

2026-11-02 11:09: feat: config-only partner onboarding (US3)
internal/partner validates and emits custom channel/EPG fields (source|value, when, default, reserved-name guard) so a new partner needs only partner.yaml edits; verified live by adding <provider> without code changes.

2026-11-02 11:09: feat: consolidate auth into partner.yaml (US4)
config/auth.yaml removed; GET /api/auth/config serves the active profile's auth block from config/partner.yaml, falls back to legacy auth.yaml then the built-in default; verified live including the rename-and-fallback path.

2026-11-02 11:09: feat: EPG-driven ads, interval insertion removed (US5)
Removed AD_INSERTION_INTERVAL / loadAdSegments / interval ad injection; planner now marks EPG programme.is_ads items with stable break id, start/duration, out/in boundaries and pre-roll cues; go test coverage added.

2026-11-02 11:09: chore: fix upstream paths, image field and error mapping
Fixed upstream paths are no longer configurable (removed partner.yaml api.endpoints), img now reads the real img_url field, and upstream 404/422 map to a 404 channel_not_found instead of 502.

2026-11-03 10:36: feat: bucket-inclusive streamId, STORAGE_BUCKET removed
EPG streamId is the complete storage path (rclone remote:bucket/...) consumed verbatim by planner/engine; STORAGE_BUCKET/ADS_STREAM_ID env config and the strip/re-add bucket logic removed (specs/013 FR-019, research D12).
