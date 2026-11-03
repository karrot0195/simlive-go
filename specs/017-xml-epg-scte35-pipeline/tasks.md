# Tasks: XML EPG Ad Pipeline

**Input**: Design documents from `specs/014-xml-epg-scte35-pipeline/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: REQUIRED — constitution Principle III (Test-First, NON-NEGOTIABLE). Tests are written first and must fail before implementation.

**Organization**: Tasks are grouped by user story so each story can be implemented and verified independently. User stories: US1 XML-only ads, US2 boundary placement, US3 PTS continuity, US4 static PMT, US5 XML schedule source.

**Verification skills**: Taken from `.specific/project.yaml` — `start planner` = `make start-planner`, `start all` = `make start-all`, `start engines` = `make start-engines`, `test` = `make test`, `build` = `make build`, `db query` = `docker exec simlive_postgres psql -U postgres -d simlive`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1–US5 for user-story phases only
- Every task includes an exact file path

## Path Conventions

- Go packages: `internal/<package>/`
- Commands: `cmd/<service>/`
- Tests live beside the package under test as `*_test.go`
- Feature documents: `specs/014-xml-epg-scte35-pipeline/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish a clean baseline and the test seams required by every planner test.

- [x] T001 Run baseline `make fmt`, `make vet`, `make test`, and `make build` from `makefile`; record any pre-existing failures before changing code
- [x] T002 [P] Add an asset-reader seam so planner tests can serve manifests without rclone: introduce a `Cat` interface and `GetRenditionsFromMasterText` helper in `internal/utils/m3u8.go`, then store the reader on `SegmentProcessor` in `internal/planner/segment_processor.go`
- [ ] T003 [P] Add shared test builders for content/ad manifests and timeline assertions in `internal/planner/segment_processor_test.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared parsing, break identity, asset loading, and marker helpers required by all stories.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [ ] T004 [P] Add failing parser tests for attribute-bearing `<ads path duration start/>`, text-only `<ads>true</ads>`, `programme@is_ads`, missing streams, and malformed timestamps in `internal/api/partner_client_test.go`
- [x] T005 Extend `GetEPGScheduleItemsFromXML` in `internal/api/partner_client.go` to parse inline markers and set `Programme.IsAds` for text-only or attribute-based programme ad designations; keep time normalization to the server clock
- [ ] T006 [P] Add failing model tests asserting a content segment with `is_ad=false`, `ad_type="in"`, and break metadata emits a CUE-IN date-range in `internal/models/scte35_fields_test.go`
- [x] T007 Update `buildMediaPlaylistBody` in `internal/models/scte35.go` to emit the CUE-IN date-range when `AdType=="in"` and `AdBreakID!=0`, independent of `IsAd`
- [ ] T008 [P] Add failing tests for a per-plan break counter and a marker-derived break context in `internal/planner/segment_processor_test.go`
- [x] T009 Replace timestamp-derived break identity with a per-plan counter and an XML-marker-backed break context in `internal/planner/segment_processor.go`; use the full counter for `EventID` and `counter & 0xFFFF` for `UniqueProgramID`
- [ ] T010 [P] Add failing tests for shifting inline ad marker starts when padding shifts a programme in `internal/planner/schedule_pad_test.go`
- [x] T011 Shift each inline ad marker start by the same delta applied to its programme window in `internal/planner/schedule_pad.go`; drop markers whose timestamps cannot be parsed after the shift
- [ ] T012 Add failing tests for ad asset loading from a master playlist, primary rendition selection, `#EXT-X-PTS-RANGE` durations, `#EXTINF` fallback, and the no-rendition skip path in `internal/planner/segment_processor_test.go`
- [ ] T013 Implement `loadAdSegments` in `internal/planner/segment_processor.go`: read `<path>/vod.m3u8`, resolve renditions, read the primary rendition's `vod.m3u8`, and return segment specs with the marker path as the bucket-inclusive storage base
- [ ] T014 Add failing tests for boundary position selection and relative PTS computation in `internal/planner/segment_processor_test.go`
- [x] T015 Implement placement marker helpers in `internal/planner/segment_processor.go`: `0x30` at break start − 8 s and − 4 s, `0x36` at break end − 4 s, with `SpliceTimePTS=(currentPtsOffset-delay)*TICKS` and `SpliceTimeSeconds` set to the absolute channel position

**Checkpoint**: Parsing, break identity, padding, asset loading, and marker helpers are ready.

---

## Phase 3: User Story 1 — Ads come only from the XML EPG (Priority: P1) 🎯 MVP

**Goal**: Every XML marker becomes exactly one independent break; no marker means no break, regardless of runtime.

**Independent Test**: Plan a day with one marker and one long content programme; the timeline contains exactly one break at the marker start with that asset and duration. A day with no markers contains zero ad segments.

### Tests for User Story 1

- [ ] T016 [US1] Add failing tests in `internal/planner/segment_processor_test.go` for: one marker → one break; several markers → independent breaks in start order; no markers → zero ad segments; marker start behind the plan start skipped; `duration<=0` ignored; content exhausted before a future marker plays the break at content end

### Implementation for User Story 1

- [ ] T017 [US1] Rewrite the ad selection loop in `processStreamSegments` in `internal/planner/segment_processor.go` to build and sort the marker list, skip invalid/past markers, and call the break insertion path once per marker; remove the interval-trigger branch
- [ ] T018 [US1] Remove the content-programme ad path (`adBreakForProgramme`, `applyAdMarker` first/last handling) and its stale tests from `internal/planner/segment_processor.go` and `internal/planner/segment_processor_test.go`; programme-level ad programmes use the synthesized-marker path instead
- [ ] T019 [US1] Audit and remove dormant interval-ad and legacy JSON-schedule references from `internal/planner/planner.go`, `internal/utils/envs.go`, and `internal/api/partner_client.go` so `use_ad_epg_ads` and interval settings cannot influence the timeline
- [ ] T020 [US1] Verify XML-only ad behavior via skill `start planner` using `specs/014-xml-epg-scte35-pipeline/quickstart.md`: seed a channel with `db query` (`docker exec simlive_postgres psql -U postgres -d simlive`), run `make start-planner`, then inspect `docker exec simlive_redis redis-cli --raw ZRANGE simlive:timeline:<channel> 0 -1`; expect one break per marker, shared `ad_break_id`, and zero ad segments on a marker-free day

**Checkpoint**: US1 is independently demonstrable; interval ads are gone.

---

## Phase 4: User Story 2 — Boundaries are attached as ads approach (Priority: P1)

**Goal**: Each break carries 8 s/4 s pre-roll markers, a cue-out on the first ad, a placement-end 4 s before break end, and a cue-in on the first content segment after the break, all sharing one identity.

**Independent Test**: Inspect a known break: two `0x30` markers before it, one `0x34` on the first ad, one `0x36` at break end − 4 s, one `0x35` on the following content segment, and matching `ad_break_id` everywhere.

### Tests for User Story 2

- [ ] T021 [P] [US2] Add failing boundary tests in `internal/planner/segment_processor_test.go` covering both pre-roll positions, cue-out, placement-end, cue-in on content with `is_ad=false`, shared identity, and the case where pre-roll positions fall before the available timeline
- [ ] T022 [P] [US2] Add failing playlist-ordering tests in `internal/models/scte35_fields_test.go` asserting date-range CUE-OUT before the first ad, date-range CUE-IN before the following content, and per-segment `TYPE=48/52/53/54` tags

### Implementation for User Story 2

- [x] T023 [US2] Implement `insertAdBreak` in `internal/planner/segment_processor.go`: load the asset, compute the played duration from its segments, append ad segments with continuous `PtsOffset`, stamp the first ad as `ad_type="out"` with the `0x34` marker, and stamp the `0x36` placement-end marker in the ad segment containing break end − 4 s
- [x] T024 [US2] Implement proactive pre-roll stamping in `processSegment` in `internal/planner/segment_processor.go` so a marker is attached before `addSegment` flushes the segment; track emitted positions per break to prevent duplicates
- [x] T025 [US2] Carry a pending cue-in on `SegmentProcessor` across schedule-item boundaries in `internal/planner/segment_processor.go`; stamp the first following content segment with `ad_type="in"`, `AdBreakID`, `AdBreakStart`, and `AdBreakDuration`, without setting `IsAd`
- [ ] T026 [US2] Verify HLS boundary output via skill `start all` using `specs/014-xml-epg-scte35-pipeline/quickstart.md`: run `make start-all`, then `curl -s http://localhost:8080/live/<channel>/360p/playlist.m3u8`; expect CUE-OUT with `END-ON-NEXT=YES` before the first ad, CUE-IN before resumed content, `TYPE=48` twice before the break, `TYPE=52` on the first ad, `TYPE=54` before break end, and `TYPE=53` on the first content segment after

**Checkpoint**: US2 boundaries are visible in both the timeline and the playlist.

---

## Phase 5: User Story 3 — PTS stays continuous through content and ads (Priority: P1)

**Goal**: Playback offsets and presentation timestamps continue monotonically across every content/ad boundary; splice times use the channel timeline, not asset timestamps.

**Independent Test**: Walk a timeline with at least two breaks and assert `PtsOffset`, `MediaStart`, and `MediaEnd` never gap or rewind, and each `pts_time` matches the break position in the rewritten PTS space.

### Tests for User Story 3

- [ ] T027 [US3] Add failing PTS tests in `internal/planner/segment_processor_test.go` walking content→ad→content and two-break timelines; assert monotonic `PtsOffset`, continuous `MediaStart/MediaEnd`, `SpliceTimePTS=(currentPtsOffset-delay)*TICKS`, and `SpliceTimeSeconds` equal to the absolute break position

### Implementation for User Story 3

- [x] T028 [US3] Ensure ad segments inherit `currentPtsOffset` and advance it by their own durations in `internal/planner/segment_processor.go`; never derive output offsets from the ad asset's original PTS
- [x] T029 [US3] Audit all marker and segment PTS computations in `internal/planner/segment_processor.go` and `internal/utils/tsduck.go`; keep immediate splices free of `pts_time`, keep placement markers in the rewritten PTS space, and preserve the 33-bit mask at emission
- [ ] T030 [US3] Verify PTS continuity via skill `test`: run `make test` and confirm the new PTS tests pass together with existing `internal/models/scte35_fields_test.go` round-trip tests

**Checkpoint**: US3 has no discontinuities or rewind in the produced timeline.

---

## Phase 6: User Story 4 — Static PMT is preserved (Priority: P2)

**Goal**: Content, ad, and splice-bearing segments from one rendition declare the same SCTE-35 elementary stream, preserving spec 001.

**Independent Test**: Parse the PMT of a content segment, an ad segment, and a splice-bearing segment; the stream list, type `0x86`, PID `0x1F00`, and registration `0x43554549` are identical.

### Tests for User Story 4

- [ ] T031 [P] [US4] Add failing PMT invariant tests in `internal/utils/tsduck_test.go` asserting `SCTE35_PID=="0x1F00"`, both PMT command builders use `--add-pid 0x1F00/0x86 --add-registration 0x43554549`, and splice XML uses the correct immediate/pts_time shape per marker type

### Implementation for User Story 4

- [ ] T032 [US4] Confirm the new signaling flow does not alter PMT construction in `internal/utils/tsduck.go` or `internal/engine/engine.go`; if tests expose drift, restore the static values from spec 001 rather than copying main's ad-only PMT path
- [ ] T033 [US4] Verify PMT stability via skill `start all` using `specs/014-xml-epg-scte35-pipeline/quickstart.md`: run `make start-all`, then `./bin/pmt-verify -url http://localhost:8080/live/<channel>/360p/playlist.m3u8`; expect identical PMT content across content, ad, and splice-bearing segments, with the SCTE-35 stream declared in every segment

**Checkpoint**: US4 remains a hard invariant, not a regression risk.

---

## Phase 7: User Story 5 — EPG items come from the partner XML source (Priority: P1)

**Goal**: The planner uses only the partner XML source (plus retained-schedule fallback), and every item yields start, stop, primary stream, and ad markers.

**Independent Test**: Point the planner at a stub XML endpoint; the timeline uses exactly those items and markers, ignores the legacy JSON source, and omits items without a primary stream.

### Tests for User Story 5

- [ ] T034 [P] [US5] Add failing XML-source tests in `internal/api/partner_client_test.go` and `internal/planner/schedule_fallback_test.go` covering item mapping, missing-stream omission, retained-schedule fallback, and the absence of any second schedule source

### Implementation for User Story 5

- [ ] T035 [US5] Ensure `ProcessEPGToTimeline` and `RecoverFallbackDay` in `internal/planner/planner.go` call only `GetEPGScheduleItemsFromXML` plus the retained fallback; remove any reachable legacy JSON schedule path from the XML planning flow
- [ ] T036 [US5] Verify XML source behavior via skill `start planner` using `specs/014-xml-epg-scte35-pipeline/quickstart.md`: serve a stub XML guide, seed the channel with `db query`, run `make start-planner`, and confirm via `docker exec simlive_redis redis-cli --raw ZRANGE simlive:timeline:<channel> 0 -1` that only stub items and markers appear; then make the stub unavailable and confirm retained-schedule fallback still plans

**Checkpoint**: US5 proves the XML source is authoritative and fallback is intact.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Final gates and end-to-end validation.

- [ ] T037 [P] Run `make fmt`, `make vet`, `make test`, and `make build` from `makefile`; fix every failure before completion
- [ ] T038 Run all `specs/014-xml-epg-scte35-pipeline/quickstart.md` scenarios, including the negative cases (no ads, past marker, bad asset, zero duration, duplicate starts)
- [ ] T039 Stop started services, remove the test channel row and temporary fixtures, and record any implementation deviation in `specs/014-xml-epg-scte35-pipeline/research.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: T001 first; T002 and T003 can run in parallel after T001.
- **Foundational (Phase 2)**: Depends on Phase 1. T004→T005, T006→T007, T008→T009, T010→T011, T012→T013, T014→T015. Test tasks precede their implementation task.
- **User Stories (Phases 3–7)**: All depend on Phase 2 completion.
  - US1 → US2 → US3 run sequentially because they share `internal/planner/segment_processor.go` and its test file.
  - US4 is independent and can run alongside any planner story.
  - US5 can start after Phase 2, but T035 must follow US1 T019 because both edit `internal/planner/planner.go`.
- **Polish (Phase 8)**: Depends on all selected stories.

### User Story Dependencies

- **US1**: No dependency on other stories after Phase 2.
- **US2**: Depends on US1 T017 (the marker loop); uses Phase 2 helpers.
- **US3**: Depends on US2 T023 (break insertion); adds continuity assertions around it.
- **US4**: Independent; verification only.
- **US5**: Independent; parser work in Phase 2 is shared. Sequence T035 after US1 T019.

### Within Each Story

- Tests first, then implementation, then the verification skill task.
- Models and helpers before service flow.
- Do not start the verification task until `make test` passes for the new tests.

### Parallel Opportunities

- T002 and T003 in Phase 1.
- T004, T006, T008, and T010 are independent test files in Phase 2; T012 and T014 follow T008 in `internal/planner/segment_processor_test.go` and cannot run concurrently with it.
- After Phase 2: US1, US2, and US3 are sequential; US4 (T031–T033) can run in parallel with them; US5 (T034–T036) can run in parallel except T035, which follows T019.
- T021 and T022 are parallel test tasks in US2 because they touch different files.
- T037 runs first; T038 depends on T037 passing.

---

## Parallel Example: User Story 2

```bash
# Write the two independent test sets together:
Task: "Add boundary tests in internal/planner/segment_processor_test.go"
Task: "Add playlist-ordering tests in internal/models/scte35_fields_test.go"

# Then implement the planner flow:
Task: "Implement insertAdBreak in internal/planner/segment_processor.go"
Task: "Implement proactive pre-roll stamping in internal/planner/segment_processor.go"
Task: "Carry pending cue-in across items in internal/planner/segment_processor.go"
```

## Parallel Example: Foundational Tests

```bash
# All failing test suites can be written in parallel:
Task: "Parser tests in internal/api/partner_client_test.go"
Task: "Playlist cue-in tests in internal/models/scte35_fields_test.go"
Task: "Break identity tests in internal/planner/segment_processor_test.go"
Task: "Padding-shift tests in internal/planner/schedule_pad_test.go"
Task: "Ad-loader tests in internal/planner/segment_processor_test.go"
Task: "Boundary helper tests in internal/planner/segment_processor_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Complete Phase 1 and Phase 2.
2. Complete US1 (T016–T020).
3. **STOP and VALIDATE** with `start planner`: one marker → one break, zero markers → zero breaks.
4. Deploy/demo the XML-only ad behavior.

### Incremental Delivery

1. Foundation + US1 → XML-only breaks (MVP).
2. Add US2 → boundaries and playlist cue tags.
3. Add US3 → PTS continuity proof.
4. Add US5 → XML source and fallback proof.
5. Add US4 → PMT invariant proof.
6. Run Phase 8 full validation.

### Notes

- Every implementation task has a preceding failing test task; do not skip the red step.
- Keep `ad_break_id` shared across cue-out, pre-roll, placement-end, cue-in, and every ad segment.
- The cue-in segment is content: `is_ad=false`, `ad_type="in"`.
- `unique_program_id` is always masked to 16 bits; `EventID` uses the full counter.
- Do not modify the static PMT values while restoring main's signaling flow.
