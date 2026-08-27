# specification.md (Updated)

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
* The system supports dynamic ad insertion by parsing a separate ad manifest (`ads-v3/L0/vod.m3u8`).


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
* Compute absolute timeline timestamps for each `.ts` file based on `local_date_start` + accumulated segment durations.
* **Ad Insertion:** Periodically insert ad segments at configured intervals (every 120 seconds).


4. Push segment mappings into a **Redis Sorted Set** (`ZADD` with Epoch timestamp as score):
* **Key:** `simlive:timeline:{playout_id}`
* **Value Structure (JSON):**
```json
{
  "sequence": 1052,
  "storage_path": "espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/L_0/segment_001.ts",
  "duration": 6.006,
  "start_time": 1234567890
}

```


5. **Redis Pipeline Batching:** Batch insert operations every 50 items to optimize Redis performance.

#### **Module 2: Engine (StartSimlive - Live Engine & HLS Generator)**

An infinite real-time loop managing playout state:

1. **Clean Channel State:** On startup, clean local cache directory and HLS Redis key while preserving timeline data.
2. **Clock Sync:** Check current system epoch timestamp (`currentTime`).
3. **Sliding Window Selector:**
* Query Redis for segments within `[currentTime - 120s, currentTime + 60s]`.


4. **Segment Fetcher (Worker Pool):**
* Inspect local cache directory `/tmp/simlive/{playout_id}/`.
* Use a worker pool (configurable max goroutines) to run `rclone copyto` and prebuffer upcoming segments.
* Segments are stored as `seg_{sequence}.ts` in the local cache.


5. **HLS Dynamic Playlist Builder:**
* Generate live `playlist.m3u8` with 3-5 segments in the `[now-20s, now+10s]` window.
* Increment `#EXT-X-MEDIA-SEQUENCE` based on the first segment's sequence.
* Write playlist to both local disk and Redis.


6. **Garbage Collection (Clean Up):**
* Delete local `.ts` files no longer in the active window.
* Purge expired items from Redis older than 120 seconds.



---

### 6. Key Implementation Details

#### **Redis Data Structure**

* **Key:** `simlive:timeline:{channel_id}`
* **Type:** Sorted Set
* **Score:** Unix timestamp (start time of segment)
* **Member:** JSON-encoded `SegmentItem`
* **Expiry:** 26 hours

#### **Ad Insertion Logic**

* Load ad segments from `espx-video-lib:espx-video-lib/ads-v3/L0/vod.m3u8`
* Insert ads when:
1. Current segment time >= current time
2. Elapsed time since last ad >= 120 seconds
* Ads are inserted between content segments and increment both global time and ad offset

#### **Segment Processing Constants**

| Constant | Value | Description |
|----------|-------|-------------|
| `WindowItemSize` | 50 | Redis pipeline flush batch size |
| `AdInsertionInterval` | 120 | Seconds between ad insertions |
| `RedisExpiryHours` | 26 | Redis key expiration in hours |
| `WindowBufferSeconds` | 120 | Lookback buffer for segment inclusion |

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
| `TMP_DIR` | OS Temp | Directory for local segment cache |
| `MAX_WORKERS` | Configurable | Maximum concurrent downloads per channel |
| `TZ_NUM` | Configurable | Timezone offset for API queries |

---

### 9. Error Handling & Resilience

- **Ad Load Failure:** Non-critical; system continues with content segments only
- **Segment Download Failure:** Logged but doesn't block the playlist generation
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
- Fetches and parses m3u8 manifest using `rclone cat`
- Extracts segment duration and filenames
- Processes each segment through `processSegment()`

#### processSegment()
- Increments sequence counter
- Calculates actual start time from global running time
- Adds segment to Redis if within window (`now - 120s`)
- Updates global running time with segment duration
- Triggers ad insertion via `insertAdsIfNeeded()`

#### insertAdsIfNeeded()
- Checks if time to insert ads (after current time and 120s interval)
- Iterates through loaded ad segments
- Sets sequence and start time for each ad
- Adds to Redis pipeline
- Updates ad offset and global running time

#### loadAdSegments()
- Fetches ad manifest from `ads-v3/L0/vod.m3u8`
- Parses EXTINF and segment filenames
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
    rdb        *redis.Client          // Redis client for timeline access
    rclone     *storage.RcloneService // RClone service for file operations
    tmpDir     string                 // Temporary directory for local cache
    workerPool chan struct{}          // Worker pool for concurrent downloads
}
```

### Key Methods

#### CleanChannelState()
- Removes local cache directory on startup
- **Preserves Redis timeline data** (`simlive:timeline` key)
- Clears only the HLS playlist cache key (`simlive:hls`)

#### StartPlayoutLoop()
The main infinite loop that runs every 2 seconds:

1. **Fetch Timeline Window**
   - Queries Redis for segments in `[now-120s, now+60s]`
   - Deserializes JSON segment items

2. **Sync Local Cache**
   - Removes local `.ts` files not in the active window

3. **Download Missing Segments**
   - Uses worker pool for concurrent downloads
   - Skips already cached segments
   - Stores as `seg_{sequence}.ts`

4. **Generate HLS Playlist**
   - Creates playlist with 3-5 segments in `[now-20s, now+10s]`
   - Writes to both local disk and Redis

5. **Cleanup Redis**
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
[Redis Timeline] ← [ZRangeByScore: now-120 to now+60]
         ↓
[Local Cache Check] → [Download Missing Segments]
         ↓
[HLS Playlist Generation] → [Local File + Redis Cache]
         ↓
[Cleanup: Local Files + Redis Entries]
```

---

## Key Constants Comparison

| Constant | Planner | Engine | Description |
|----------|---------|--------|-------------|
| `WindowBufferSeconds` | 120 | 120 | Lookback buffer for segment inclusion |
| `AdInsertionInterval` | 120 | - | Seconds between ad insertions |
| `WindowItemSize` | 50 | - | Redis pipeline flush batch size |
| `RedisExpiryHours` | 26 | - | Redis key expiration in hours |
| Loop Interval | - | 2s | Engine processing tick |
| Lookahead Window | - | 60s | Future segment pre-fetch |
| HLS TTL | - | 10s | Playlist Redis cache expiry |

---

## Data Models

### SegmentItem
```go
type SegmentItem struct {
    Sequence    int64   `json:"sequence"`
    StoragePath string  `json:"storage_path"`
    Duration    float64 `json:"duration"`
    StartTime   int64   `json:"start_time"`
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
| `{bucket}/{streamID}/L_0/vod.m3u8` | Planner | Content m3u8 manifest |
| `{adsBucket}/L0/vod.m3u8` | Planner | Ad manifest |
| `{bucket}/{streamID}/L_0/{segment}.ts` | Both | Remote segment storage |
| `{tmpDir}/{channelID}/seg_{seq}.ts` | Engine | Local cached segment |
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