# Phase 0 Research: Planner & Engine Channel Lifecycle Reactivity

**Feature**: `013-planner-channel-lifecycle` | **Date**: 2026-11-03

## R1. Current behavior and root causes

**Decision**: Treat the following five defects as the root-cause set; each maps to a requirement in the spec.

1. **Discovery is blocked by work, not by the poll interval.** The planner's discovery loop (`cmd/planner/main.go:162-188`) calls `runTick`, which ends with `tickWG.Wait()` (`runPlannerTick`, lines 194-244). While any channel is preparing/downloading, the loop cannot observe roster changes. With EPG HTTP (30s client timeout), rclone calls (10s each), retries (`MAX_PREPARE_RETRIES=3`, exponential backoff), today plus tomorrow planning, and a full re-plan of *all* channels on any set change, a tick can block discovery for minutes. This is the direct cause of the reported ~5 minute stop/start latency.
2. **Stop and delete do not cancel in-flight work.** `StopChannel` only writes `status='INACTIVE'` (`internal/api/handler.go:571-617`); the planner's fast watcher only looks at `is_active=false` (`cmd/planner/main.go:111-133`). `DeleteChannel` hard-deletes the row (`handler.go:371-388`), which likewise never reaches the watcher. In-flight preparation continues until it finishes.
3. **In-flight work can resurrect a stopped channel.** `HandlePrepareResultForChannel` calls `markChannelLive`, which issues an unconditional `UPDATE ... SET status='LIVE'` (`internal/planner/planner.go:464-503`). A stop that lands during a successful prepare is overwritten.
4. **Two conflicting definitions of active.** Planner and engines select `status NOT IN ('INACTIVE','FAILURE')`; the planner watcher, list APIs, and SSE use `is_active`. The failure path sets only `is_active=false` (`planner.go:447-462`), so engines keep playing a channel the platform considers disabled. Conversely, status-only checks ignore operator intent recorded in the flag.
5. **The engines share the defect.** The engine reconciler polls every 5s hardcoded (`cmd/engines/main.go:99`), selects by `status` only, never reads `is_active`, and does not purge Redis HLS keys or rendition metadata when a channel stops. `ClearChannelCache` empties only an in-memory map (`internal/engine/engine.go:85-89`).

The client documentation already describes the intended behavior (stop/delete flip both columns and purge Redis; `client/API-doc.md:24-26,52-59`), so implementation is behind the documented contract.

## R2. Synchronization mechanism: polling reconciler vs broker notification

**Decision**: Keep Postgres as the control plane and poll a small reconciler, at a configurable `CHANNEL_SYNC_INTERVAL` (default `2s`), in both planner and engines.

**Rationale**:
- The target is 10s p95. A 2s poll plus immediate cancellation/start easily meets it; the current 5-minute delay is caused by blocking work, not by poll frequency.
- No new dependency or operational component. The existing constitution favors YAGNI and no new infrastructure.
- Polling is self-healing across restarts and Redis outages; a missed notification cannot wedge channel state.
- The query is a single indexed lookup over a small table (single-digit to tens of rows).

**Alternatives considered**:
- *Redis pub/sub*: would add a second durability domain, subscription reconnection logic, and lost-message handling; still needs polling as a fallback, so it becomes extra complexity for no latency benefit at this scale.
- *Postgres `LISTEN/NOTIFY`*: lowest latency, but requires a dedicated long-lived connection, payload limits, and reconnection/resync logic; the gain over 2s polling is not needed to meet the outcome.
- *Keep 1s polling but only remove the blocking tick*: viable, but a single configurable cadence shared with engines is simpler to reason about and lets operators tune it.

## R3. Authoritative active predicate

**Decision**: A channel is active only when **`is_active = true` AND `status NOT IN ('INACTIVE','FAILURE')`**. Add `PlayoutChannel.IsActive bool` with `gorm:"column:is_active;not null;default:true"` and use one shared predicate.

**Rationale**:
- Preserves both operator signals: `status` is the visible lifecycle state, `is_active` is the enable switch. Requiring both is fail-safe: either a stop (`is_active=false`) or an inactive/failed status stops work.
- Fixes the failure path without changing its semantics: setting `is_active=false` is sufficient to stop planner and engine work even while `status` remains `LIVE`/`PREPARE`.
- Makes the existing API documentation true and gives `AutoMigrate` a field to own.
- The field has been missing from the Go model, so API create/upsert references a column by name and relies on a database default. Adding the field removes that implicit dependency.

**Alternatives considered**:
- *Status only*: rejects legitimate `is_active=false` (failure disable, operator flag), and the docs/client rely on the flag.
- *Flag only*: loses `FAILURE` and explicit `INACTIVE` intent and would require changing every status-driven API surface.
- *Add a new `lifecycle_state` column*: cleaner theoretically but a larger migration and duplicate state; rejected as over-engineering.

## R4. Planner worker model

**Decision**: Replace the global tick + parallel tick workers with a per-channel supervisor map driven by the reconciler.

- On each reconcile: read active channels, build the roster, and compare against tracked supervisors.
- New active channel → start one supervisor goroutine immediately.
- Supervisor behavior: run the existing `processRunner` path (today prepare → optional tomorrow prepare → `ProcessWindow` → cleanup) once at start, then repeat every `PLANNER_INTERVAL` while active.
- Removed/deactivated channel → cancel its context, wait for the supervisor to finish, purge channel data, then drop the entry.
- Definition change (name, base name, content timezone, EPG ID, ad-EPG flag) → cancel, purge, and restart with the new definition.
- A supervisor that is stopping is marked `stopping` so no replacement is started until it exits; reactivation during shutdown is queued as a restart after the exit.

**Rationale**:
- Removes head-of-line blocking: a long prepare for channel A can no longer delay discovery/start of channel B.
- Matches the engine's existing per-channel loop and makes the two services conceptually identical.
- Serialized stop→start per channel satisfies the "at most one active work unit" requirement and prevents cleanup/write races.
- The global worker semaphore (`PLANNER_WORKERS`) is retained to cap concurrent expensive preparations; supervisors waiting for a slot do not block discovery.

**Alternatives considered**:
- *Keep the global tick and add a separate watcher only*: cannot fix activation latency caused by a long tick, and the set-change branch still re-plans every channel.
- *One goroutine per channel started once at boot*: cannot discover post-boot additions or react to removals without a signaling layer.

## R5. Preventing resurrection and stale writes

**Decision**: Make planner lifecycle writes conditional on the channel still being active.

- `markChannelLive` becomes `UPDATE playout_channels SET status='LIVE' WHERE id=? AND is_active=true AND status NOT IN ('INACTIVE','FAILURE')`; zero rows affected is a normal no-op (the channel was stopped).
- `MarkChannelFailed` and failure-counter updates are skipped when the supervisor context is cancelled.
- `markChannelInactive` (failure threshold) continues to set `is_active=false`; it is already durable under the predicate.
- All prepare/planning checkpoints keep checking `ctx.Err()` (existing behavior) and abort before writing.

**Rationale**: A single conditional SQL statement closes the race between an operator stop and an in-flight prepare without cross-service locking. Returns whether the transition happened for logging.

**Alternatives considered**:
- *Application-level mutex shared with the API*: impossible across processes.
- *Read-then-write in Go*: still racy; the conditional update is atomic in the database.

## R6. Cancellation propagation

**Decision**: Thread the channel context into external I/O used by preparation:

- `ESPXClient.GetEPGScheduleItems` and `GetChannelDetailWithEPG` accept a `context.Context` and use `http.NewRequestWithContext`.
- `RcloneService.Cat` accepts a parent context and derives its 10s timeout from it; `CopyTo` already does.
- Keep existing per-call timeouts so cancellation is bounded even if a remote hangs.

**Rationale**: Cancellation is cooperative today and can be delayed up to the 30s HTTP timeout. Passing context makes a stop take effect promptly and satisfies the 10s p95 target without changing response semantics. API handlers pass the request context; the planner passes the supervisor context.

**Alternatives considered**:
- *Rely on existing timeouts only*: worst case reaches the 30s ceiling and can overlap with a restart, breaking the "one work unit" and 10s goals.
- *Kill the process*: not possible from another binary and would disrupt other channels.

## R7. Purge ownership and ordering

**Decision**: Purge at the point that observes the stop, idempotently, in this order:

1. **API stop/delete**: best-effort purge immediately for operator feedback (remove `simlive:hls:{id}`, `simlive:hls:{id}:rendition:*`, `simlive:renditions:{id}`, `simlive:timeline:{id}`, `planner:cursor:{id}`, rendition-metadata key; preserve `planner:failures:{id}` per the documented contract). Also clear the segment-track log.
2. **Planner supervisor exit**: cancel, wait, then call `CleanChannelData` (timeline, cursor, failure counter) only after the worker has fully stopped.
3. **Engine reconciler**: on deactivation/removal, cancel the playout loop, stop downloads via context, purge the same Redis HLS/timeline keys plus rendition metadata, clear in-memory `rendMetaCache`, and remove the channel disk directory.

Because the API purge is best-effort and the worker/engine purges are authoritative, every path converges. Cleanup always runs after the work unit exits so a new session can never be wiped by an old worker.

**Rationale**: Satisfies FR-002/FR-009 (no ghost output, no stale session data) and keeps the failure breaker so a restart behaves like a fresh session only when an operator explicitly starts it.

**Alternatives considered**:
- *API-only purge*: misses direct DB changes and engine-side failures.
- *Planner-only purge*: leaves stale HLS playlists served by the API for up to the 24h TTL.
- *Purge on start instead of stop*: leaves a window where a stopped channel keeps serving output.

## R8. Engines parity

**Decision**:
- Use the shared active query/predicate and `CHANNEL_SYNC_INTERVAL` instead of `time.Sleep(5s)`.
- Keep the 500ms playout tick and `CancelCauseFunc` per channel.
- On definition change, cancel the old loop and **do not spawn the replacement in the same reconcile pass**; spawn on the next pass so the old loop (exits within one 500ms tick) is gone. This removes the current overlap where a restart starts while the old loop is still using the same directories and Redis keys.
- On deactivation/removal, call a new `PurgeChannel` that clears `rendMetaCache`, deletes HLS master/rendition and rendition-metadata Redis keys, and removes `TMP_DIR/{channel}`.
- Keep `isChannelChanged` semantics (definition fields only, not `status`), moved to the shared helper.

**Rationale**: The reported question — "does the engines script have the same issue?" — is answered yes in two ways: it ignores `is_active` (leaving failure-disabled channels playing) and it leaves stale HLS output cached after a stop. Both are fixed by the same predicate and purge contract.

**Alternatives considered**:
- *Immediate spawn after cancel*: causes duplicate work on shared state.
- *Restart the engine process on roster changes*: disrupts all channels; rejected.

## R9. Delete semantics

**Decision**: Keep `DELETE` as a hard delete, and align `client/API-doc.md` and `client/swagger.yaml` with that behavior. Do not introduce soft-delete columns.

**Rationale**:
- The spec's requirement is removal from the roster and immediate stop; a hard delete is unambiguous and already implemented.
- Soft delete would require filtering every list/registered query and changing the meaning of `DELETE` for clients, which is broader than this feature.
- Removal is detected by the reconciler because the channel disappears from the active query; in-flight work is cancelled and conditional writes cannot recreate the row.

**Alternatives considered**:
- *Implement the documented soft delete*: plausible but changes API list semantics (rows still returned) and client expectations; scope creep not needed to meet the lifecycle outcomes.

## R10. API transition normalization

**Decision**: One mapping drives all mutation endpoints:

| Requested state | `status` | `is_active` |
|---|---|---|
| start / reactivate | `PREPARE` | `true` |
| status `PREPARE` or `LIVE` | as requested | `true` |
| status `INACTIVE` | `INACTIVE` | `false` |
| explicit `is_active=false` | `INACTIVE` | `false` |
| explicit `is_active=true` | `PREPARE` | `true` |

- Add `IsActive *bool` to `UpdateChannelReq` (currently missing although documented).
- When both are supplied, `status` wins for the column pair; an explicit contradiction is rejected with `400`.
- Create/upsert sets `IsActive: true`, `Status: PREPARE`.
- Implement the best-effort Redis purge currently commented out in `StopChannel` and missing from `DeleteChannel`.

**Rationale**: Resolves the "need verify" question from the request: reactivation works through either re-adding the channel or changing status from inactive to LIVE/PREPARE, and both converge on the same state. The mapping guarantees no contradictory row exists.

## R11. Job cadence and configuration

**Decision**:
- `CHANNEL_SYNC_INTERVAL` (default `2s`): planner and engine roster reconciliation. This bounds detection latency.
- `PLANNER_INTERVAL` (default `5m`, `.env` currently `1m`): per-channel re-plan cadence, unchanged meaning.
- `PLANNER_WORKERS` (default `4`) and `MAX_WORKERS` (default `4`) remain concurrency caps.
- The 10s/30s success targets are documented as outcomes, not as extra timeouts.

**Rationale**: Follows the project preference for environment-driven platform settings with sensible defaults; keeps detection cadence independent from re-plan cadence.

**Alternatives considered**:
- *One shared interval for detection and re-planning*: forces a bad trade-off between fast detection and expensive re-planning.

## R12. Testing strategy

**Decision**:
- Unit tests (pure, no DB) for `models` active predicate, status normalization, and definition equality.
- Unit tests for the roster diff/desired-state function in `internal/lifecycle` (new/removed/unchanged/definition-changed/stopping-reactivation).
- Unit tests for the API transition mapping (start/stop/status/inactive update).
- Unit tests for the planner supervisor state machine where feasible without I/O (start, cancel, restart-after-stop, no duplicate).
- Integration validation via `quickstart.md` against the real stack, including measuring stop/start latency from logs, verifying Redis keys are gone, and confirming unrelated channels are unaffected.
- `make fmt`, `make vet`, `make test` are the merge gates.

**Rationale**: Constitution Principle III requires stack-level validation for playout changes; pure logic stays fast and deterministic. No new test dependency is added.

**Alternatives considered**:
- *Adding a DB-backed integration test harness*: valuable but larger than this feature; the manual/scripted quickstart covers the end-to-end path with existing tooling.
