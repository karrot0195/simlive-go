# Contract: Upstream EPG XML Input

The planner consumes the partner's XML EPG for one channel and day. Times use the partner XML layout (`YYYYMMDDHHMMSS ±HHMM`) and are normalized to the server clock before planning.

## Document shape

```xml
<?xml version="1.0" encoding="UTF-8"?>
<epg>
  <programme start="20261103000000 +0700" stop="20261103010000 +0700">
    <stream stream_id="partner-video-lib:partner-video-lib/vod/ch-1/prog-1"/>
    <ads path="partner-video-lib:partner-video-lib/ads-v3/break-1"
         duration="30"
         start="20261103003000 +0700"/>
  </programme>
  <programme start="20261103010000 +0700" stop="20261103010500 +0700" is_ads="true">
    <stream stream_id="partner-video-lib:partner-video-lib/ads-v3/break-2"/>
    <ads>true</ads>
  </programme>
</epg>
```

## Rules

| Element | Required | Rules |
|---------|----------|-------|
| `programme` | yes | One per guide item. Requires both `start` and `stop`, with `stop` after `start`. |
| `stream` | yes | At least one. The primary stream is the first where `is_vod && !is_trailer`, or `name == "Default Stream"`. |
| `stream@stream_id` | yes | Bucket-inclusive storage base used verbatim. |
| `ads` with attributes | no | Inline break marker. `path`, `duration` (> 0), and `start` are all required. |
| `ads` with text only | no | `true` marks the whole programme as an ad break and synthesizes one marker from the programme window. Any other value is ignored. |
| `programme@is_ads` | no | Equivalent to a text-only `true` marker when present. |

## Normalization

- All timestamps are converted to the server's local clock and re-formatted with the same layout.
- Items without a usable primary stream are omitted.
- Items whose start or stop cannot be parsed are omitted.
- Marker order is the document order; the planner sorts by declared start before processing.
