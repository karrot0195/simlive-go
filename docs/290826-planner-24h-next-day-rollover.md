# Planner 24/7 Eager Next-Day Rollover — 2026-08-29

> **Status:** ✅ Implemented
> **Scope:** `cmd/planner/main.go` (cron loop), `internal/planner/planner.go`
> (per-date horizon + dedup helper + cleanup key fix),
> `internal/planner/segment_processor.go` (new `isEndOfTimeline` signature),
> `internal/utils/times.go` (`StartOfDay` + `GetLocFor`),
> `internal/planner/horizon_rollover_test.go` (regression test).
> **Backs:** `docs/280826-continuous-planner-rollover-design.md`
> **Cures:** `docs/280826-chromium-pipeline-stopped-negative-pts.md` is unrelated
> (PTS rewrite) — left in place.

---

## 1. Problem Statement

The user's expectation: **"planner always prepare data for all day and next day
if segment near finish"**, and **"if data is here, we don't process again"**.

Before this change the planner only satisfied part of the requirement:

- `cmd/planner/main.go` ran the EPG walk **once at startup** (the `for {}` loop
  was commented out, and only today's date string was ever passed in).
- `ProcessEPGToTimeline` stopped at `EndOfDay(processStartTime)`, which is
  **today's 23:59:59** in the server's local zone. There was no way to walk
  past midnight into tomorrow in the same call.
- `isEndOfTimeline` compared `globalRunningTime` against
  `EndOfDay(processStartTime)`, so even if the cron ticked again, the
  per-call `processStartTime` was locked to today and the next call would
  start a fresh EPG walk from today's beginning — every tick re-walked today
  and never reached tomorrow.
- `CleanupOldSegments` used the wrong Redis key (`timeline:%s` instead of
  `simlive:timeline:%s`), so the bounded retention model was a silent no-op
  the moment the loop actually started ticking.
- The "data is here, don't reprocess" half was unenforceable because
  `ProcessEPGToTimeline` ZADDed unconditionally (segments get a fresh
  `sequence` from `getLastSequenceNumber`, so a re-run would silently produce
  duplicate sequences with the same `MediaStart`).

## 2. Solution Overview

Three coordinated changes implement **Approach A: eager next-day** with
**ZCOUNT-based dedup**:

1. **Per-date horizon** on `Planner` (`horizon`, `processDate`). Each call to
   `ProcessEPGToTimeline` parses the requested `dateStr` and sets
   `horizon = EndOfDay(parsedDate)`. `isEndOfTimeline` is rewritten to take
   just `runningTime` and consult `horizon` — independent of when the planner
   was started. This unblocks a single cron tick from filling both today AND
   tomorrow without an artificial midnight stop.
2. **Today + tomorrow cron loop** in `cmd/planner/main.go`. Each tick:
   iterates active channels; honors the channel's `content_timezone` to
   compute today/tomorrow; calls a new `prepareDate()` helper that
   short-circuits via `HasDateInTimeline`; then bumps the per-channel
   cursor (`planner:cursor:{channelID}`) and trims the ZSET with
   `CleanupOldSegments` (now hitting the real key).
3. **`HasDateInTimeline` dedup** via `ZCOUNT simlive:timeline:{ch}
   <dayStart> <dayEnd>`. If the ZSET already has ≥ 1 segment whose
   `MediaStart` falls inside the requested local date, the date is skipped.
   No EPG fetch, no `rclone cat`, no Redis write traffic — the per-tick cost
   is one round-trip per (channel, date) once the data is prepared.

## 3. What Changed (by file)

### `cmd/planner/main.go` (+60 lines)

| Area | Change |
|------|--------|
| Cron loop | Re-enabled `for {}` with `time.Sleep(plannerInterval())` (default 5m, `PLANNER_INTERVAL` override) |
| Date iteration | For each active channel: today + tomorrow (in `ch.ContentTimezone`, via `utils.GetLocFor`) |
| `prepareDate` | New helper: `HasDateInTimeline` → skip if already prepared, else `ProcessEPGToTimeline` |
| Per-tick bookkeeping | `ProcessWindow` bumps `planner:cursor:{ch}` to `nowUnix + LOOKAHEAD_SECONDS`; `CleanupOldSegments` trims the ZSET with `RETENTION_SECONDS` |
| Timezone | "Today" / "tomorrow" computed in the channel's content timezone, not the server's local zone — avoids cross-tz EPG date mismatch |

### `internal/planner/planner.go`

| Area | Change |
|------|--------|
| New fields | `horizon time.Time` (per-call, `EndOfDay(parsedDate)`) and `processDate time.Time` |
| `ProcessEPGToTimeline` | Parses `dateStr` in `ch.ContentTimezone` and sets `p.horizon` / `p.processDate`; the per-schedule `isEndOfTimeline` checks use the new horizon |
| `isEndOfTimeline` | Signature reduced from `(processTime, runningTime)` to `(runningTime)` — now consults `p.horizon`. Returns false when `horizon` is zero (defensive) |
| `HasDateInTimeline` | New public helper: `ZCOUNT simlive:timeline:{ch} <start> <end> > 0` |
| `CleanupOldSegments` | Bug fix: key was `timeline:%s` (silent no-op), now `simlive:timeline:%s` |

### `internal/planner/segment_processor.go`

| Area | Change |
|------|--------|
| `isEndOfTimeline` call sites | 2 call sites updated to the new single-arg form (video segment path, ad insertion path) |

### `internal/utils/times.go`

| Area | Change |
|------|--------|
| `StartOfDay` | New: returns local midnight of `t` |
| `GetLocFor` | New: IANA → `*time.Location` with UTC fallback |

### `internal/planner/horizon_rollover_test.go` (new, 170 lines)

| Test | Asserts |
|------|--------|
| `TestIsEndOfTimeline_HorizonIndependentOfProcessStartTime` | At 23:58 today with horizon=tomorrow's 23:59:59, `isEndOfTimeline` returns false; at midday tomorrow same; 1s past horizon same returns true. **Pure Go, no Redis required.** |
| `TestHasDateInTimeline_DedupSkipsPreparedDates` | After seeding one segment per date in `simlive:timeline:test-horizon-rollover-dedup`, `HasDateInTimeline` reports `has=true` for both dates; ZCARD remains 2 (no duplicate ZADDs); `ZREMRANGEBYSCORE` via the now-correct key clears the ZSET. **Skipped if no Redis at `REDIS_ADDR`.** |

## 4. Operational Notes

- **`PLANNER_INTERVAL` is now actually used** (was documented but never
  consumed). Default `5m`; min effective value `1s`. Override via env.
- **Per-tick cost** is bounded: 1× `ZCOUNT` per (channel, date) = 4 round
  trips per channel (today skip, tomorrow skip, cursor GET, cleanup
  ZREMRANGEBYSCORE). At 100 active channels this is ~400 round-trips every
  5 min — negligible.
- **Cold start**: first tick fills today + tomorrow (~1–2 min of EPG fetch +
  rclone cat per channel). Subsequent ticks skip both dates.
- **Long-term**: when `RETENTION_SECONDS` (7200) trims today out of the ZSET,
  the next tick will re-prepare today (ZCOUNT returns 0). This is acceptable
  because the engine has already played past those segments and the new
  prepare is cheap. If the re-prepare is undesirable, swap the dedup source
  to a separate `planner:done:{ch}:{date}` marker (48h TTL) — left as a
  future improvement.
- **No regressions in engine**: the engine still reads
  `simlive:timeline:{channelID}` and writes the same `playlist.m3u8`. The
  `engine` package and its tests are unchanged.

## 5. Validation

- `go build ./cmd/... ./internal/...` — clean
- `go test ./internal/engine/...` — `TestColdStartPtsClamp`,
  `TestMinFirstDTSProbe` PASS
- `go test ./internal/planner/... -run TestIsEndOfTimeline_...` — PASS
- `go test ./internal/planner/... -run TestHasDateInTimeline_...` — SKIP
  (no local Redis on this host; re-run with `REDIS_ADDR=localhost:6371` to
  execute)

## 6. Future Work

- Replace `ZCOUNT` dedup with a `planner:done:{ch}:{date}` marker when
  `RETENTION_SECONDS`-induced re-prepares become a concern.
- Per-channel `PLANNER_INTERVAL` override in the `playout_channels` table
  for high-traffic channels that need faster EPG pickup.
- Expose `planner:cursor:{ch}` and ZSET size via the API for ops dashboards.
