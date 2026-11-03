# Quickstart: 24/7 Playout EPG Cache Fallback

## Prerequisites

- Go 1.26+; `make build` produces `config/bin/`.
- Postgres + Redis running (`config/docker-compose.yml`).
- `rclone` configured for the VOD bucket (real planning needs it).
- A test channel with a known `channel_epg_id` and `content_timezone`.
- A stub schedule provider (any tiny HTTP server) that can switch between:
  - `{"date":"YYYY-MM-DD","total_items":0,"data":[]}` (empty)
  - a valid non-empty schedule payload for the requested `$date`

## Setup

> **Day boundary**: every scenario uses the host-local day. Set `TZ` explicitly (e.g. `export TZ=UTC`) so day boundaries are deterministic across machines; `configs.TZ` is only a fallback. Do not assume the channel's `content_timezone` defines the target day.

```bash
export TMP_DIR=$(mktemp -d)
export EPG_CACHE_DIR="$TMP_DIR/epg-cache"
export PARTNER_URL=http://127.0.0.1:9099      # stub provider
export PLANNER_INTERVAL=30s
```

Start the stub provider, then start the planner:

```bash
./config/bin/planner
```

## Validation scenarios

### 1. Retained schedule is written from real data

1. Stub returns a valid schedule for the requested date.
2. Let the planner plan the channel (or trigger a tick).
3. Verify `$EPG_CACHE_DIR/<channel_id>.json` exists and contains the returned items.
4. Restart the planner; verify the file is still present (no `ENABLE_CLEAN_UP` wipe).

### 2. Previous day's cache covers an empty next-day fetch

1. Confirm a retained schedule exists (scenario 1).
2. Make the stub return an empty payload for the next day.
3. Let the planner run through the next-day preparation window (or force a tick).
4. Verify:
   - planner logs a fallback event with channel, source day, and target day;
   - `redis-cli HGETALL planner:fallback:<channel_id>` has the target day field;
   - `redis-cli ZCARD simlive:timeline:<channel_id>` covers the target day end;
   - timeline entries have strictly increasing `sequence` and continuous `running_time`.
5. Play the HLS stream across the day boundary; verify playback does not stop.

### 3. Empty response does not clobber the cache

1. Capture the cache file's checksum.
2. Make the stub return empty and let the planner tick.
3. Verify the checksum is unchanged.

### 4. Automatic recovery purges unplayed fallback coverage

1. Put the channel on a fallback day (scenario 2).
2. Switch the stub to return a valid, different schedule for that day.
3. Let one planning cycle run.
4. Verify:
   - the fallback field for that day is gone from the Redis hash;
   - previously played entries (score < now − `WindowBufferSeconds`) remain;
   - unplayed entries were replaced by segments derived from the new schedule;
   - sequence numbers and running time remain strictly increasing;
   - the cache file now holds the new provider schedule.

### 5. No retained schedule ⇒ visible failure

1. Clear `$EPG_CACHE_DIR` and `redis-cli HDEL planner:fallback:<channel_id>` (or delete the key).
2. Make the stub return empty.
3. Verify the planner reports a no-data failure (failure counter increments, channel status reflects failure after the threshold) instead of marking the day prepared.

### 6. Admin label in the timeline UI

1. Put the channel on a fallback day that covers the current time.
2. Open the admin Running Timeline screen.
3. Verify the channel card shows a fallback label with the source date.
4. Confirm channels running on real data show no label.

### 7. Multi-day outage (30 consecutive days)

1. With a retained schedule in place, keep the stub returning empty.
2. Advance the host clock (container/VM clock shift or a loops harness) one day at a time for 30 days, letting the planner plan each target day.
3. Verify for every day:
   - the channel still has coverage through that day's end;
   - `sequence` and `running_time` remain strictly increasing across all boundaries;
   - the retained schedule file is unchanged (no age expiry);
   - no operator action was required.

### 8. Planner restarts mid-day while on fallback

1. Put the channel on a fallback day whose coverage is currently playing.
2. Stop and restart the planner without clearing cache or Redis.
3. Verify the channel continues without a gap or duplicate segments, the fallback marker is still present for the day, and the admin label remains visible.

## Unit tests

```bash
go test ./internal/planner/...
```

Covers: cache round-trip, atomic write, identity mismatch, corrupt file, no-overwrite-on-empty, fallback builder full-day coverage and repetition, zero-duration guard.

## Integration notes

- Constitution Principle III requires the day-boundary and recovery scenarios to be run against planner + engine + Redis + Postgres, not just unit tests.
- Delete `$EPG_CACHE_DIR` to reset the feature; there is no age-based expiry by design.
