## 🇬🇧 English Version: SimLive Project Specification & Architecture (Updated)

### 1. High-Level Architecture Overview

The system operates as a 24/7 linear playout engine (Simulated Live Streaming):

1. **PrepareSimlive (Planner):** Periodically queries the ESPX API to fetch EPG schedules, maps program items to `.ts` files using `rclone`, and writes the timeline metadata to **Redis**.
2. **StartSimlive (Engine):** Reads mapped segment timelines from Redis using real-time system clocks, downloads required `.ts` files to a local sliding window directory, stitches them into a dynamic HLS stream (`playlist.m3u8`), and serves video players.

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
* `programme.short_url` (Stream ID): Base directory path on storage (e.g., `PCwdH9LA7uS6jYJDDsmZCL3x`).



---

### 4. Media Storage Access via RClone

After extracting the Stream ID (e.g., `PCwdH9LA7uS6jYJDDsmZCL3x`), the Planner executes **`rclone cat`** via Go's `os/exec` to parse HLS manifests and extract `.ts` segment lists:

1. **Read Master / Variant Playlist:**
```bash
rclone cat espx-video-lib:espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/vod.m3u8
rclone cat espx-video-lib:espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/L_0/vod.m3u8

```


2. **Parse Manifest Contents:**
* Extract segment file names (`segment_000.ts`, `segment_001.ts`) and target durations (`#EXTINF:XX.X`).


3. **Download Segment TS (Engine Phase):**
* Download `.ts` files to a temporary local cache when entering the sliding window:


```bash
rclone copyto espx-video-lib:espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/L_0/segment_000.ts /tmp/simlive/channel1/segment_000.ts

```



---

### 5. Detailed System Modules

#### **Module 1: PrepareSimlive (Async Planner - Cron Job)**

Runs periodically (e.g., every 30-60 minutes) or at start-of-day:

1. Query active channels from `playout_channels`.
2. Call **API 3** to retrieve today's and tomorrow's EPG schedules using the channel's timezone.
3. For each schedule item:
* Read Stream ID (`programme.short_url`).
* Execute `rclone cat` to read variant `m3u8` contents.
* Compute absolute timeline timestamps for each `.ts` file based on `local_date_start` + accumulated segment durations.


4. Push segment mappings into a **Redis Sorted Set** (`ZADD` with Epoch timestamp as score):
* **Key:** `simlive:timeline:{playout_id}`
* **Value Structure (JSON):**
```json
{
  "sequence": 1052,
  "storage_path": "espx-video-lib/PCwdH9LA7uS6jYJDDsmZCL3x/L_0/segment_001.ts",
  "duration": 6.006,
  "start_time": "2026-08-23T20:00:00+07:00"
}

```





#### **Module 2: StartSimlive (Live Engine & HLS Generator)**

An infinite real-time loop managing playout state:

1. **Clock Sync:** Check current system epoch timestamp (`currentTime`).
2. **Sliding Window Selector:**
* Query Redis for segments within `[currentTime - 30s, currentTime + 30s]`.


3. **Segment Fetcher (Worker Pool):**
* Inspect local cache directory `/tmp/simlive/{playout_id}/`.
* Use a worker pool (e.g., max 5 goroutines per channel) to run `rclone copyto` and prebuffer upcoming segments.


4. **HLS Dynamic Playlist Builder:**
* Generate live `playlist.m3u8` directly in memory or Redis:
* Retain 3–5 current/upcoming segments.
* Increment `#EXT-X-MEDIA-SEQUENCE`.




5. **Fallback / Slate Injector:**
* If Redis contains schedule gaps or `rclone` downloads fail, inject standby video segments (`slate_loop.ts`) to maintain HLS playback continuity.


6. **Garbage Collection (Clean Up):**
* Delete local `.ts` files older than `start_time + duration < currentTime - 60s`.
* Purge expired items from Redis.



---

### 6. Architectural Diagram Summary

```text
[ESPX API v2]
   │  (1. Sync Channels / EPG)
   ▼
[PostgreSQL] ──► (playout_channels)
   │
   ▼
[PrepareSimlive (Cron)]
   │
   ├─► Call API 3 (Get Schedules)
   ├─► Execute `rclone cat` ──► [Storage: espx-video-lib]
   │   (Parse m3u8 & build TS timeline)
   │
   └─► Save Timeline ──► [Redis Sorted Set]
                             │
                             ▼
                    [StartSimlive (Engine)]
                             │
                             ├─► Fetch upcoming TS via `rclone copyto`
                             ├─► Manage local disk buffer (/tmp/simlive)
                             ├─► Generate Dynamic Live m3u8
                             │
                             ▼
                      [Video Players / CDN]

```