# Research: 24/7 Playout EPG Cache Fallback

## Decision 1 — Treat empty provider responses as "no data"

**Decision**: Change the existing guard in `ProcessEPGToTimeline` so that a successful HTTP response with zero schedule items returns a distinct sentinel error (`ErrNoScheduleData`) instead of the current `nil`.

**Rationale**: Today `if err != nil || len(schedules.Data) == 0 { return err }` returns `nil` when the payload is empty. The caller then resets the failure counter and marks the channel `LIVE`, so the day is silently considered prepared while no timeline is produced — the exact failure mode reported. A sentinel lets the caller distinguish "provider down / empty" from "planned successfully" and from unrelated errors.

**Alternatives considered**:
- Returning a generic error: rejected because retry/status behavior needs to distinguish "unrecoverable, no cache" from schema or transport failures.
- Returning `nil` and letting the timeline run dry: rejected — it hides the outage and is the current bug.

## Decision 2 — Retained schedule lives in a dedicated on-disk directory

**Decision**: Store the most recent non-empty schedule per channel at `EPG_CACHE_DIR/<channel_id>.json`, with `EPG_CACHE_DIR` defaulting to `{TMP_DIR}/epg-cache`. Files carry a schema version, channel identity, provider EPG identity, source day, fetch time, and the raw schedule items. Writes go to a temp file in the same directory and are renamed into place.

**Rationale**: The request explicitly asks for an on-disk cache. `TMP_DIR` is already the durable server data root (`/root/simlive-go/outputs` in the systemd unit) and is shared by planner, engines, and API. A dedicated subdirectory keeps schedule data separate from generated segments and makes the cache easy to inspect or clear. Atomic rename prevents a torn read after a crash. Identity fields guard against applying a cache from a different channel or after an EPG reassignment.

**Alternatives considered**:
- Storing the cache in Redis: rejected — the request asks for disk, and Redis is already used as the hot timeline path with expiry semantics.
- Reusing the raw `TMP_DIR` root: rejected — mixes schedule metadata with segment data and complicates cleanup.
- Keeping multiple historical days: rejected — only the most recent provider-sourced day is needed (FR-009); extra history adds selection ambiguity with no benefit.

## Decision 3 — Preserve the cache across `ENABLE_CLEAN_UP=true`

**Decision**: When `prepareEnv` wipes `TMP_DIR`, it removes the directory's children except the resolved `EPG_CACHE_DIR`. Explicit cache invalidation requires deleting the cache directory.

**Rationale**: Production runs with `ENABLE_CLEAN_UP=true`, so every planner restart currently removes `TMP_DIR`. Without this change, the retained schedule would never survive a restart, violating FR-001/SC-002. A cold start is meant to reset generated artefacts, not the last known-good schedule.

**Alternatives considered**:
- Placing the cache outside `TMP_DIR` by default: rejected — surprises operators and breaks the existing "all persistent data under TMP_DIR" convention.
- Not preserving it and accepting a cold cache on restart: rejected — contradicts the spec's restart requirement.

## Decision 4 — Fallback generation: repeat the retained day and anchor it to the target day

**Decision**: Add `buildFallbackSchedule(items, targetDayStart, horizon, tz)` that walks the retained items in order, repeats the sequence as needed, assigns `start = cursor`, `end = start + originalDuration`, and stops at `horizon`. Programme streams are deep-copied. Zero/negative-duration items are skipped; no progress causes the builder to stop rather than loop.

`targetDayStart` is the absolute instant of host-local midnight for the target day; the produced timestamps are formatted in the channel's content timezone so the processor's `ParsePARTNERTime(..., channel.ContentTimezone)` reads them correctly.

**Rationale**: Timeline segment durations come from each item's m3u8 (`#EXT-X-PTS-RANGE`), not from EPG start/end. EPG times are used only to skip items that already ended and to seed `runningTime`. Repeating the retained day and anchoring it at the target day start therefore produces continuous segments from the seeded running time, with no gap and no rewind. The builder covers "any day" required by the clarification, including a current day after a restart.

**Alternatives considered**:
- Reusing `padScheduleToCoverDay`: rejected — it only fills head/tail gaps around existing items and assumes items already belong to the target day.
- Shifting only the first day then relying on the temporary pad helper: rejected — produces a single-day cover, not a guaranteed full target-day cover.
- Generating segments directly from cached m3u8 paths: rejected — bypasses the existing ad-insertion, SCTE-35, and sequence logic.

## Decision 5 — Recovery: purge not-yet-played fallback coverage, then re-plan

**Decision**: Record each fallback-filled day in Redis. On later ticks, if a day was fallback-filled and the provider now returns a usable schedule, remove timeline entries for that day from `max(now - WindowBufferSeconds, dayStart)` upward, update the retained schedule, clear the fallback marker for that day, and re-run the normal planning path so only not-yet-produced coverage is emitted.

**Rationale**: The planner eagerly plans to the end of the day, so a fallback day is fully populated ahead of time. Simply calling the planner again with real data would append duplicate segments at the same times and break continuity. Purging only unplayed future entries lets the existing `getLastSegmentState` seed sequence and running time from the surviving tail, satisfying FR-007/FR-008 and automatic recovery (FR-010/SC-005). `HasDateInTimeline` cannot be the only gate any more, because it would permanently skip a fallback-filled day.

**Alternatives considered**:
- Leave fallback coverage for the whole day and recover only on the next day: rejected — violates FR-010/SC-005 and leaves a full day of stale programming after a short outage.
- Rebuild the entire day from scratch: rejected — would delete already-played segments and break the live playlist.
- Keep a separate "planned ahead" index: rejected — adds state for no benefit; the timeline score already encodes coverage time.

## Decision 6 — Operator visibility via Redis + existing timeline payload

**Decision**: Write fallback markers to Redis hash `planner:fallback:{channel_id}` with one field per target day (`YYYY-MM-DD`) whose value carries source day, source EPG id, day start/end, and creation time. The API reads the hash while building the timeline payload and exposes `fallback_active` and `fallback_source_date` on each channel; the admin timeline renders a chip while `now` is within the fallback day.

**Rationale**: Planner and API are separate processes and may run on different hosts, so disk alone is not a reliable cross-service signal; Redis is already the shared hot path. Using a per-day hash avoids one day's marker overwriting another during a multi-day outage. Activating only within the target day's host-local `[day_start, day_end]` prevents a next-day fallback planned at 21:00 from labeling the still-real current day.

**Alternatives considered**:
- New DB column on `playout_channels`: rejected — the state is ephemeral and per-day, not channel configuration.
- Reading the cache file from the API: rejected — requires shared filesystem assumptions between services.
- Logs only: rejected by the clarification; admins need an in-UI label.

## Decision 7 — Failure semantics

**Decision**: A successful fallback is a successful preparation: reset the failure counter, mark the channel `LIVE`, and do not inactivate. If the provider returns no data and no retained schedule exists (or it is unreadable/identity-mismatched), return `ErrNoScheduleData` so the existing retry/counter path reports the channel as failing and eventually inactivates it.

**Rationale**: Matches FR-011/FR-012 and prevents a fallback-covered channel from being killed by the failure breaker while still surfacing genuinely unrecoverable channels.

**Alternatives considered**:
- Treat missing cache as success: rejected — silently dark channel.
- Never inactivate on empty data: rejected — loses the existing operator signal for a channel that truly cannot be planned.

## Decision 8 — Testing strategy

**Decision**: Unit-test pure logic (cache round-trip/identity/corruption, fallback builder coverage and repetition, marker parsing) with `go test ./internal/planner/...`. Validate end-to-end continuity and recovery in `quickstart.md` against a running planner + engine + Redis + Postgres stack with a stub provider that can return empty, real, then empty data.

**Rationale**: Constitution Principle III requires integration verification for changes to the timeline model; the builder and cache are pure and cheap to unit test. A stub provider makes the empty/recover transitions deterministic.

**Alternatives considered**:
- Only integration tests: rejected — slow and cannot cover corruption/identity edge cases.
- Only unit tests: rejected — does not prove playback continuity across the day boundary.

## Decision 9 — Canonical day boundary is host-local

**Decision**: The target day is the host-local day: `processDate = utils.StartOfDay(now.In(utils.LocalLocation()))`, `horizon = utils.EndOfDay(processDate)`, and the fallback marker stores those same `day_start`/`day_end` instants. The only content-timezone use is formatting item timestamps so `utils.ParsePARTNERTime(..., channel.ContentTimezone)` yields the intended absolute instants.

**Rationale**: FR-014 mandates the project's existing local-timezone convention with `configs.TZ` as fallback. The current planner already derives `processDate`/`horizon` this way and sends `dateStr` with the local offset. Using a content-timezone day would make the fallback window, the planner horizon, and the UI label disagree whenever the two zones differ — potentially leaving uncovered hours at the end of the day. Host-local keeps all three aligned.

**Alternatives considered**:
- Content-timezone day boundary: rejected — disagrees with the existing horizon and the provider request date, and would need a parallel boundary concept across planner, marker, and API.
- Per-channel selectable boundary: rejected — new configuration for no proven need (YAGNI).

## Decision 10 — Explicit no-duplicate guard for fallback

**Decision**: `ProcessEPGToTimeline` refuses to generate fallback coverage for a day that already has timeline entries; it reloads the last timeline member to seed sequence and running time and only proceeds when the day is empty. Recovery is the one path that removes existing entries before replanning.

**Rationale**: FR-007 requires that fallback never duplicates or rewinds coverage. Relying solely on the caller's `HasDateInTimeline` check makes correctness invisible at the point of generation and fragile to future callers. An in-function guard makes the invariant explicit and testable; seeding from the last member preserves continuity.

**Alternatives considered**:
- Keep the caller-only gate: rejected — too implicit, flagged as underspecified in analysis.
- Deduplicate by scanning the whole ZSET: rejected — more work per tick and unnecessary when the day-level guard suffices.

## Decision 11 — Marker lifecycle

**Decision**: Write the fallback marker only after the fallback plan produced coverage; remove it if planning fails. During recovery, keep the marker until the purge and real-data replan have both succeeded.

**Rationale**: The UI label is driven by the marker, so writing before success can show a false fallback label for a channel that produced nothing; clearing before a successful replan can leave the day with neither coverage nor a marker (flagged as I2/I5). Tying the marker to the committed outcome keeps the UI truthful.

**Alternatives considered**:
- Write before planning (simpler): rejected — false positives on failure.
- Derive the label by scanning timeline entries: rejected — the timeline does not record provenance.
