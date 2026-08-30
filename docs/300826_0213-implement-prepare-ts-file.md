# Engine Download/Cleanup Window Refinement

**Plan version:** 1.0  
**Date:** 2026-08-29  
**Author:** (engine team)  

---

## Overview

The engine currently keeps a large set of `.ts` segments on disk (up to ~420 files per channel) because it downloads and retains everything returned by the Redis timeline window (`[-300s, +120s]`). The playlist advertises only 5 segments, but the cache grows unnecessarily, causing disk bloat and longer startup cleanup.

**Goal:** Reduce on‑disk storage to a **fixed sliding window of 15 segments** per channel:
- **5 old** (behind the current playhead) – for backwards seek/continuity  
- **5 current** (the playlist window) – advertised to players  
- **5 ahead** (pre‑fetched) – to avoid download delays  

All other files are **evicted every tick** based on the active set, ensuring disk usage stays bounded regardless of timeline length or previous runs.

---

## Current Behavior vs. Proposed

| Aspect | Current | Proposed |
|--------|---------|----------|
| **Redis query** | `ZRangeByScore [-300, +120]` → up to ~420 segments | unchanged (still queries the same window for timeline awareness) |
| **Download set** | all segments from Redis | only the **15‑segment active window** (`keepBehind` + `PlaylistWindow` + `downloadAhead`) |
| **Playlist** | 5 segments (`candidateSegs`) | unchanged – 5 segments |
| **Cleanup** | based on all Redis segments (so it deletes nothing because all are “active”) | based on the 15‑segment active set – everything else is removed |
| **On‑disk files** | ~420 `.ts` + `.pts` | ≤15 `.ts` + ≤15 `.pts` + playlist + temp |

---

## Design

### Constants and Configuration

```go
const (
    DefaultKeepBehind    = 5   // number of segments to keep before the playlist start
    DefaultDownloadAhead = 5   // number of segments to pre-fetch after the playlist end
)

type Engine struct {
    // ... existing fields
    keepBehind    int
    downloadAhead int
}
```

Environment variables (read in `NewEngine`):
- `SIMLIVE_KEEP_BEHIND` (default `5`) – segments retained *before* the current playlist window  
- `SIMLIVE_DOWNLOAD_AHEAD` (default `5`) – segments pre‑fetched *after* the playlist window  

If `envs.go` lacks `GetEnvInt`, add a simple helper:

```go
func GetEnvInt(key string, defaultVal int) int {
    if v := os.Getenv(key); v != "" {
        if i, err := strconv.Atoi(v); err == nil {
            return i
        }
    }
    return defaultVal
}
```

### Window Calculation (inside the tick loop)

After determining `currentIndex` (the segment containing `nowFloat`), compute the **on‑disk active range**:

```go
// Playlist window (advertised to player) – stays 5
playlistWindow := 5

// Active set to keep on disk:
startIdx := currentIndex - e.keepBehind
if startIdx < 0 {
    startIdx = 0
}
endIdx := currentIndex + playlistWindow + e.downloadAhead - 1
if endIdx >= len(allSegs) {
    endIdx = len(allSegs) - 1
}

downloadSegs := allSegs[startIdx : endIdx+1]
```

**What this keeps:**
- Segments with indices `[startIdx … currentIndex-1]` → old (behind)  
- `[currentIndex … currentIndex+playlistWindow-1]` → playlist window (still 5)  
- `[currentIndex+playlistWindow … endIdx]` → ahead (pre‑fetched)  

Total = `keepBehind + playlistWindow + downloadAhead` (default 5+5+5 = 15).

### Download & Cleanup

- **Download:** Call `e.downloadAndPrepareSegments(channelDir, channelID, downloadSegs)` instead of passing `allSegs`.  
- **Active Map for Cleanup:** Build `activeMap` from `downloadSegs` (not `allSegs`).  
- **Playlist Generation:** Remains unchanged – uses `candidateSegs` (the 5‑segment window starting at `currentIndex`).  
- **Cleanup Function:** `syncLocalCacheDir` is reused as‑is; it deletes any file whose base name is not in `activeMap`.

### Handling Leftover Files from Previous Runs

The active‑set cleanup runs **every tick**. On engine start, the first tick will compute a valid window and remove any files outside that window – including those from a prior execution. No extra startup logic is needed.

If a time‑based failsafe is desired (e.g., remove files older than 1 hour), it can be added as a separate one‑time cleanup at engine startup, but it is **not required** for correctness.

---

## Code Changes Summary

**Files to modify:**
- `engine.go` – the tick loop, constants, struct fields, env reading  
- `envs.go` – add `GetEnvInt` (if missing)  
- `specification.md` – update §5 (Segment Fetcher & Cleanup) and §8 (environment variables)  
- `simlive-summary.instructions.md` – document new env vars and disk‑use invariant

**Key code changes in `engine.go`:**

```diff
// At top, after imports
const (
    DefaultKeepBehind    = 5
    DefaultDownloadAhead = 5
)

// In Engine struct
type Engine struct {
    // ... existing fields
+   keepBehind    int
+   downloadAhead int
}

// In NewEngine
+ e.keepBehind = utils.GetEnvInt("SIMLIVE_KEEP_BEHIND", DefaultKeepBehind)
+ e.downloadAhead = utils.GetEnvInt("SIMLIVE_DOWNLOAD_AHEAD", DefaultDownloadAhead)

// In StartPlayoutLoop, after currentIndex is determined:
- windowSize := 5
+ playlistWindow := 5
+ startIdx := currentIndex - e.keepBehind
+ if startIdx < 0 { startIdx = 0 }
+ endIdx := currentIndex + playlistWindow + e.downloadAhead - 1
+ if endIdx >= len(allSegs) { endIdx = len(allSegs) - 1 }
+ downloadSegs := allSegs[startIdx : endIdx+1]

// Candidate segs for playlist (old windowSize logic)
+ candidateSegs := allSegs[currentIndex : min(currentIndex+playlistWindow, len(allSegs))]

// Active map for cleanup
- for _, seg := range allSegs { ... }
+ for _, seg := range downloadSegs { ... }

// Download call
- go e.downloadAndPrepareSegments(channelDir, channelID, allSegs)
+ go e.downloadAndPrepareSegments(channelDir, channelID, downloadSegs)
```

---

## Verification Plan

1. **Build** – `make build` compiles cleanly.  
2. **Run engine** – check logs:
   - `playlist: writing 5 segments`  
   - `dispatching background prepare for ≤15 segment(s)` (depending on env values)  
   - `cache cleanup: removed N stale file(s)` (will be >0 on first tick if old files exist)  
3. **Check disk** – `ls $TMP_DIR/<channelID>/ | wc -l` stabilises at ≤ 32 (15 `.ts` + 15 `.pts` + `playlist.m3u8` + temporary files, but typically ≤ 31).  
4. **Player test** – `http://localhost:8080/live/<id>/playlist.m3u8`:
   - Playlist still contains 5 segments  
   - Slides forward by +1 every ~4 seconds (or segment duration)  
   - No 404 errors – pre‑fetched files are ready when they enter the playlist  
5. **Restart engine** – after a previous run, the first tick evicts leftover files, logs cleanup, and reaches steady state within 1 second.  
6. **Edge case**: timeline shorter than 15 segments – the window is capped to available segments; cleanup keeps only what exists.

---

## Documentation Updates

### `specification.md`

**§5 – Component Design**  
- Update #4 *Segment Fetcher*: describe that it downloads only the active window (`SIMLIVE_KEEP_BEHIND` + `SIMLIVE_DOWNLOAD_AHEAD` + playlist size).  
- Update #7 *Cleanup*: clarify that `syncLocalCacheDir` removes files outside the active window every tick; no global scan needed.

**§8 – Environment Variables**  
Add:
- `SIMLIVE_KEEP_BEHIND` – number of segments to retain before the playlist start (default 5).  
- `SIMLIVE_DOWNLOAD_AHEAD` – number of segments to pre‑fetch after the playlist end (default 5).

### `simlive-summary.instructions.md`

Add a note under *Operational Details*:  
> The engine maintains at most `SIMLIVE_KEEP_BEHIND + 5 + SIMLIVE_DOWNLOAD_AHEAD` `.ts` files per channel on disk (default 15). This bounded storage ensures low disk usage and fast startup.

---

## Additional Considerations

- **Worker pool size** – With a maximum of 15 downloads per tick, the 50‑worker pool is over‑provisioned. Consider reducing to 5‑10 in a later follow‑up, but out of scope for this plan.  
- **Failed‑rewrite segments** – Files that fail `rewritePtsOffset` are still marked prepared (via `.pts` marker) and will be kept if they fall inside the active window. This is existing behaviour and not changed.  
- **Time‑based failsafe** – If desired, add a startup routine that deletes files older than `X` hours (e.g., 1h) to guard against orphaned files from crashes. This can be a separate issue.

---

## Final Notes

This plan strictly adheres to your requirements:
- Keep 5 old (behind), 5 current (playlist), 5 ahead → **15 files on disk**.  
- Cleanup is based on the active window, which automatically removes older files from previous runs.  
- The player sees the same 5‑segment sliding window; no visible change.  
- Implementation touches only `engine.go` and env helpers, with doc updates.

**Ready for implementation.**