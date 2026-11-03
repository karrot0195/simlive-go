# Contract: HLS Cue and SCTE-35 Output

The engine turns timeline members into per-rendition media playlists. Marker order within a segment is: date-range tag (if any), then per-segment SCTE-35 tags in insertion order, then `#EXTINF` and the segment URI.

## Date-range tags

First ad segment of a break:

```text
#EXT-X-DATERANGE:ID="7",CLASS="com.apple.hls.avails",START-DATE="2026-11-03T03:30:00Z",SCTE35-OUT="<base64>",DURATION=30,END-ON-NEXT=YES
```

First content segment after the break:

```text
#EXT-X-DATERANGE:ID="7",START-DATE="2026-11-03T03:30:00Z",SCTE35-IN="<base64>",DURATION=30
```

Rules:
- `ID` is the break identity as a decimal string.
- `START-DATE` is the break start in RFC3339 UTC.
- `DURATION` is the played break duration, unquoted, trailing-zero trimmed.
- `CUE-OUT` carries `CLASS` and `END-ON-NEXT=YES`; `CUE-IN` carries neither.
- If the base64 payload cannot be produced, the line is still emitted without the `SCTE35-*` attribute and the failure is logged.

## Per-segment SCTE-35 tags

```text
#EXT-X-SCTE35:CUE="<base64>",TYPE=<segmentation_type_id_decimal>,ID="7"
```

| Marker | Type (decimal) | Where |
|--------|----------------|-------|
| Placement opportunity start | 48 (`0x30`) | Content segment containing break start − 8 s and break start − 4 s |
| Cue-out | 52 (`0x34`) | First ad segment of the break |
| Cue-in | 53 (`0x35`) | First content segment after the break |
| Placement opportunity end | 54 (`0x36`) | Ad segment containing break end − 4 s |

## SCTE-35 command shape

This table describes the bitstream injection (TSDuck) shape. The per-segment `#EXT-X-SCTE35` CUE payload keeps the existing main-branch encoding, which uses the immediate-splice form for every marker type; the playlist tag's `TYPE` attribute still distinguishes placement, cue-out, cue-in, and placement-end.

| Marker | Immediate splice | `pts_time` | Break duration | Out-of-network |
|--------|------------------|------------|----------------|----------------|
| Placement start | no | yes | yes | yes |
| Cue-out | yes | no | yes | yes |
| Cue-in | yes | no | no | no |
| Placement end | no | yes | yes | yes |

`pts_time` is expressed in the segment's rewritten PTS space — the timeline offset the engine applies with `output_ts_offset` — not the absolute wall clock. It is masked to 33 bits when emitted.

## PMT

Every segment from one rendition must expose identical PMT content:

| Property | Value |
|----------|-------|
| SCTE-35 stream type | `0x86` |
| SCTE-35 PID | `0x1F00` |
| Registration descriptor | `0x43554549` (`CUEI`) |

The SCTE-35 elementary stream is declared even in segments that carry no SCTE-35 packets.

## Playlist invariants

1. A break produces exactly one cue-out date-range and exactly one cue-in date-range.
2. The cue-in date-range appears before the `#EXTINF` of the first content segment after the break.
3. Every splice marker appears before the `#EXTINF` of the segment it is attached to.
4. `#EXT-X-MEDIA-SEQUENCE` and `#EXT-X-TARGETDURATION` remain as today.
5. No cue tag appears for a segment that is not in the timeline window.
