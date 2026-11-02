# Data Model: Partner Common XML Source

## PartnerConfig

Root of `config/partner.yaml`.

| Field | Type | Required | Rules |
|-------|------|----------|-------|
| `active` | string | yes | Must match a key in `partners`; otherwise the loader reports an error and falls back for auth. |
| `partners` | map[string]PartnerProfile | yes | At least one profile. |

**Resolution**: `active` selects the only profile applied to common-endpoint requests (FR-017). Profile keys are operator-facing identifiers; they are not exposed in URLs.

## PartnerProfile

| Field | Type | Required | Rules |
|-------|------|----------|-------|
| `api` | PartnerAPI | yes | Upstream partner API connection. |
| `auth` | map | no | Login flow served by `GET /api/auth/config`; must contain `steps` to be usable. |
| `xml` | PartnerXML | no | Declarative extensions; absent means base XML only. |

## PartnerAPI

| Field | Type | Required | Default | Rules |
|-------|------|----------|---------|-------|
| `base_url` | string | no | `PARTNER_API_URL` env or `http://localhost:3030` | Absolute URL, no trailing slash required. |
| `timeout` | duration string | no | `10s` | Applies to every upstream call. |
| `headers` | map[string]string | no | empty | Added to every upstream request. |
| `endpoints.channel_list` | string | no | `/api/v2/common/channel-playouts` | May contain `{channel_id}` / `{epg_id}` placeholders where applicable. |
| `endpoints.channel_detail` | string | no | `/api/v2/common/channel-playouts/{channel_id}` | Used to resolve an unregistered channel's guide id. |
| `endpoints.schedule` | string | no | `/api/v2/common/channel-epg/{epg_id}/schedules` | Schedules for a day. |
| `channel_list_query` | map[string]string | no | `$limit=500`, `is_playout_assigned=true` | Values may be overridden per partner. |
| `schedule_query` | map[string]string | no | `$eager`, `$skip`, `$limit` | `$date` and `tz` are always added by the handler. |

## PartnerXML

| Field | Type | Rules |
|-------|------|-------|
| `channel.custom_fields` | []XMLField | Appended after fixed channel elements. |
| `epg.custom_fields` | []XMLField | Appended after fixed EPG item elements. |

### XMLField

| Field | Type | Required | Rules |
|-------|------|----------|-------|
| `name` | string | yes | Valid XML element name; must not collide with reserved base names (`id`, `name`, `img`, `start`, `stop`, `streamId`, `ads`, `channelId`). |
| `source` | string | conditional | Dotted path into the normalized item view (e.g. `programme.name`, `base_name`, `logo.url`). Exactly one of `source`/`value` must be set. |
| `value` | string | conditional | Literal text emitted for every item. |
| `when` | string | no | `always` (default) or `metadata` (only when `$include_metadata=true`). |
| `default` | string | no | Text used when the source path is missing. Defaults to empty. |

Invalid fields are logged and skipped; they never change the base output (FR-016).

## ChannelView

Normalized channel passed to the XML builder.

| Field | Type | Source | Rules |
|-------|------|--------|-------|
| `id` | string | upstream `id` | Required; empty id drops the item and logs. |
| `name` | string | upstream `name`; fallback `base_name` | Empty allowed. |
| `img` | string | profile source, default candidates `img`, `image`, `logo`, `thumbnail`, `poster`; object → `url` or `src` | Empty string renders `<img></img>`. |
| `raw` | map[string]any | whole upstream item | Addressable by custom field sources. |

## EPGItemView

Normalized programme passed to the XML builder.

| Field | Type | Source | Rules |
|-------|------|--------|-------|
| `start` | string | parsed `local_date_start` formatted RFC3339 | In channel `ContentTimezone`, host-local fallback. Must be before `stop`. |
| `stop` | string | parsed `local_date_end` formatted RFC3339 | Same location as `start`. |
| `streamId` | string | primary stream selection over `programme.programmeStreams` | Item omitted when no stream matches (edge case). |
| `ads` | bool | `programme.is_ads` | Rendered as lowercase `true`/`false`. |
| `channelId` | string | request's playout channel id | Emitted once as a root child. |
| `raw` | map[string]any | schedule item plus `channel`, `programme` | Addressable by custom field sources. |

### Primary stream selection

Same rule as the planner: first stream where `is_vod == true && is_trailer == false`, or `name == "Default Stream"`. `is_ads` programmes use the same rule; the ad status is carried separately.

## XML Output Shapes

### Channel list

```xml
<?xml version="1.0" encoding="UTF-8"?>
<channel-playouts>
  <channel>
    <id>ch-1</id>
    <name>Channel One</name>
    <img>https://cdn.example.com/ch-1.png</img>
  </channel>
</channel-playouts>
```

### EPG

```xml
<?xml version="1.0" encoding="UTF-8"?>
<epg>
  <channelId>ch-1</channelId>
  <epgitem>
    <start>2026-11-02T00:00:00+07:00</start>
    <stop>2026-11-02T01:00:00+07:00</stop>
    <streamId>stream-123</streamId>
    <ads>false</ads>
  </epgitem>
</epg>
```

### Error

```xml
<?xml version="1.0" encoding="UTF-8"?>
<error>
  <code>channel_not_found</code>
  <message>Channel not found</message>
</error>
```

## PlayoutChannel (existing, reused)

| Field | Role in this feature |
|-------|----------------------|
| `id` | Request path `:channelId`. |
| `channel_epg_id` | Resolves the upstream guide id without another channel-detail call. |
| `content_timezone` | Renders `start`/`stop` offsets. |
| `use_ad_epg_ads` | Deprecated; ignored now that EPG ads are the only mode. Kept for schema/client compatibility. |

## SegmentItem (existing, changed behavior)

| Field | Change |
|-------|--------|
| `is_ad` | Now set from `programme.is_ads` instead of interval injection. |
| `ad_type` | `out` on the first segment of an EPG ad programme, `in` on the last. |
| `ad_break_id` | Stable id derived from the ad programme's start time. |
| `ad_break_start` | Ad programme start (Unix seconds, local timeline). |
| `ad_break_duration` | Ad programme end minus start (seconds). |
| `splice_inserts` | `out` (0x34) at break start, `in` (0x35) at break end, plus pre-roll cues on the preceding content segment. |

## State and Validation Summary

- Partner profile selection is deployment state (`active`), not request state.
- `$include_metadata` is request state: any value other than true excludes `when: metadata` fields.
- `date`/`$date` is request state; default host-local today.
- Channel lookup precedence: local `playout_channels` → upstream channel detail → 404.
- Base names are reserved; invalid extensions degrade to base XML and log.
- XML is always well-formed, including error responses.
