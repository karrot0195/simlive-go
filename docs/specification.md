# specification.md (Updated)

> Status: reflects the **implemented** system as of 2026-08-30 — 3-tier quality
> selection (`SIMLIVE_QUALITY`), VOD↔ads rendition matching, local `PtsOffset`
> timeline, ffmpeg PTS/DTS rewrite in the engine, and the **M1.5 multi-rendition
> detach/map** (1 shared segment cache + 3 advertised rungs via master
> playlist, with per-rung media playlists `playlist_360p.m3u8` /
> `playlist_720p.m3u8` / `playlist_1080p.m3u8`).
> Plan: [300826_0620-multi-rendition-abr-ladder.md](300826_0620-multi-rendition-abr-ladder.md).
> For current status and

```markdown
## 🇬🇧 English Version: SimLive Project Specification & Architecture (Updated)

### 1. High-Level Architecture Overview

The system operates as a 24/7 linear playout engine (Simulated Live Streaming):

1. **Planner (PrepareSimlive):** Periodically queries the ESPX API to fetch EPG schedules, maps program items to `.ts` files using `rclone`, and writes the timeline metadata to **Redis** with ad insertion capabilities. Runs as a 24/7 cron loop (default 5m, `PLANNER_INTERVAL` override); each tick prepares **today + tomorrow** in the channel's `content_timezone`, short-circuiting dates whose ZSET range is already non-empty (`HasDateInTimeline` via `ZCOUNT`).
2. **Engine (StartSimlive):** Reads mapped segment timelines from Redis using real-time system clocks, downloads required `.ts` files to a local sliding window directory, stitches them into a dynamic HLS stream (`playlist.m3u8`), and serves video players.

---

### 2. Database Schema (PostgreSQL)

A single table is used to store and manage active playout channels.

```sql
CREATE TABLE playout_channels (
    id VARCHAR(36) PRIMARY KEY,           -- Playout Channel ID (e.g., 7261da4b-9c45-4619-bfb0-a6119dc7b6e6)
    name VARCHAR(255) NOT NULL,          -- Name (e.g., "Playout 09")
    base_name VARCHAR(255),               -- Base name (e.g., "playout-09")
    content_timezone VARCHAR(50),        -- Timezone (e.g., "Asia/Saigon")
    channel_epg_id VARCHAR(36),          -- EPG ID fetched from API 2
    status VARCHAR(20) DEFAULT 'PREPARE', -- Status: PREPARE, LIVE, INACTIVE (ERROR planner-only)
    is_active BOOLEAN DEFAULT TRUE,      -- Whether the planner should walk this channel
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

> **`is_active` and `status` are written by four callers**:
> 1. The planner's cron loop **reads** them (`db.Where("is_active = ?", true)`).
> 2. The planner's breaker (`markChannelInactive` in
>    `internal/planner/planner.go`) **writes** `is_active=false` after
>    `failureInactivateThreshold` (5) consecutive `(channel, date)` failures
>    (the status column is untouched).
> 3. The planner **writes** `status='LIVE'` (`markChannelLive` /
>    `MarkChannelLiveForChannel`) on a successful prepare or a "date
>    already prepared" short-circuit — it is the SINGLE writer of 'LIVE',
>    and the engine gates on it.
> 4. The REST API **writes** `status='PREPARE'` (register + start) and
>    `status='INACTIVE'` (stop + soft delete) via `PUT/PATCH
>    /api/channels/:id` and `POST /api/channels/:id/{start,stop}` (see §3
>    "REST API Surface"). **DELETE** is a soft delete: the row is
>    preserved, only `is_active=false / status='INACTIVE'`.
>
> **Redis purge on stop/delete.** `POST /api/channels/:id/stop` and
> `DELETE /api/channels/:id` both call `purgeChannelRedisData(id)` (in
> `internal/api/handler.go`), which DELs every per-channel Redis key:
> `simlive:hls:{ch}` (master), `simlive:hls:{ch}:rendition:{rung}` (×3),
> `simlive:timeline:{ch}` (planner ZSET), `simlive:renditions:{ch}`
> (per-channel ladder metadata), and `planner:cursor:{ch}` (per-channel
> cursor). The breaker counter `planner:failures:{ch}` is INTENTIONALLY
> PRESERVED across the purge — `StartChannel` clears it on the next
> start, so a stop → start cycle always behaves like a fresh attempt.
> Disk cache (`{TMP_DIR}/{ch}/`) is recovered by the engine reconciler
> cancelling the per-channel goroutine within ~5s of the stop; the
> goroutine's last `syncLocalCacheDir` pass evicts the bounded active
> window.

---

### 3. ESPX API Integration & Data Flow

#### **API 1: Get Playout Channels List**

* **Endpoint:** `GET [https://api2.espx.cloud/api/v2/common/channel-playouts](https://api2.espx.cloud/api/v2/common/channel-playouts)`
* **Purpose:** Fetch all available playout channels to seed or synchronize the `playout_channels` database table.

#### **API 2: Get Playout EPG ID**

#### **REST API Surface (Local)**

The `cmd/api` binary serves the following routes for playout-channel management
(handlers in `internal/api/handler.go`, request/response types in
`internal/models/models.go`). The planner reads the same `is_active` /
`status` columns so manual flips take effect on the next planner tick
(default 5m, `PLANNER_INTERVAL`).

| Verb   | Path                            | Role                                                                                          |
|--------|---------------------------------|-----------------------------------------------------------------------------------------------|
| `POST` | `/api/channels`                 | Create / upsert. Validates `channel_id` against ESPX API 2 (`GetChannelDetailWithEPG`). Status starts at `'PREPARE'` (the planner flips it to `'LIVE'` on the next tick once timeline data is in Redis). |
| `GET`  | `/api/channels`                 | List local channels. Query params: `?page=` (1-indexed, default 1), `?page_size=` (1-100, default 20), `?status=`, `?is_active=`, `?q=` (name contains). |
| `GET`  | `/api/channels/esp`             | List ESPX upstream channels + `registered` map (keyed by ESPX id; `true` = already in our DB). |
| `GET`  | `/api/channels/:id`             | Read one channel. `404` if not found.                                                         |
| `PUT`  | `/api/channels/:id`             | Full update of mutable fields. Validates `Status` enum (`PREPARE`/`LIVE`/`INACTIVE`; `ERROR` is planner-only). |
| `PATCH`| `/api/channels/:id`             | Partial update — only non-nil body fields are written (pointer-field semantics on `UpdateChannelReq`). |
| `DELETE`| `/api/channels/:id`            | **Soft** delete — flips `is_active=false / status='INACTIVE'`. Idempotent (returns 204 even on missing rows). |
| `POST` | `/api/channels/:id/start`       | Flip `is_active=true / status='PREPARE'` (engine starts playout loop immediately on PREPARE; planner flips to `'LIVE'` on the next tick once timeline data is in Redis) and **clear** the planner's breaker counter key `planner:failures:{id}`. |
| `POST` | `/api/channels/:id/stop`        | Flip `is_active=false / status='INACTIVE'`. Breaker counter is preserved (StartChannel clears it again on the next start). |

All `:id` routes apply a path-guard that rejects `..` and `/` with `400`
(mirrors the HLS route guards).

* **Endpoint:** `GET [https://api2.espx.cloud/api/v2/common/channel-playouts/](https://api2.espx.cloud/api/v2/common/channel-playouts/){playout_id}?$eager=[channelEpg]`
* **Purpose:** Retrieve the associated `channelEpg.id` for each `playout_id` and update the `channel_epg_id` column.

#### **API 3: Get EPG Schedules & Items**

* **Endpoint:** `GET [https://api2.espx.cloud/api/v2/common/channel-epg/](https://api2.espx.cloud/api/v2/common/channel-epg/){channel_epg_id}/schedules?$eager=[programme,programme.programmeStreams,programme.pods]&$skip=0&$limit=30000&$date={YYYY-MM-DD}&tz={+HH:MM}`
* **Purpose:** Fetch all scheduled items for a given date.
* **Key Fields Extracted:**
* `local_date_start` & `local_date_end`: Broadcasting time slots.
* `programme.short_url` (Stream ID): Base directory path on storage.

---

### 4. Media Storage Access via RClone

After extracting the Stream ID (e.g., `PCwdH9LA7uS6jYJDDsmZCL3x`), the Planner executes **`rclone cat`** to parse HLS manifests and extract `.ts` segment lists:

1. **Read Master / Variant Playlist:**
```bash
rclone cat espx-video-lib:espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/L_0/vod.m3u8
```


2. **Parse Manifest Contents:**
* Extract segment file names (`segment_000.ts`, `segment_001.ts`) and target durations (`#EXTINF:XX.X`).
* The system supports dynamic ad insertion by parsing a separate ad manifest (`ads-v3/{adsRend}/vod.m3u8`).
* **Rendition selection:** the VOD dir (`L_0`/`L_1`/`L_2`) and ads dir (`L0`/`L1`/`L2`) are resolved per `SIMLIVE_QUALITY` (see §6.1) so VOD and ads always match resolution.


3. **Download Segment TS (Engine Phase):**
* Download `.ts` files to a temporary local cache when entering the sliding window:


```bash
rclone copyto espx-video-lib:espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/L_0/segment_000.ts /tmp/simlive/channel1/seg_1052.ts

```



---

### 5. Detailed System Modules

#### **Module 1: Planner (PrepareSimlive - Cron Job)**

Runs as a 24/7 cron loop (default 5m, `PLANNER_INTERVAL` override; min effective value 1s). Each tick:

1. Query active channels from `playout_channels`.
2. For each channel, compute **today + tomorrow** in the channel's `content_timezone` (via `utils.GetLocFor`), so a channel in `Asia/Saigon` (UTC+7) doesn't accidentally query tomorrow's EPG when the server clock is in UTC.
3. Call `prepareDate(ch, date)` which short-circuits via `HasDateInTimeline` (`ZCOUNT simlive:timeline:{ch} [dayStart, dayEnd] > 0`) and only invokes `ProcessEPGToTimeline` when the date is missing.
4. For each schedule item inside the per-date horizon:
* Read Stream ID (`programme.short_url`).
* Execute `rclone cat` to read variant `m3u8` contents.
* Compute timeline timestamps for each `.ts` file from the manifest's `#EXT-X-PTS-RANGE` (raw PTS) shifted to a **local timeline**: every segment gets `PtsOffset = MediaStart − timelineBase` (timelineBase = first segment's MediaStart, in seconds). Local offsets avoid the 2^33/90000 ≈ 95443.7s PTS wrap of huge epoch offsets.
* **Ad Insertion:** Periodically insert ad segments at configured intervals (`AdInsertionInterval` = 30 seconds); ads are placed on the same local timeline.
* **Quality tier:** resolve `SIMLIVE_QUALITY` (`auto`|`360p`|`720p`|`1080p`) and pick the VOD/ads rendition pair matching the tier resolution (see §6.1).

8. **Manual control via REST:** `POST /api/channels/:id/{start,stop}` and
   `PUT/PATCH /api/channels/:id` flip the same `is_active` / `status`
   columns the planner reads, so an operator flip takes effect on the
   next planner tick (default 5m, `PLANNER_INTERVAL`). `start` sets
   `status='PREPARE'` — the planner is the SINGLE writer of `'LIVE'` and
   flips it once timeline data is in Redis; the engine only streams
   `is_active` AND `status='LIVE'` channels. `start` also deletes the
   `planner:failures:{id}` Redis key to reset the breaker; `stop`
   preserves the counter. See §3 "REST API Surface".

4. Push segment mappings into a **Redis Sorted Set** (`ZADD` with Epoch timestamp as score):
* **Key:** `simlive:timeline:{playout_id}`
* **Value Structure (JSON):**
```json
{
  "sequence": 1052,
  "storage_path": "espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/L_0/segment_001.ts",
  "duration": 6.006,
  "start_time": 1234567890,
  "pts_start": 2259900,
  "pts_end": 2264550,
  "media_start": 677.80,
  "media_end": 681.72,
  "pts_offset": 12345.60
}

```
* `pts_offset` (local timeline, seconds) is what the engine rewrites each segment's PTS/DTS to (`MediaStart − timelineBase`); `nil` on legacy segments → engine falls back to `media_start`.


6. **Redis Pipeline Batching:** Batch insert operations every 50 items to optimize Redis performance.
7. **Per-tick bookkeeping:** `ProcessWindow` bumps `planner:cursor:{ch}` to `nowUnix + LOOKAHEAD_SECONDS`; `CleanupOldSegments` trims `simlive:timeline:{ch}` with `ZREMRANGEBYSCORE` at `nowUnix - RETENTION_SECONDS`. (The earlier `timeline:%s` key in `CleanupOldSegments` was a silent no-op — fixed.)

#### **Module 2: Engine (StartSimlive - Live Engine & HLS Generator)**

An infinite 1-second tick loop managing playout state:

0. **Reconciler gate (`cmd/engines/main.go`, 5s cadence):** the engine
   starts playout loops for channels matching
   `is_active = true AND status != 'INACTIVE'` (i.e. PREPARE or LIVE).
   A channel registered/started via the REST surface gets `status='PREPARE'`
   and the engine begins its playout loop IMMEDIATELY — it does NOT wait
   for the planner to flip to 'LIVE'. The loop will idle (no segments to
   serve) until the planner writes timeline data to Redis. The planner is
   the SINGLE writer of 'LIVE' (it flips the channel on its next tick once
   timeline data is in Redis). The engine never writes status; it only
   gates on it. Legacy rows stuck in the removed 'ACTIVE' state self-heal
   on the next planner tick.

1. **Clean Channel State (warm-start by default):** By default, on
   startup the engine **preserves** the local cache directory
   (`{TMP_DIR}/{ch}/`) so a restarted engine can resume immediately
   on whatever the previous run had already downloaded. The first
   tick's `syncLocalCacheDir` pass evicts any segments that no longer
   fit the new active window. To force a cold start (e.g. after a
   storage change or in CI), set `ENABLE_CLEAN_UP=true` — the engine
   then `os.RemoveAll(tmpDir)` on startup as before. The Redis
   timeline + renditions + cursor keys are NOT touched here; they
   survive engine restarts by design. (The planner's `PrepareEnv`
   similarly defaults to a scoped wipe of just `simlive:timeline:*`
   and only falls back to a full `FlushAll + os.RemoveAll` when
   `ENABLE_CLEAN_UP=true`.)
2. **Clock Sync:** Check current system epoch timestamp (`currentTime`).
3. **Sliding Window Selector:**
* Query Redis for segments within `[now - 300s, now + 120s]` (score = `media_start`).
* **M1.5 (shared segment cache):** The engine keeps ONE flat set of `seg_N.ts` files in `{TMP_DIR}/{ch}/` and serves them under 3 rung labels. The active rung's VOD dir is the single source the engine downloads from. The master playlist `playlist.m3u8` advertises all 3 rungs via `#EXT-X-STREAM-INF` (360p/720p/1080p); per-rung media playlists `playlist_360p.m3u8` / `playlist_720p.m3u8` / `playlist_1080p.m3u8` all reference the SAME `seg_N.ts` files. `groupSegsByRendition` rescues empty `Rendition` to `DefaultRendition` (`360p`) for legacy timeline members.


4. **Segment Fetcher (Worker Pool, async):**
* Inspect local cache directory `{TMP_DIR}/{playout_id}/` — **M1.5 shared segment cache**: ONE flat set of `seg_N.ts` files is downloaded from the active rung's VOD dir and shared across all 3 rung media playlists. There is no per-rung subdir; the engine keeps a single `channelState` per channel.
* The Redis timeline window `[now−300s, now+120s]` is still queried for timeline awareness, but only the **active on-disk window** is downloaded:
  `SIMLIVE_KEEP_BEHIND` (default 5, behind the playhead) +
  `PlaylistWindowSize` (5, advertised to players) +
  `SIMLIVE_DOWNLOAD_AHEAD` (default 5, pre-fetched ahead).
  Default total = **15 `.ts` files per channel**.
* Use a worker pool (configurable max goroutines) to run `rclone copyto` and prebuffer upcoming segments; downloads run async so the tick never blocks (window slides exactly +1 segment per ~4s).
* Segments are stored as `seg_{sequence}.ts` in the local cache, with an in-flight dedupe map so a file is never downloaded/rewritten twice.

5. **PTS/DTS Rewrite (`rewritePtsOffset`):**
* After download, each `.ts` is remuxed once with ffmpeg `-c copy -copyts -muxdelay 0 -muxpreload 0 -output_ts_offset <offset>`, where `offset = target − firstVideoDTS` (probed with ffprobe).
* Target = `seg.PtsOffset` (local timeline) → fallback `MediaStart` (legacy) → fallback `PtsStart`. No re-encode — DTS becomes continuous across VOD↔ads boundaries.
* Success is marked by a `seg_{sequence}.ts.pts` marker (idempotent); if probing fails the raw file is kept but still marked prepared so the window never stalls.


6. **HLS Dynamic Playlist Builder:**
* Generate live `playlist.m3u8` (5-segment sliding window) with `#EXT-X-TARGETDURATION` from the longest segment.
* Increment `#EXT-X-MEDIA-SEQUENCE` based on the first segment's sequence.
* **Only advertises prepared segments** (those with a `.pts` marker) so players never request in-progress files; keeps the previous playlist while nothing is ready.
* Written atomically to `{TMP_DIR}/{playout_id}/playlist.m3u8`, served by the API at `http://localhost:8080/live/{channelID}/playlist.m3u8`.


7. **Garbage Collection (Clean Up):**
* `syncLocalCacheDir` removes any `.ts`/`.pts` file whose base name is not in the active set (the bounded 15-segment window computed in step 4). Runs every tick, so leftover files from a previous run are evicted on the first tick after startup; no separate startup cleanup is required.
* Purge expired items from Redis older than 120 seconds.



---

### 6. Key Implementation Details

#### **Redis Data Structure**

* **Key:** `simlive:timeline:{channel_id}`
* **Type:** Sorted Set
* **Score:** `MediaStart` (segment start on the local timeline, seconds)
* **Member:** JSON-encoded `SegmentItem` (with `rendition` field; omitempty)
* **Expiry:** 26 hours

* **Key:** `simlive:renditions:{channel_id}` (M1.5)
* **Type:** String (JSON)
* **Value:** Array of `RenditionMeta` for **all 3 rungs** (360p / 720p / 1080p) regardless of `SIMLIVE_QUALITY`
* **Expiry:** 24 hours
* **Producer:** Planner (`writeRenditionMetadata` after each pipeline flush)
* **Consumer:** Engine (`loadRenditionMeta` → `activeRungMeta` → `generateMasterPlaylist`)

* **Key:** `simlive:hls:{channel_id}` (M1.5 — semantics change)
* **Type:** String
* **Value:** **Master** HLS playlist (`#EXTM3U` + 3 × `#EXT-X-STREAM-INF` + `#EXT-X-INDEPENDENT-SEGMENTS`)
* **Expiry:** 24 hours
* **Consumer:** API `GET /live/{channel_id}/playlist.m3u8`

* **Key:** `simlive:hls:{channel_id}:rendition:{rendition}` (M1.5)
* **Type:** String
* **Value:** Per-rung media HLS playlist (5-segment sliding window, prepared segments only; references the same shared `seg_N.ts` files for all 3 rungs)
* **Expiry:** 24 hours
* **Consumer:** API `GET /live/{channel_id}/playlist_{rendition}.m3u8`

#### **Ad Insertion Logic**

* Load ad segments from `espx-video-lib:espx-video-lib/ads-v3/{adsRend}/vod.m3u8`, where `{adsRend}` is chosen by `resolveAdsRendition()` (see §6.1)
* Insert ads when:
1. Segment start time >= current time
2. Elapsed time since last ad >= `AdInsertionInterval` (30 seconds)
* Ads are inserted between content segments, get the same local `PtsOffset` treatment, and increment both global time and ad offset
* **SCTE-35 break metadata (M-SCTE35):** every ad segment in a single break is stamped with 4 `omitempty` fields on the `SegmentItem` so the engine can render `#EXT-X-DATERANGE` cue tags in the per-rung media playlist (see §6.1a below): `is_ad`, `ad_break_id` (stable ID `adbreak-<int64(breakStart)>`), `ad_break_start` (unix seconds → `START-DATE`), `ad_break_duration` (sum of pool durations, seconds → `DURATION` + SCTE-35 `BreakDuration.Duration` 90kHz ticks). Library: `github.com/Comcast/scte35-go` v1.8.0. Plan: [300826_0616-implement-scte35-cue-markers.md](300826_0616-implement-scte35-cue-markers.md).

#### 6.1a SCTE-35 cue tags in media playlists

Per-rung media playlists (`playlist_360p.m3u8` / `_720p` / `_1080p.m3u8`) emit `#EXT-X-DATERANGE` lines on VOD<->ad transitions, mirroring `tmp/SCTE35Utils.ts`:

* **CUE-OUT** (`!prev.IsAd && seg.IsAd`): emitted BEFORE the first ad `seg_N.ts` EXTINF
  ```
  #EXT-X-DATERANGE:ID="<id>",CLASS="com.apple.hls.avails",START-DATE="<RFC3339 UTC>",SCTE35-OUT="<base64>",DURATION=<n>,END-ON-NEXT=YES
  ```
* **CUE-IN** (`prev.IsAd && !seg.IsAd`): emitted AFTER the last ad `seg_N.ts` EXTINF, BEFORE the first post-ad VOD EXTINF
  ```
  #EXT-X-DATERANGE:ID="<id>",START-DATE="<RFC3339 UTC>",SCTE35-IN="<base64>",DURATION=<n>
  ```
* `SCTE35-OUT` / `SCTE35-IN` base64 is a `SpliceInfoSection` with `SpliceInsert{SpliceImmediateFlag:true, OutOfNetworkIndicator:<out>, BreakDuration{AutoReturn:true, Duration:uint64(durSec*90000)} if out}, Tier:4095, SAPType:3` (CRC32 auto-validated by the library).
* `DURATION` is unquoted and trailing — threefive `TagParser` parses a line as quoted-vs-unquoted by the line's last char; a trailing quoted base64 value gets misparsed (see `tmp/SCTE35Utils.ts#L51-L59`).
* DURATION precision: 3 decimal places (matches `#EXTINF:%.3f`); trailing zeros + lone trailing dot stripped. The `comcast/scte35-go` library builds the `SpliceInfoSection`; `internal/engine/scte35.go` wraps it (`buildSpliceInsert`, `buildCueOutLine`, `buildCueInLine`, `buildMediaPlaylistBody`, `adBreakEventID`).
* Detection runs only on the **prepared** (`available`, `.pts`-marker-filtered) segments so a CUE-OUT never references a segment not in the playlist.

#### **Segment Processing Constants**

| Constant | Value | Description |
|----------|-------|-------------|
| `WindowItemSize` | 50 | Redis pipeline flush batch size |
| `AdInsertionInterval` | 30 | Seconds between ad insertions |
| `RedisExpiryHours` | 26 | Redis key expiration in hours |
| `WindowBufferSeconds` | 120 | Lookback buffer for segment inclusion (planner) |
| `RETENTION_SECONDS` | 7200 | Per-tick `ZREMRANGEBYSCORE` cutoff for `simlive:timeline:{ch}` |
| `LOOKAHEAD_SECONDS` | 7200 | Per-tick cursor bump to `nowUnix + LOOKAHEAD_SECONDS` |

### 6.1 Quality Tier & Rendition Matching (SIMLIVE_QUALITY)

| Quality | Target resolution | VOD dir | Ads dir |
|---------|-------------------|---------|---------|
| 360p    | 640×360           | `L_0`   | `L2`    |
| 720p    | 1280×720          | `L_1`   | `L1`    |
| 1080p   | 1920×1080         | `L_2`   | `L0`    |

Selection logic (`internal/planner/planner.go`):
* `SIMLIVE_QUALITY=auto` (default) — highest equal-resolution VOD==ads pair.
* Explicit tier — `resolveRenditions()`: (1) exact VOD==ads pair at the tier resolution; (2) closest VOD==ads pair; (3) min-area-diff pair. Probe failures (e.g. missing `L_3`) are skipped gracefully.
* Hard overrides `SIMLIVE_VOD_RENDITION` + `SIMLIVE_ADS_RENDITION` (both set) beat the tier.
* Ads side is resolved once per planner run by `resolveAdsRendition()`: env override > tier-closest > tier fallback map > largest (auto). **Note:** `pair.ads` in `resolveRenditions()` is log-only — real ads paths come from `resolveAdsRendition()`.
* **Invariant:** VOD and ads must resolve to the SAME resolution — a mid-stream resolution change in one MSE SourceBuffer fails playback even with continuous PTS.

---

### 7. Architectural Diagram Summary

```text
[ESPX API v2]
   │  (1. Sync Channels / EPG)
   ▼
[PostgreSQL] ──► (playout_channels)
   │
   ▼
[Planner (Cron)]
   │
   ├─► Call API 3 (Get Schedules)
   ├─► Execute `rclone cat` ──► [Storage: espx-video-lib]
   │   (Parse m3u8 & build TS timeline)
   ├─► Load Ads Manifest (rclone cat)
   └─► Save Timeline with Ads ──► [Redis Sorted Set]
                             │
                             ▼
                    [Engine (Live Loop)]
                             │
                             ├─► Clean cache on startup (preserve timeline)
                             ├─► Fetch upcoming TS via `rclone copyto`
                             ├─► Manage local disk buffer (/tmp/simlive)
                             ├─► Generate Dynamic Live m3u8 (local + Redis)
                             ├─► Remove stale files from window
                             │
                             ▼
                      [Video Players / CDN]
```

---

### 8. Configuration & Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SIMLIVE_QUALITY` | `auto` | `360p`/`720p`/`1080p` quality tier (currently `360p` in `.env`) |
| `SIMLIVE_RENDITIONS` | unset | Reserved for M2 (true per-rung separate segments). M1.5 ignores this env var and always advertises 3 rungs via the engine's hard-coded `ActiveLadder` (or planner-written `simlive:renditions:{ch}` metadata). |
| `SIMLIVE_VOD_RENDITION` | (empty) | Hard override: VOD rendition dir (e.g. `L_0`) |
| `SIMLIVE_ADS_RENDITION` | (empty) | Hard override: ads rendition dir (e.g. `L2`) |
- **REST start/stop best-effort Redis:** `POST /api/channels/:id/start` deletes the breaker counter key `planner:failures:{id}` as part of the operation. A Redis blip is logged at `[api] StartChannel: DEL ...` and the request still returns `200` — the DB flip is the durable part of the operation, the Redis clear is a "best effort" reset.
| `DB_HOST`/`DB_USER`/`DB_PASSWORD`/`DB_NAME` | localhost/postgres | Postgres (`playout_channels`) |
| `REDIS_ADDR` | `localhost:6371` | Redis (note: 6371, not 6379) |
| `ESPX_API_URL` | `https://api2.espx.cloud` | EPG API base URL |
| `TMP_DIR` | `./outputs` | Engine local segment cache (`{TMP_DIR}/{channelID}/`) |
| `MAX_WORKERS` / `WORKER_POOL_SIZE` | 50 | Download concurrency |
| `ENABLE_CLEAN_UP` | `false` | Startup cleanup toggle. When `true`, the engine `os.RemoveAll`s the `{TMP_DIR}` on startup and the planner `FlushAll`s Redis + `os.RemoveAll`s `{TMP_DIR}`. Default (`false`) preserves the on-disk cache across restarts (engine) and scopes the Redis wipe to `simlive:timeline:*` only (planner), so a restarted engine can resume immediately on whatever the previous run had downloaded. |
| `SIMLIVE_KEEP_BEHIND` | `5` | Segments retained *before* the playlist window (active on-disk window). |
| `SIMLIVE_DOWNLOAD_AHEAD` | `5` | Segments pre-fetched *after* the playlist window (active on-disk window). |
| `PLANNER_INTERVAL` | `5m` | Cron sleep between planner ticks (Go duration string, e.g. `1m`, `30s`, `1h`). Zero/negative falls back to 5m. |
| `PLANNER_WORKERS` | `4` | Max active channels processed in parallel per tick (semaphore-bounded goroutine pool, floored at 1). Safe to set > 1 because per-call walk state (`runningTime`, `totalSegmentsAdded`, `channelID`, `horizon`) lives on the `SegmentProcessor` created per call, not on the shared `Planner` (see NOTE in `internal/planner/planner.go`). Different channels therefore run concurrently; the `cmd/planner` in-flight guard still prevents overlapping ticks for the same channel. **History:** the state used to live on the shared `Planner` unlocked, which raced under parallel calls (2 active channels → only 1 had data). A stopgap `epgMu` mutex + clamp-to-1 fixed the race but serialized whole cold-start walks, so channel #2 had no data for the entire window; that stopgap was removed in favour of the per-call-state refactor. `rclone cat`/`copyto` run under hard timeouts (30s/60s) so a hung remote cannot wedge a worker. |
| `MAX_PREPARE_RETRIES` | `3` | Total attempts for `ProcessEPGToTimelineWithRetry` per (channel, date) before giving up. |
| `PREPARE_RETRY_BASE_MS` | `500` | Initial backoff delay (ms). Doubled each attempt up to the cap. |
| `PREPARE_RETRY_MAX_MS` | `8000` | Maximum backoff delay (ms). |
| `TZ_NUM` | Configurable | Timezone offset for API queries |

---

### 9. Error Handling & Resilience

- **Ad Load Failure:** Non-critical; system continues with content segments only
- **Segment Download Failure:** Logged but doesn't block the playlist generation; in-flight dedupe prevents duplicate downloads
- **Rendition Probe Failure:** Missing renditions (e.g. `L_3`) fail the probe gracefully and are skipped; per-tier fallback map used when nothing probes
- **PTS Rewrite Failure:** Anchor falls back from `PtsOffset` → `MediaStart` → `PtsStart`; if all fail the raw file is kept but still marked prepared — the window never stalls
- **Batch Flush Failures:** Individual batch failures are logged; system continues
- **Redis Empty Windows:** Fallback to slate (handled by player-side fallback)
- **End of Timeline Check:** Prevents processing beyond the configured day
- **EPG Fetch Failure (Planner):** `ProcessEPGToTimelineWithRetry` retries with bounded exponential backoff (`base * 2^(i-1)`, capped at `PREPARE_RETRY_MAX_MS`, `MAX_PREPARE_RETRIES` total attempts). Final failure increments `planner:failures:{channelID}` (TTL 24h).
- **Repeated Channel Failures:** After `failureInactivateThreshold` (5) consecutive `(channel, date)` failures the channel is marked `is_active=false` via `markChannelInactive` — the cron loop skips it on subsequent ticks. A successful prepare or a "date already prepared" short-circuit resets the counter and flips `status` to `'LIVE'` (`markChannelLive`).
- **Cron Loop Concurrency:** Per-tick channel processing is parallelised by a `PLANNER_WORKERS`-bounded goroutine pool (semaphore + WaitGroup); metrics for each active channel (`zset_size`, `cursor`) are emitted once per tick as `[METRIC] channel=...` log lines for scraping by external observability tooling. Default `PLANNER_WORKERS=4` (floored at 1 by `cmd/planner plannerWorkerCount()`). Different channels run concurrently because per-call walk state (`runningTime`, `totalSegmentsAdded`, `channelID`, `horizon`) is owned by the `SegmentProcessor` created per `ProcessEPGToTimeline` call; the shared `Planner` holds only connection handles and `rendMu`-guarded caches. A per-channel in-flight registry prevents overlapping ticks for the same channel. **History:** the planner used to hold per-call state on the shared `Planner` unlocked, which produced a classic race: with 2 active channels, the two parallel `ProcessEPGToTimeline` calls would clobber each other's `globalRunningTime` / `processDate` / `horizon` / `channelID` mid-walk, resulting in a corrupt timeline (typically: one channel's segments landing in the other's ZSET, or the EPG walk stopping early). Symptom: 2 active channels → only 1 had data. A stopgap `epgMu` mutex + clamping to 1 fixed the race but serialized whole cold-start walks (~30s–2min per date; rclone had no timeouts), so channel #2 had no data for the entire window; that stopgap was removed in favour of the per-call-state refactor.
```

# summary.md (Updated)

```markdown
# SimLive Engine & Planner Module Summary

## Overview

The SimLive system consists of two main modules: **Planner** (preparation) and **Engine** (real-time playout). The Planner builds the timeline with ad insertion, while the Engine manages HLS streaming from the prepared timeline.

---

## Module 1: Planner

### Purpose
Builds the complete timeline by fetching EPG schedules from ESPX API, parsing media segments, inserting ads, and storing the timeline in Redis.

### Key Components

#### Planner Structure
```go
type Planner struct {
    db         *gorm.DB              // PostgreSQL for channel configs
    rdb        *redis.Client         // Redis for timeline storage
    espx       *api.ESPXClient       // ESPX API client
    rclone     *storage.RcloneService // RClone for storage access
    
    // Processing state
    globalRunningTime  int64          // Current timeline position
    totalSegmentsAdded int64          // Total segments processed
    redisSyncOffset    int64          // Offset tracking for ads
    channelID          string         // Current channel being processed
    processStartTime   time.Time      // Processing start time

    // Rendition matching (quality tier)
    rendMu     sync.Mutex              // Guards rendCache
    rendCache  map[string]renditionPair // streamID -> {vod, ads} pair
    adsRend    string                  // Resolved once (ads shared across streams)
    adsRendSet bool                    // Whether adsRend was resolved
}
```

#### SegmentProcessor Structure
```go
type SegmentProcessor struct {
    planner *Planner          // Reference to parent planner
    pipe    redis.Pipeliner   // Redis pipeline for batch operations
    ctx     context.Context   // Context for operations
    
    sequenceCounter int64     // Global sequence counter
    adOffset        int64     // Total ad duration offset
    lastAdTime      int64     // Last ad insertion time
    totalAdsAdded   int       // Count of ads inserted
    nowUnix         int64     // Current Unix timestamp
    adSegments      []models.SegmentItem // Loaded ad segments

    // PTS timeline (local, seconds)
    currentPtsOffset float64  // Shift applied to raw PTS for the current program
    timelineBase     float64  // MediaStart of first segment (local anchor)
}
```

### Key Methods

#### ProcessEPGToTimeline()
Main entry point for timeline building:
1. Fetches schedules from ESPX API for the given date
2. Loads ad segments from storage (non-critical)
3. Initializes `SegmentProcessor`
4. Processes each schedule item
5. Flushes Redis pipeline

#### processScheduleItem()
- Parses start/end times for each program
- Skips already ended programs
- Updates global running time with ad offset
- Delegates to `processStreamSegments()`

#### processStreamSegments()
- Fetches and parses m3u8 manifest using `rclone cat` (VOD dir from the tier pair)
- Parses `#EXT-X-PTS-RANGE` → raw `pts_start`/`pts_end`
- Shifts to local timeline: `mediaStart = (pts_start + currentPtsOffset) / 90000`
- Processes each segment through `processSegment()`

#### processSegment()
- Increments sequence counter
- Computes `PtsOffset = setTimelineBase(mediaStart)` (MediaStart − timelineBase)
- Adds segment to Redis if within window (`now - 120s`)
- Updates global running time with segment duration
- Triggers ad insertion via `insertAdsIfNeeded()`

#### insertAdsIfNeeded()
- Checks if time to insert ads (after current time and 30s interval)
- Iterates through loaded ad segments
- Assigns each ad the same local `PtsOffset` treatment (`media_start − timelineBase`)
- Adds to Redis pipeline
- Updates ad offset and global running time

#### loadAdSegments()
- Fetches ad manifest from `ads-v3/{adsRend}/vod.m3u8` (ads rendition resolved once via `resolveAdsRendition()`)
- Parses EXTINF, segment filenames and PTS
- Returns slice of ad segments for insertion

### Processing Flow

```
[ESPX API] → [Schedule Items] → [Stream ID Extraction]
         ↓
[Storage: espx-video-lib] ← [rclone cat] ← [Parse m3u8]
         ↓
[Segment Processing] → [Ad Insertion]
         ↓
[Redis Pipeline] → [Batch Flush (every 50 items)]
         ↓
[Redis Sorted Set: simlive:timeline:{channel}]
```

---

## Module 2: Engine

### Purpose
Reads the prepared timeline from Redis and generates a live HLS stream with dynamic segment fetching and playlist management.

### Key Components

#### Engine Structure
```go
type Engine struct {
    rdb           *redis.Client          // Redis client for timeline access
    rclone        *storage.RcloneService // RClone service for file operations
    tmpDir        string                 // Local cache dir (TMP_DIR, default ./outputs)
    workerPool    chan struct{}          // Worker pool for concurrent downloads
    enableCleanup bool                   // ENABLE_CLEAN_UP toggle
    states        map[string]*channelState // Per-channel playlist state (seq tracking)
    statesMu      sync.Mutex
    inFlight      map[string]bool        // In-flight download/rewrite dedupe
    inFlightMu    sync.Mutex
}
```

### Key Methods

#### CleanChannelState()
- Removes local cache directory on startup
- **Preserves Redis timeline data** (`simlive:timeline` key)
- Clears only the HLS playlist cache key (`simlive:hls`)

#### StartPlayoutLoop()
The main infinite loop that ticks every **1 second**:

1. **Fetch Timeline Window**
   - Queries Redis for segments in `[now-300s, now+120s]` (score = `media_start`)
   - Deserializes JSON segment items

2. **Sync Local Cache**
   - Removes local `.ts`/`.pts` files not in the active window

3. **Download Missing Segments (async)**
   - Uses worker pool for concurrent `rclone copyto`
   - Skips already cached segments; in-flight dedupe prevents duplicate downloads across ticks
   - Stores as `seg_{sequence}.ts`

4. **Rewrite PTS/DTS (`rewritePtsOffset`)**
   - Probes first video DTS (ffprobe), remuxes with `-c copy -copyts -output_ts_offset` so DTS is continuous across VOD↔ads
   - Target = `seg.PtsOffset` (local timeline) → fallback `MediaStart` → fallback `PtsStart`
   - Writes `seg_{sequence}.ts.pts` marker when prepared (idempotent)

5. **Generate HLS Playlist**
   - Creates 5-segment sliding playlist; `#EXT-X-MEDIA-SEQUENCE` from first segment
   - **Only advertises segments with a `.pts` marker**; keeps previous playlist while nothing is ready
   - Writes atomically to `{TMP_DIR}/{channelID}/playlist.m3u8`

6. **Cleanup Redis**
   - Removes segments older than `now-120s`

#### syncLocalCacheDir()
- Compares active file set with existing files
- Removes `.ts` files not in the current window

#### generateHLSPlaylist()
- Builds M3U8 playlist from segment items
- Calculates target duration as max duration + 1
- Sets media sequence from first segment's sequence
- Stores in Redis with 10-second TTL

### Processing Flow

```
[Redis Timeline] ← [ZRangeByScore: now-300 to now+120]
         ↓
[Local Cache Check] → [Download Missing Segments]
         ↓
[PTS/DTS Rewrite (ffmpeg -copyts -output_ts_offset)] → [.pts marker]
         ↓
[HLS Playlist Generation (prepared segs only)] → [Local File]
         ↓
[Cleanup: Local Files + Redis Entries]
```

---

## Key Constants Comparison

| Constant | Planner | Engine | Description |
|----------|---------|--------|-------------|
| `WindowBufferSeconds` | 120 | - | Lookback buffer for segment inclusion (planner) |
| `AdInsertionInterval` | 30 | - | Seconds between ad insertions |
| `WindowItemSize` | 50 | - | Redis pipeline flush batch size |
| `RedisExpiryHours` | 26 | - | Redis key expiration in hours |
| Loop Interval | - | 1s | Engine processing tick |
| Lookback Window | - | 300s | Past segments fetched from Redis |
| Lookahead Window | - | 120s | Future segment pre-fetch |
| HLS TTL | - | 10s | Playlist Redis cache expiry |

---

## Data Models

### SegmentItem
```go
type SegmentItem struct {
    Sequence    int64    `json:"sequence"`
    StoragePath string   `json:"storage_path"`
    Duration    float64  `json:"duration"`    // Used for #EXTINF
    StartTime   int64    `json:"start_time"`  // Unix timestamp (debug/display)
    TimeText    string   `json:"time_text"`   // HH:mm display

    // PTS timeline fields
    PtsStart   int64    `json:"pts_start"`   // Raw #EXT-X-PTS-RANGE start (90kHz)
    PtsEnd     int64    `json:"pts_end"`     // Raw #EXT-X-PTS-RANGE end (90kHz)
    MediaStart float64  `json:"media_start"` // PtsStart/90000 + offset (seconds)
    MediaEnd   float64  `json:"media_end"`   // PtsEnd/90000 + offset (seconds)

    // Local timeline position: MediaStart - timelineBase (seconds).
    // Engine rewrites PTS/DTS to this value; nil on legacy segments
    // (engine falls back to MediaStart).
    PtsOffset  *float64 `json:"pts_offset,omitempty"`

    // M1.5: per-rung dispatch tag. Empty on legacy members; engine
    // rescues to DefaultRendition ("360p").
    Rendition  string   `json:"rendition,omitempty"`

    // M-SCTE35: ad-break metadata stamped by insertAdsIfNeeded (see
    // §6.1a). All omitempty so VOD members round-trip to JSON
    // unchanged and pre-existing timeline entries remain valid
    // without a flush.
    IsAd            bool    `json:"is_ad,omitempty"`
    AdBreakID       string  `json:"ad_break_id,omitempty"`         // "adbreak-<int64(breakStart)>"
    AdBreakStart    float64 `json:"ad_break_start,omitempty"`      // unix seconds; → START-DATE
    AdBreakDuration float64 `json:"ad_break_duration,omitempty"`   // seconds; → DURATION + SCTE-35 BreakDuration
}
```

### ESPXScheduleItem
```go
type ESPXScheduleItem struct {
    LocalDateStart string
    LocalDateEnd   string
    Programme      struct {
        ProgrammeStreams []struct {
            StreamID string
        }
    }
}
```

---

## Redis Keys

| Key Pattern | Type | Description |
|-------------|------|-------------|
| `simlive:timeline:{channel_id}` | Sorted Set | Complete timeline with score = start_time |
| `simlive:hls:{channel_id}` | String | Cached HLS playlist (10s TTL) |

---

## Storage Paths

| Path Pattern | Module | Description |
|--------------|--------|-------------|
| `{bucket}/{streamID}/{vodDir}/vod.m3u8` | Planner | Content m3u8 manifest (vodDir per tier: `L_0`/`L_1`/`L_2`) |
| `{adsBucket}/{adsRend}/vod.m3u8` | Planner | Ad manifest (adsRend per tier: `L0`/`L1`/`L2`) |
| `{bucket}/{streamID}/{vodDir}/{segment}.ts` | Both | Remote segment storage |
| `{tmpDir}/{channelID}/seg_{seq}.ts` | Engine | Local cached segment |
| `{tmpDir}/{channelID}/seg_{seq}.ts.pts` | Engine | Prepared marker (PTS rewrite done) |
| `{tmpDir}/{channelID}/playlist.m3u8` | Engine | Local HLS playlist |

---

## Error Handling Patterns

### Planner
- **Ad load failure:** Log warning, continue with empty ads
- **Schedule item failure:** Log warning, continue to next item
- **Batch flush failure:** Log error, attempt to continue
- **End of timeline:** Stop processing to prevent overflow

### Engine
- **Empty timeline:** Continue loop (fallback handled client-side)
- **Download failure:** Log error, skip segment
- **Playlist generation failure:** Log error, preserve previous playlist
- **Redis operations:** Non-blocking with error logging

---

## Performance Considerations

### Planner
- **Pipeline batching:** Flushes every 50 items to reduce Redis round-trips
- **Ad pre-loading:** Ads loaded once per channel processing
- **Selective insertion:** Only segments in window are added to Redis

### Engine
- **Worker Pool:** Limits concurrent downloads per channel
- **Batch Processing:** Each loop iteration handles multiple segments
- **Cache Efficiency:** Existing segments skip download
- **Disk:** Automatic cleanup prevents unlimited storage growth
- **Memory:** JSON deserialization only for active window
```