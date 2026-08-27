# specification.md (Updated)

> Status: reflects the **implemented** system as of 2026-08-27 — 3-tier quality
> selection (`SIMLIVE_QUALITY`), VOD↔ads rendition matching, local `PtsOffset`
> timeline, and ffmpeg PTS/DTS rewrite in the engine. For current status and
> gotchas see `docs/HANDOFF.md` and `AGENTS.md`.

```markdown
## 🇬🇧 English Version: SimLive Project Specification & Architecture (Updated)

### 1. High-Level Architecture Overview

The system operates as a 24/7 linear playout engine (Simulated Live Streaming):

1. **Planner (PrepareSimlive):** Periodically queries the ESPX API to fetch EPG schedules, maps program items to `.ts` files using `rclone`, and writes the timeline metadata to **Redis** with ad insertion capabilities.
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
    status VARCHAR(20) DEFAULT 'ACTIVE', -- Status: ACTIVE, INACTIVE, ERROR
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

---

### 3. ESPX API Integration & Data Flow

#### **API 1: Get Playout Channels List**

* **Endpoint:** `GET [https://api2.espx.cloud/api/v2/common/channel-playouts](https://api2.espx.cloud/api/v2/common/channel-playouts)`
* **Purpose:** Fetch all available playout channels to seed or synchronize the `playout_channels` database table.

#### **API 2: Get Playout EPG ID**

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

Runs periodically (e.g., every 30-60 minutes) or at start-of-day:

1. Query active channels from `playout_channels`.
2. Call **API 3** to retrieve today's and tomorrow's EPG schedules using the channel's timezone.
3. For each schedule item:
* Read Stream ID (`programme.short_url`).
* Execute `rclone cat` to read variant `m3u8` contents.
* Compute timeline timestamps for each `.ts` file from the manifest's `#EXT-X-PTS-RANGE` (raw PTS) shifted to a **local timeline**: every segment gets `PtsOffset = MediaStart − timelineBase` (timelineBase = first segment's MediaStart, in seconds). Local offsets avoid the 2^33/90000 ≈ 95443.7s PTS wrap of huge epoch offsets.
* **Ad Insertion:** Periodically insert ad segments at configured intervals (`AdInsertionInterval` = 30 seconds); ads are placed on the same local timeline.
* **Quality tier:** resolve `SIMLIVE_QUALITY` (`auto`|`360p`|`720p`|`1080p`) and pick the VOD/ads rendition pair matching the tier resolution (see §6.1).


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


5. **Redis Pipeline Batching:** Batch insert operations every 50 items to optimize Redis performance.

#### **Module 2: Engine (StartSimlive - Live Engine & HLS Generator)**

An infinite 1-second tick loop managing playout state:

1. **Clean Channel State:** On startup, delete the local cache directory (`TMP_DIR`) while preserving the Redis timeline; also deletes `.ts`/`.pts` files that leave the active window.
2. **Clock Sync:** Check current system epoch timestamp (`currentTime`).
3. **Sliding Window Selector:**
* Query Redis for segments within `[now - 300s, now + 120s]` (score = `media_start`).


4. **Segment Fetcher (Worker Pool, async):**
* Inspect local cache directory `{TMP_DIR}/{playout_id}/`.
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
* Delete local `.ts`/`.pts` files no longer in the active window.
* Purge expired items from Redis older than 120 seconds.



---

### 6. Key Implementation Details

#### **Redis Data Structure**

* **Key:** `simlive:timeline:{channel_id}`
* **Type:** Sorted Set
* **Score:** `MediaStart` (segment start on the local timeline, seconds)
* **Member:** JSON-encoded `SegmentItem`
* **Expiry:** 26 hours

#### **Ad Insertion Logic**

* Load ad segments from `espx-video-lib:espx-video-lib/ads-v3/{adsRend}/vod.m3u8`, where `{adsRend}` is chosen by `resolveAdsRendition()` (see §6.1)
* Insert ads when:
1. Segment start time >= current time
2. Elapsed time since last ad >= `AdInsertionInterval` (30 seconds)
* Ads are inserted between content segments, get the same local `PtsOffset` treatment, and increment both global time and ad offset

#### **Segment Processing Constants**

| Constant | Value | Description |
|----------|-------|-------------|
| `WindowItemSize` | 50 | Redis pipeline flush batch size |
| `AdInsertionInterval` | 30 | Seconds between ad insertions |
| `RedisExpiryHours` | 26 | Redis key expiration in hours |
| `WindowBufferSeconds` | 120 | Lookback buffer for segment inclusion (planner) |

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
| `SIMLIVE_VOD_RENDITION` | (empty) | Hard override: VOD rendition dir (e.g. `L_0`) |
| `SIMLIVE_ADS_RENDITION` | (empty) | Hard override: ads rendition dir (e.g. `L2`) |
| `DB_HOST`/`DB_USER`/`DB_PASSWORD`/`DB_NAME` | localhost/postgres | Postgres (`playout_channels`) |
| `REDIS_ADDR` | `localhost:6371` | Redis (note: 6371, not 6379) |
| `ESPX_API_URL` | `https://api2.espx.cloud` | EPG API base URL |
| `TMP_DIR` | `./outputs` | Engine local segment cache (`{TMP_DIR}/{channelID}/`) |
| `MAX_WORKERS` / `WORKER_POOL_SIZE` | 50 | Download concurrency |
| `ENABLE_CLEAN_UP` | `false` | Cache cleanup toggle |
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