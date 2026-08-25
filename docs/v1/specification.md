## 🇬🇧 English Version: SimLive Project Specification & Architecture

### 1. Infrastructure

* **Language/Framework:** Golang with Gin Gonic (API)
* **Database:** PostgreSQL (Metadata, channel configurations) + **GORM** (ORM)
* **Cache/State:** **Redis** (TS mapping, session state, live stream state) *(Added)*
* **Media Tools:** FFmpeg, TSDuck, FFprobe (Executed via Go `os/exec`)
* **Job/Queue:** Asynq or Go-cron (For managing background scheduled tasks) *(Added)*
* **Deployment:** Docker & `docker-compose.yml` (App, Postgres, Redis)

### 2. Optimizations & Missing Pieces (Critical Additions)

To ensure the 24/7 system runs smoothly, the following missing pieces have been added to your original scope:

* **Fallback Mechanism (Slate Video):** If the EPG data is empty or a `.ts` download fails, the system must not crash. It should inject a looping standby video (Slate/B-roll) to keep the HLS connection alive.
* **Sliding Window Download:** Avoid downloading all TS files at once. Implement a sliding window approach (downloading only the next 3-5 segments ahead of playout time) to save bandwidth and disk space.
* **Garbage Collection:** Auto-delete old, already-played `.ts` files from the disk and clear expired keys from Redis to prevent Disk Full or Memory Limit errors.
* **Worker Pool:** Limit the number of concurrent goroutines downloading `.ts` files to prevent network/CPU bottlenecks when running multiple channels.

### 3. Detailed Flow

#### Module 1: Core API & Management

* Provide RESTful endpoints for Live Playout CRUD operations (Create, Read, Update, Delete, Start, Stop).
* Store channel and EPG metadata in PostgreSQL.
* Serve the live `master.m3u8` and `playlist.m3u8` directly to video players.

#### Module 2: PrepareSimlive (The Planner)

*Runs asynchronously via Cron (e.g., every 30 minutes) to prepare the next few hours of data.*

* **Pull Data:** Scan active channels, fetch EPG data via HTTP (`$eager=[programme...]`, limited by date).
* **Fetch & Parse:** Get the m3u8 URL of each programme -> Download playlist -> Extract original `.ts` URLs.
* **Mapping:** Generate continuous sequential IDs (e.g., `channel1_seq_1.ts`, `channel1_seq_2.ts`).
* **Store:** Save the mapping (`channel1_seq_1.ts` -> `https://origin-server/real-path/segment.ts`) and TS metadata (duration, EXTINF) into **Redis**.
* **Wait & Retry:** If no data is available for a time slot, flag the channel as *Waiting* and retry after `X` minutes.

#### Module 3: StartSimlive (The Engine)

*An infinite loop responsible for the actual live HLS packaging.*

* **Manifest Generator:** Continuously generate/update a live `m3u8` manifest (stored in memory/Redis), maintaining a sliding window of the latest 3-5 segments and incrementing the sequence number.
* **Segment Downloader (Worker Pool):** Read mappings from Redis, eagerly download upcoming `.ts` chunks to a temporary directory, or stream them on the fly.
* **Media Processing:** (Optional) Use FFmpeg/TSDuck to normalize `.ts` files (ensure matching bitrate/resolution) to prevent player glitches when transitioning between different programmes.
* **Cleanup:** Delete expired `.ts` files from the local storage once they fall out of the live m3u8 window.