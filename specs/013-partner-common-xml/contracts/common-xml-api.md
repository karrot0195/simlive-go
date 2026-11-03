# Contract: Common XML API

All responses use `Content-Type: application/xml; charset=utf-8`. The XML declaration is always emitted. Element order is deterministic: fixed base elements first, then the active partner's custom fields in configuration order.

The base address is deployment-specific. Examples use `http://localhost:3030` per the feature input; the server binds to `API_ADDR` (default `:8080`).

## GET /common/channel-playouts-xml

Returns the playout channel list from the active partner profile's upstream channel source.

### Request

No required parameters. Unknown query parameters are ignored.

### 200 Response

```xml
<?xml version="1.0" encoding="UTF-8"?>
<channel-playouts>
  <channel>
    <id>ch-1</id>
    <name>Channel One</name>
    <img>https://cdn.example.com/ch-1.png</img>
  </channel>
  <channel>
    <id>ch-2</id>
    <name>Channel Two</name>
    <img></img>
  </channel>
</channel-playouts>
```

With a partner custom field `base_name` (`when: always`) and `region` (`when: metadata`):

```xml
<channel>
  <id>ch-1</id>
  <name>Channel One</name>
  <img>https://cdn.example.com/ch-1.png</img>
  <base_name>news</base_name>
</channel>
```

`region` appears only on the EPG endpoint when metadata is requested; channel/extensions follow the same `when` rule per endpoint.

### Empty result

```xml
<?xml version="1.0" encoding="UTF-8"?>
<channel-playouts></channel-playouts>
```

## GET /common/channel/epg/:channelId

Returns one day of the channel's programme guide.

### Request

| Parameter | In | Required | Rules |
|-----------|----|----------|-------|
| `channelId` | path | yes | Playout channel id. Rejected with 400 if it contains `..` or `/`. |
| `$include_metadata` | query | no | `true` (case-insensitive, also `1`) includes fields with `when: metadata`; anything else omits them. |
| `date` / `$date` | query | no | `YYYY-MM-DD`; default is host-local today. Invalid values return 400. |

### 200 Response

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
  <epgitem>
    <start>2026-11-02T01:00:00+07:00</start>
    <stop>2026-11-02T01:00:30+07:00</stop>
    <streamId>ads-stream-01</streamId>
    <ads>true</ads>
  </epgitem>
</epg>
```

With `$include_metadata=true` and a custom EPG field `title` (`source: programme.name`, `when: metadata`):

```xml
<epgitem>
  <start>2026-11-02T00:00:00+07:00</start>
  <stop>2026-11-02T01:00:00+07:00</stop>
  <streamId>stream-123</streamId>
  <ads>false</ads>
  <title>Morning News</title>
</epgitem>
```

### Empty guide

```xml
<?xml version="1.0" encoding="UTF-8"?>
<epg>
  <channelId>ch-1</channelId>
</epg>
```

## Errors

| Status | Code | When |
|--------|------|------|
| 400 | `invalid_channel_id` | `:channelId` contains `..` or `/`. |
| 400 | `invalid_date` | `date`/`$date` is not `YYYY-MM-DD`. |
| 404 | `channel_not_found` | Channel is absent locally and upstream. |
| 502 | `partner_unavailable` | Upstream channel list, detail, or schedule call fails. |

```xml
<?xml version="1.0" encoding="UTF-8"?>
<error>
  <code>channel_not_found</code>
  <message>Channel not found</message>
</error>
```

## Field guarantees

- `channel` always contains `id`, `name`, `img` in that order, even when a value is empty.
- `epgitem` always contains `start`, `stop`, `streamId`, `ads` in that order.
- `streamId` is the complete storage path including the bucket (rclone `remote:bucket/...` form, e.g. `espx-video-lib:espx-video-lib/vod/...`). Consumers resolve segments as `streamId/{rendition}/{file}` directly; no separate `STORAGE_BUCKET` setting is needed.
- Custom fields are appended after base fields; a custom field that resolves to an empty value with no `default` emits an empty element.
- `ads` is the lowercase string `true` or `false`.
- Times are RFC3339 with an explicit offset from the channel's content timezone (host-local fallback).
