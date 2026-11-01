# Tasks: Persistent Segment Track Logs

**Input**: Design documents from `/specs/009-segment-track-logs/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, quickstart.md

**Tests**: Manual verification only (per plan.md). No automated test tasks.

**Organization**: Tasks grouped by user story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2)

## Path Conventions

- Backend: `internal/api/`, `cmd/api/main.go`
- Frontend: `client/src/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the new backend module skeleton.

- [x] T001 Create `internal/api/segment_track.go` with package `api` declaration and required imports (encoding/json, os, path/filepath, bufio, strings, time, gin, simlive/internal/models, simlive/internal/utils).

**Checkpoint**: File compiles as part of the api package.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core storage helpers that both user stories depend on.

- [x] T002 Add `SegmentTrackEntry` struct to `internal/api/segment_track.go` with fields: `ChannelID string`, `Sequence int64`, `Status string`, `Timestamp int64` (unix), `Message string` (omitempty), with JSON tags `channel_id`, `sequence`, `status`, `timestamp`, `message,omitempty`.
- [x] T003 Implement `segmentTrackPath(channelID string) string` in `internal/api/segment_track.go` returning `filepath.Join(tmpDir, "logs", channelID+".jsonl")` where tmpDir comes from `utils.GetSettings().TMPDir` (or `os.Getenv("TMP_DIR")` with fallback to `os.TempDir()`).
- [x] T004 Implement `AppendSegmentTrackEntry(channelID string, entry SegmentTrackEntry) error` in `internal/api/segment_track.go`: ensure the `logs` directory exists (`os.MkdirAll`), marshal entry to JSON line, append to file, then trim file to at most 1000 lines (keep newest) by rewriting when exceeded.
- [x] T005 Implement `ReadSegmentTrack(channelID string, limit int) ([]SegmentTrackEntry, error)` in `internal/api/segment_track.go`: read file if exists, parse JSONL lines, return the last `limit` entries (default 100 when limit <= 0 or > 1000), oldest-first ordering. Return empty slice (not error) when file missing.

**Checkpoint**: Storage helpers compile and are reusable.

---

## Phase 3: User Story 1 - Persist segment track (Priority: P1)

**Goal**: Segment preparation/download events are persisted per channel in append-only JSONL files (max 1000 entries).

**Independent Test**: Trigger segment processing; verify log file grows under `TMPDir/logs/<channel>.jsonl` and oldest entries are trimmed past 1000.

### Implementation for User Story 1

- [x] T006 [US1] Add `GetSegmentTrack` method on `*ChannelHandler` in `internal/api/segment_track.go`: read `:id` path param with `channelIDGuard`, parse `?limit=` query (default 100, clamp to [1,1000]), call `ReadSegmentTrack`, return `gin.H{"channel_id": id, "entries": entries}` as JSON. Return 400 on invalid id, 500 on read error.
- [x] T007 [US1] Register route `GET /api/channels/:id/segment-track` in `cmd/api/main.go` inside the `/api` group (after the prepared route), wiring to `channelHandler.GetSegmentTrack`.
- [x] T008 [US1] Add a best-effort recording hook in `internal/api/monitor.go` `buildTimelinePayload`: for each monitored channel, scan its on-disk prepared segment files (reuse `GetPreparedSegments` disk scan) and append a `SegmentTrackEntry{ChannelID, Sequence, Status: "prepared", Timestamp: now.Unix()}` for each sequence not in a process-local last-seen set (package-level map guarded by mutex). Scan/append errors logged via `utils.Logger` only; never affect the SSE payload or playout (FR-005). Last-seen set is process-local and resets on API restart.

**Checkpoint**: API serves `GET /api/channels/:id/segment-track?limit=N`; log files grow as segments are prepared.

---

## Phase 4: User Story 2 - View segment track from UI (Priority: P1)

**Goal**: Operator can view a channel's segment track via a button on the timeline screen.

**Independent Test**: Click the log button on a channel card; a dialog lists recent entries or shows an empty state.

### Implementation for User Story 2

- [x] T009 [P] [US2] Add `SegmentTrackEntry` interface to `client/src/types/api.ts` with fields `channel_id: string`, `sequence: number`, `status: string`, `timestamp: number`, `message?: string`.
- [x] T010 [US2] Add `getSegmentTrack(id: string, limit?: number)` method to `client/src/libs/api.ts` calling `GET /channels/${id}/segment-track` with optional `limit` param and unwrapping to `{ channel_id, entries }`.
- [x] T011 [US2] Add a "View track" button per channel card in `client/src/views/RunningChannelsTimeline.vue` (in `v-card-title`) that opens a dialog and fetches entries for that channel.
- [x] T012 [US2] Add a dialog in `client/src/views/RunningChannelsTimeline.vue` listing entries (sequence, status, timestamp formatted, message) with an empty state when no entries. Use stable min dimensions to avoid layout shift (per taste).

**Checkpoint**: Operators can view track entries from the UI; empty state shown when none.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [x] T013 [P] Run `gofmt`/`go vet ./...` and fix any issues in `internal/api/segment_track.go`.
- [x] T014 [P] Run client typecheck (`pnpm --filter client typecheck` or `tsc --noEmit`) and fix type errors.
- [x] T015 Validate against `specs/009-segment-track-logs/quickstart.md`: trigger segment processing, verify log file grows, exceed 1000 entries (trim), open timeline screen and click log button, verify entries displayed.

## Scope Sync (captured during implementation)

- [x] T016 Deduplicate repeated segment cards in timeline by sequence in buildTimelinePayload and GetRunningChannelsTimeline. (captured request: "timeline data on UI -> sometime appear 1 segment many times")
- [x] T017 Extend SegmentTrackEntry with datetime, VOD/AD type (is_ad), and renditions; update recorder to populate them. (captured request: "expect it will keep infomration like segment on timeline - datetime, which renditions, VOD or ADS")
- [x] T018 Add Clear button + DELETE endpoint to reset track file to empty; keep seen set to avoid immediate refill. (captured request: "we also FE reset track file to empty")
- [x] T020 Clear keeps the seen set intact (no immediate refill) so cleared data does not reappear; channel start/stop resets it for a fresh run. (improvement: "when call delete track API -> no more log for seg add to it" / "clear button not work, data reappear")
- [x] T019 Prevent engine orphan cleanup and cold-start PrepareEnv from deleting the shared logs/ directory. (captured request: track log kept disappearing)
- [x] T021 Reconcile track log with on-disk segments: evict entries whose segment files no longer exist (stale entries). (captured request: "view track shows stale segments not on disk")
- [x] T022 Unify the two recorders: make both use the same source/semantics (disk scan vs Redis timeline) and avoid logging future segments as "prepared". (captured request: "track log and timeline differ")
- [x] T023 Reset track log + seen set on channel start/stop so a new run does not inherit prior-run sequence baseline. (captured request: "track keeps accumulating across runs")
- [ ] T024 Populate real status values (prepared/failed/downloaded) instead of hardcoded "prepared". (captured request: "track only ever shows prepared") — deferred: requires engine/planner to report per-segment status.
- [x] T025 Ensure FE View Track dialog tolerates entries beyond the timeline window and documents audit vs timeline. (captured request: "view track vs timeline mismatch")
- [x] T026 Make View Track rendition counts consistent with timeline: re-log segment when rendition set changes and dedup by sequence keeping latest entry. (captured request: "timeline shows #324 with 2 renditions, track view shows 1")
- [x] T027 Add Reload button to View Track dialog to refresh data. (captured request: "View Track -> add a reload button for refresh new data")
- [x] T028 Change View Track Time column to show segment running time instead of recording time. (captured request: "Time column ... i expect it is running time of segment")
- [x] T029 Remove from/to time filter from View Track dialog. (captured request: "no need filter on FE -> remove it")

---
## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Phase 1.
- **US1 (Phase 3)**: Depends on Phase 2.
- **US2 (Phase 4)**: Depends on Phase 3 (needs API endpoint).
- **Polish (Phase 5)**: Depends on all above.

### Parallel Opportunities

- T009 (TS type) can run in parallel with backend tasks.
- T013 and T014 can run in parallel.

### Implementation Strategy

MVP: Complete Phases 1–3 (backend storage + API). Then Phase 4 (FE). Then polish.