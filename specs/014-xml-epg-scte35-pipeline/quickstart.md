# Quickstart: XML EPG Ad Pipeline Validation

This guide proves the feature end to end: XML markers become breaks, boundaries land in the right segments, PTS stays continuous, and the engine emits expected cue tags with a static PMT.

## Prerequisites

- Docker Compose stack: `docker compose up -d postgres redis`
- A `.env` with the local DB/Redis settings and a writable `TMP_DIR`
- `rclone` configured with a test remote containing one content asset and one ad asset, each with `vod.m3u8` and rendition playlists
- `tsp`, `ffmpeg`, and `ffprobe` on `PATH`
- `config/partner.yaml` active profile's `api.base_url` pointing at the stub server below

## 1. Unit and build gates

```bash
make fmt
make vet
make test
make build
```

Expected: all pass.

## 2. Serve a deterministic XML EPG

Write a fixture that returns one content programme with two markers, plus one ad-only programme:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<epg>
  <programme start="20261103000000 +0700" stop="20261103003000 +0700">
    <stream stream_id="<content-path>"/>
    <ads path="<ad-path-1>" duration="20" start="20261103001000 +0700"/>
    <ads path="<ad-path-2>" duration="10" start="20261103002000 +0700"/>
  </programme>
  <programme start="20261103003000 +0700" stop="20261103003030 +0700" is_ads="true">
    <stream stream_id="<ad-path-3>"/>
    <ads>true</ads>
  </programme>
</epg>
```

Serve it over HTTP and register the channel in PostgreSQL with an id matching the path the planner requests (`/common/channel/epg/{channelId}`):

```bash
docker exec -i simlive_postgres psql -U postgres -d simlive <<'SQL'
INSERT INTO playout_channels (id, name, base_name, content_timezone, status)
VALUES ('ch-test', 'Test Channel', 'test', 'Asia/Saigon', 'PREPARE')
ON CONFLICT (id) DO UPDATE SET status = 'PREPARE';
SQL
```

## 3. Plan the day

```bash
make start-planner
tail -f logs/planner.log
```

Expected: the planner logs a completed plan for `ch-test`; no interval-ad setting is read.

## 4. Inspect the timeline

```bash
docker exec simlive_redis redis-cli --raw ZRANGE simlive:timeline:ch-test 0 -1 > /tmp/timeline.jsonl
jq -c 'select(.is_ad == true or .ad_type == "in") | {seq: .sequence, is_ad, ad_type, ad_break_id, start: .media_start, end: .media_end, types: [.splice_inserts[]?.segmentationTypeId]}' /tmp/timeline.jsonl
```

Expected per break:
- Two content segments with `segmentationTypeId` 48 (`0x30`) at break start − 8 s and − 4 s.
- One ad segment with `ad_type: "out"` and type 52 (`0x34`).
- One ad segment with type 54 (`0x36`) at break end − 4 s.
- One content segment with `is_ad: false`, `ad_type: "in"`, and type 53 (`0x35`).
- The same `ad_break_id` on every entry of a break.
- `media_start` values never move backwards; `ad_break_duration` equals the sum of the break's ad segment durations.

Also confirm the ad-only programme produced a break with no content segment from its stream.

## 5. Verify playlist tags

```bash
make start-engines
curl -s http://localhost:8080/live/ch-test/360p/playlist.m3u8 | tee /tmp/playlist.m3u8
```

Expected order at a break edge:

```text
#EXT-X-DATERANGE:ID="<id>",CLASS="com.apple.hls.avails",START-DATE="...",SCTE35-OUT="...",DURATION=...,END-ON-NEXT=YES
#EXT-X-SCTE35:CUE="...",TYPE=52,ID="<id>"
#EXTINF:...
seg_N.ts
```

and when content resumes:

```text
#EXT-X-DATERANGE:ID="<id>",START-DATE="...",SCTE35-IN="...",DURATION=...
#EXT-X-SCTE35:CUE="...",TYPE=53,ID="<id>"
#EXTINF:...
seg_M.ts
```

Placement-start (`TYPE=48`) and placement-end (`TYPE=54`) tags must appear on the segments identified in step 4.

## 6. Verify SCTE-35 and PMT

Use the existing verification skill and tools against a channel that has run through at least one break:

```bash
./bin/scte35-check -url http://localhost:8080/live/ch-test/playlist.m3u8
./bin/pmt-verify -url http://localhost:8080/live/ch-test/360p/playlist.m3u8
./bin/hls-watch -url http://localhost:8080/live/ch-test/playlist.m3u8
```

Expected:
- Every break has one cue-out and one cue-in with matching event IDs.
- Immediate-splice commands (`0x34`, `0x35`) carry no `pts_time`; placement markers (`0x30`, `0x36`) do.
- PMT content is identical across content, ad, and splice-bearing segments; SCTE-35 stream type 0x86, PID 0x1F00, registration CUEI.
- No interval-generated ad appears when the fixture has no markers.

## 7. Negative scenarios

| Scenario | Fixture change | Expected |
|----------|----------------|----------|
| No ads | Remove all `<ads>` elements | Zero ad segments, zero cue tags |
| Past marker | Set a marker start before the plan start | Marker skipped, content uninterrupted |
| Bad asset | Point one marker at a missing path | That break skipped and logged; other breaks still play |
| Bad duration | Set `duration="0"` | Marker ignored |
| Two markers same start | Duplicate a marker | Two sequential breaks, each with its own markers |

## 8. Cleanup

```bash
make stop
```

Delete the test channel row when finished if it is not part of a shared environment.
