Based on the provided code and the requirement for 24/7 continuous operation, the current planner implementation has several limitations:

- **Single‑pass processing**: `ProcessEPGToTimeline` processes the entire day’s schedule in one run, then stops.
- **No cleanup**: Segments accumulate indefinitely in Redis (only a global expiry of 26 hours, but that still leaves a huge dataset).
- **No handover to next day**: When the end of day is reached, no new segments are added, causing a gap for engines.
- **Potential duplication**: If the loop were uncommented and run again, duplicate segments would be added.

Below is a comprehensive plan to make the planner robust for 24/7 operation.

---

## 1. Incremental, Continuous Processing

Instead of processing the whole day in one batch, run the planner in a loop (every **5‑10 minutes**) and process only a **lookahead window** of upcoming segments.

### 1.1 Maintain a Processing Cursor
- For each channel, store the **last processed time** (or sequence number) in Redis, e.g.,  
  `planner:cursor:{channelID}` → Unix timestamp.
- Each run:
  - Fetch the current time `now`.
  - Determine the lookahead: `lookahead = now + 2 * WindowBufferSeconds` (or a fixed duration like 2 hours).
  - If the cursor is older than `now` (i.e., we fell behind), set cursor = `now` to catch up.
  - Process only schedule events that fall between `cursor` and `lookahead`.
  - Update the cursor to `lookahead` (or the actual end time of the last added segment).

### 1.2 Use Idempotent Addition
- When adding a segment, check if it already exists by its start time or sequence number.  
  This avoids duplicates if the planner restarts or runs overlapping windows.
- Sequence numbers should be globally incremented per channel; use `ZINCRBY` or a separate counter.

### 1.3 Multi‑Day Handling
- The EPG API (`GetSchedules`) accepts a date string. Fetch:
  - Today’s schedule (for segments up to end of day).
  - Tomorrow’s schedule if the lookahead reaches beyond midnight.
- When the cursor approaches end‑of‑day (e.g., within **2 hours** of midnight), start adding segments from tomorrow’s schedule as well.

---

## 2. Redis Cleanup (Remove Old Segments)

Engines consume segments in real‑time, so we must delete segments that are no longer needed to keep the sorted set size manageable.

### 2.1 Define a Retention Window
- Keep only segments within a **sliding window** of e.g., **2 hours** behind the current time (`WindowBufferSeconds` = 120s is too small for catch‑up; use a configurable retention, say `RETENTION_SECONDS = 7200`).
- Alternatively, keep the last N segments (e.g., 1000) per channel, but time‑based cleanup is simpler.

### 2.2 Periodic Cleanup Job
- In the main loop (or a separate goroutine), run a cleanup every minute:
  - For each active channel, compute `cutoff = time.Now().Unix() - RETENTION_SECONDS`.
  - Execute `ZREMRANGEBYSCORE` on the channel’s timeline key with `-inf` to `cutoff`.
- Do this **before** adding new segments to keep the key small.

### 2.3 Avoid Deleting Actively Used Segments
- Ensure `RETENTION_SECONDS` is large enough to cover any buffering in engines (e.g., DVR or catch‑up).  
  A value of 2‑4 hours is typical.

---

## 3. Handling the End‑of‑Day Transition

To ensure there is **no gap** for engines:

### 3.1 Preload Next Day’s Schedule
- When processing the lookahead, if the lookahead exceeds midnight, fetch the next day’s EPG.
- Add those segments with their correct absolute timestamps (Unix time) so they follow seamlessly.

### 3.2 Cursor Beyond Midnight
- The cursor should continue beyond midnight; no special handling is needed as long as we have the schedule for the next day.

### 3.3 Recovery on Planner Restart
- If the planner restarts, it should read the last cursor from Redis and resume from there, or fall back to `now` if the cursor is too far behind.

---

## 4. Concurrency and Performance

- The planner processes multiple channels in parallel (using goroutines) but with a limit to avoid overloading the system.
- Use a **worker pool** to process channels concurrently.
- For each channel, pipeline Redis commands to reduce round‑trips.
- Cache EPG responses to avoid repeated API calls within the same run.

---

## 5. Implementation Outline

### Modified `main.go` Loop
```go
for {
    var activeChannels []models.PlayoutChannel
    db.Where("is_active = ?", true).Find(&activeChannels)

    var wg sync.WaitGroup
    for _, ch := range activeChannels {
        wg.Add(1)
        go func(ch models.PlayoutChannel) {
            defer wg.Done()
            p := planner.NewPlanner(db, rdb, espxClient, rcloneSvc)
            // Process up to next 2 hours, including tomorrow if needed
            p.ProcessWindow(ctx, ch)
            // Cleanup old segments for this channel
            p.CleanupOldSegments(ctx, ch.ID)
        }(ch)
    }
    wg.Wait()

    time.Sleep(5 * time.Minute) // adjustable
}
```

### New `ProcessWindow` Method
- Reads cursor from Redis.
- Fetches today’s and tomorrow’s schedules.
- Iterates over schedule items within the window.
- Adds segments (with rendition selection, etc.) to Redis using pipeline.
- Updates cursor.

### New `CleanupOldSegments` Method
- Deletes segments with score < (now - retention) using `ZREMRANGEBYSCORE`.

---

## 6. Configuration

Add environment variables:
- `PLANNER_INTERVAL` (default 5m)
- `RETENTION_SECONDS` (default 7200)
- `LOOKAHEAD_SECONDS` (default 7200)

---

## 7. Error Handling and Monitoring

- Log errors when EPG fetch fails; retry with backoff.
- If a channel fails repeatedly, mark it as inactive or alert.
- Expose metrics (Redis size, last processed time) for monitoring.

---

This plan ensures:
- **Continuous availability** of segments for engines.
- **Redis memory** is bounded by cleanup.
- **Seamless transition** between days.

The changes can be implemented incrementally, starting with the cleanup and then refactoring the processing logic to be incremental.