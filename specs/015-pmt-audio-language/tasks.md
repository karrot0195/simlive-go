---
description: "Task list for Static PMT Audio Language"
---

# Tasks: Static PMT Audio Language

**Input**: Design documents from `/specs/012-pmt-audio-language/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/README.md, quickstart.md, `.specify/memory/constitution.md`

**Tests**: Unit tests for pure logic are included because `plan.md` and the constitution require them (`make test`, `make vet`). Integration validation uses the scenarios in `quickstart.md` against a running planner+engine+Redis+Postgres+TSDuck stack.

**Organization**: Tasks are grouped by user story so each story can be implemented and validated independently. Paths are repository-relative.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: User story from `spec.md` (US1, US2, US3)
- Every task includes an exact file path

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Document the new platform setting before code reads it.

- [X] T001 [P] Add `PMT_AUDIO_LANGUAGE=eng` with a one-line comment to the local environment file `.env`
- [X] T002 [P] Add `PMT_AUDIO_LANGUAGE=eng` with a one-line comment to the deployment template `config/.env`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Shared language resolution and command construction required by every user story.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T003 [P] Add `DefaultPmtAudioLanguage = "eng"` and `func GetPmtAudioLanguage() string` to `internal/utils/envs.go`: read `PMT_AUDIO_LANGUAGE`, trim whitespace, lowercase, accept only `^[a-z]{3}$` ("Three ASCII letters, case-insensitive"), and on empty/invalid input log a warning via `utils.GetLogger("CONFIG")` and return `eng`
- [X] T004 Add unit tests for language resolution in `internal/utils/envs_test.go` using `t.Setenv`: unset → `eng`, `ENG` → `eng`, `fra` → `fra`, `english`/`xx`/empty → `eng` (depends on T003)
- [X] T005 [P] Add an unexported PMT command builder and language argument helper in `internal/utils/tsduck.go` that emits `--audio-language <lang>` alongside the existing `--add-pid 0x1F00/0x86 --add-registration 0x43554549`, using the value from `GetPmtAudioLanguage()`; do not yet change the three public wrappers' behavior
- [X] T006 Add unit tests in `internal/utils/tsduck_test.go` asserting the builder output contains `--audio-language eng`, retains `--add-pid 0x1F00/0x86` and `--add-registration 0x43554549`, and uses the validated lowercase value (depends on T005)

**Checkpoint**: Language resolution and a single PMT command builder exist; user stories can proceed.

---

## Phase 3: User Story 1 - Audio Language Descriptor Never Drifts (Priority: P1) 🎯 MVP

**Goal**: Every prepared segment's PMT declares exactly one ISO 639 descriptor on its audio elementary stream with the configured language, whether the source PMT had `eng`, `und`, another value, or no descriptor. Consecutive segments from different origins (VOD → ad → VOD) present identical PMT content.

**Independent Test**: Prepare segments whose sources carry `eng`, `und`, `fra`, and no descriptor; inspect each PMT with `tsp -I file <seg> -P psi -O drop` and confirm one `Language: eng` descriptor, unchanged `version_number`, and unchanged SCTE-35 ES/CUEI. Then compare two consecutive prepared segments from different origins (`quickstart.md` scenarios 1–2).

### Implementation for User Story 1

- [X] T007 [US1] Replace the duplicated command strings in `ConfigurePmtForVodTs` and `ConfigurePmtForAdsTs` in `internal/utils/tsduck.go` with the shared builder so both emit `--audio-language <lang>`; keep the existing 5 s timeout and error string `[SCTE35] event=pmt_config_failed`
- [X] T008 [US1] Add `--audio-language <lang>` to the `-P pmt` plugin invocation in `InjectSpliceEvent` in `internal/utils/tsduck.go`, preserving `-P spliceinject --service 1 --pid 0x1F00 --files <xml> --wait-first-batch` and the 10 s timeout
- [X] T009 [US1] Update the behavior comment block at the top of `internal/utils/tsduck.go` to record `--audio-language` semantics: idempotent (byte-identical on re-run), does not change `version_number`, non-fatal `audio stream 1 not found` on audio-less input (exit 0, PMT unchanged), and uppercase codes must never be passed
- [X] T010 [US1] Extract the splice-injection command into a testable unexported builder in `internal/utils/tsduck.go` and extend `internal/utils/tsduck_test.go` to assert it contains exactly one `--audio-language` occurrence, `--pid 0x1F00`, and `-P spliceinject` (depends on T008)
- [X] T011 [US1] Validate `quickstart.md` scenarios 1 and 2 against prepared segments from a running engine and record PASS/FAIL with the observed PMT lines (depends on T007, T008)

**Checkpoint**: User Story 1 is fully functional and testable independently — normalization applies to all three PMT paths.

---

## Phase 4: User Story 2 - Normalization Covers Every Variant, Segment Type, and Cached Segment (Priority: P2)

**Goal**: No rendition or segment origin can serve a differently labelled PMT, and segments prepared before the fix are re-prepared so the partner never receives an old-vintage PMT.

**Independent Test**: After upgrading and restarting the engine, confirm `{TMP_DIR}/{channel}/.prep_version` contains `v1;audio-language=eng`, previously prepared `seg_*.ts`/`.pts` files were purged once, and segments on 360p, 720p, and 1080p all declare the configured language. Restart again with the same language and confirm no second purge (`quickstart.md` scenarios 3, 5, 7).

### Implementation for User Story 2

- [X] T012 [P] [US2] Create `internal/engine/prep_version.go` with `prepVersionValue()` returning `v1;audio-language=<lang>` and `(e *Engine) ensurePreparationVersion(ctx context.Context, logger *utils.Logger, channelID string)`: on missing/mismatched `{TMP_DIR}/{channelID}/.prep_version`, remove and recreate `{channelID}/{360p,720p,1080p}` directories, remove `{channelID}/playlist.m3u8`, delete Redis keys `simlive:hls:{channelID}` and `simlive:hls:{channelID}:rendition:{rendition}`, write the stamp atomically (temp + rename), and log `[PMT] event=prep_version_invalidated` with `channel`, `previous`, `current`
- [X] T013 [US2] Call `ensurePreparationVersion` from `StartPlayoutLoop` in `internal/engine/engine.go` after the channel/rendition directories are created and before the ticker loop begins (depends on T012)
- [X] T014 [US2] Add unit tests in `internal/engine/prep_version_test.go`: missing stamp purges a stale `seg_1.ts` + `seg_1.ts.pts` and writes the stamp; mismatched stamp purges; matching stamp preserves files; stamp content equals `v1;audio-language=eng` (depends on T012)
- [X] T015 [US2] Validate `quickstart.md` scenarios 5 and 7 against a running engine: confirm the one-time purge, no re-purge on same-language restart, and that AC-3 registration descriptors are preserved while video-only PMTs gain no audio ES (depends on T013)
- [X] T016 [US2] Validate `quickstart.md` scenario 3 for all active renditions using direct `tsp -I file <seg> -P psi -O drop` inspection (not the verifier, which is US3), confirming identical language on 360p, 720p, and 1080p (depends on T013)

**Checkpoint**: Every rendition and segment origin serves a normalized PMT; pre-fix cache cannot leak.

---

## Phase 5: User Story 3 - Language Drift Is Detectable by Verification Tooling (Priority: P3)

**Goal**: The PMT consistency check includes the ISO 639 descriptor in its fingerprint and reports which elementary stream/descriptor differs, so an `eng`/`und` flip can never again report "matches baseline".

**Independent Test**: Feed the verifier two segments that differ only in the audio language descriptor and confirm it reports a mismatch naming the stream and descriptor; feed consecutive `eng` segments and confirm every one matches (`quickstart.md` scenario 4).

### Implementation for User Story 3

- [X] T017 [US3] Change `InspectPmt` in `internal/verify/pmt.go` to run `ffprobe -print_format json -show_entries stream=index,codec_name,codec_type,id:stream_tags=language`, parse only the top-level `streams` array (not `programs[].streams`), add `Language string` to `StreamInfo` (`""` when the tag is absent), and include it in the fingerprint as `%04x:%02x:%s;` with `-` for a missing language
- [X] T018 [US3] Add `DiffPmt(baseline, current *PmtInfo) []string` to `internal/verify/pmt.go` producing lines such as `PID 0x0101 stream_type 0x0F: language "und" -> "eng"` and `... stream only in baseline/current`, and change `PmtBaseline.Check` to accept `*PmtInfo`, retain a full copy, and return `(baseline *PmtInfo, consistent bool, first bool)` (same file as T017)
- [X] T019 [US3] Update `cmd/pmt-verify/main.go`: adapt to the new `PmtBaseline.Check` signature, print `language=<lang>` (or `<none>`) per stream, add a `-expect-lang` flag defaulting to `utils.GetPmtAudioLanguage()` that warns `⚠️ audio language <v> on PID 0xNNNN (expected <lang>)` or prints `✅ audio language <lang> on all N audio stream(s)`, print the `DiffPmt` lines after `❌ PMT DIFFERS from baseline`, and load `.env` with a warning-only `godotenv.Load()`
- [X] T020 [P] [US3] Update `cmd/scte35-check/main.go`: include lines containing `ISO-639` and `Language:` in `computePmtFingerprint`, and on a baseline mismatch print the raw `tsp -P psi` lines added/removed relative to the baseline
- [X] T021 [US3] Add unit tests in `internal/verify/pmt_test.go`: fixture ffprobe JSON with `tags.language` and without → `Language` populated/empty; fingerprint differs between `eng` and `und`; identical for identical stream sets; `DiffPmt` reports the language change and added/removed streams (depends on T017, T018)
- [X] T022 [US3] Validate `quickstart.md` scenarios 3 and 4: `pmt-verify -expect-lang eng` prints a match for consecutive `eng` segments, and a deliberately rewritten `und` copy produces `❌ PMT DIFFERS` plus the element line (depends on T019, T020)

**Checkpoint**: All user stories are independently functional; a descriptor-only regression is caught and explained.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, repository checks, and end-to-end validation.

- [X] T023 [P] Update `.agents/skills/scte35-pmt-verify/SKILL.md`: add a language-drift scenario, document `pmt-verify -expect-lang`, correct the overstated "byte-for-byte identical" wording to reflect that the fingerprint covers PID + stream_type + language, and fix the broken `../../requests/` and `../../specs/` relative links
- [X] T024 [P] Record the new environment variable in `config/DEPLOY.md` under the deployment env-change log, following the existing entry format
- [X] T025 Run `make fmt`, `make vet`, and `make test` from the repository root (`makefile`, `go.mod`) and fix any failures introduced by this feature
- [X] T026 Run the full `specs/012-pmt-audio-language/quickstart.md` suite (scenarios 1–7) against the running stack and record the PASS/FAIL outcomes for the feature delivery report

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately (T001, T002 in parallel)
- **Foundational (Phase 2)**: Depends on Setup — BLOCKS all user stories (T003/T005 in parallel; T004 after T003; T006 after T005)
- **User Story 1 (Phase 3)**: Depends on Phase 2 — T007 → T008 → T009 → T010 → T011 (same file `internal/utils/tsduck.go`, sequential)
- **User Story 2 (Phase 4)**: Depends on Phase 2 — T012 → T013 → T014; T015/T016 after T013
- **User Story 3 (Phase 5)**: Depends on Phase 2 — T017 → T018 → T019/T020 → T021 → T022
- **Polish (Phase 6)**: Depends on all desired user stories

### User Story Dependencies

- **US1 (P1)**: After Phase 2; no dependency on US2/US3
- **US2 (P2)**: After Phase 2; independent of US1 and US3 (touches `internal/engine/`)
- **US3 (P3)**: After Phase 2; independent of US1 and US2 (touches `internal/verify/` and `cmd/`)

### Within Each User Story

- Command builder/implementation before call sites
- Call sites before integration validation
- Tests after the code they exercise compiles
- `tsp`/`ffprobe` integration validation last

### Parallel Opportunities

- T001 and T002 (different env files)
- T003 and T005 (different packages/files); T004 and T006 are separate files but depend on T003/T005 respectively
- After Phase 2, US1 (`internal/utils/tsduck.go`), US2 (`internal/engine/`), and US3 (`internal/verify/`, `cmd/`) can be developed concurrently by different developers
- T019 (`cmd/pmt-verify/main.go`) and T020 (`cmd/scte35-check/main.go`) are different files and can run in parallel once T017/T018 are done
- T023 and T024 (different documentation files)

---

## Parallel Example: Foundation + Stories

```bash
# Phase 2 in parallel:
Task: "Add GetPmtAudioLanguage() to internal/utils/envs.go"           # T003
Task: "Add PMT command builder with --audio-language in internal/utils/tsduck.go"  # T005

# After Phase 2, start stories in parallel:
Task: "US1: wire --audio-language into all three PMT paths in internal/utils/tsduck.go"
Task: "US2: implement ensurePreparationVersion in internal/engine/prep_version.go"
Task: "US3: extend InspectPmt with JSON + language in internal/verify/pmt.go"

# Within US3 once T017/T018 land:
Task: "Update cmd/pmt-verify/main.go for language output and diff"     # T019
Task: "Update cmd/scte35-check/main.go fingerprint and diff"           # T020
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 (env documentation)
2. Complete Phase 2 (language resolution + shared PMT command builder)
3. Complete Phase 3 (US1: normalize all three PMT paths)
4. **STOP and VALIDATE**: `quickstart.md` scenarios 1–2 — one `eng` descriptor per PMT, identical PMTs across a VOD → ad boundary
5. Deploy the engine if the partner-facing defect must be stopped immediately (add US2 before rollout for cached-segment invalidation)

### Incremental Delivery

1. Setup + Foundational → foundation ready
2. US1 → validate descriptor normalization → MVP
3. US2 → validate cache invalidation and all renditions → safe rollout to existing channels
4. US3 → validate drift detection → regression guard
5. Polish → docs, `make fmt/vet/test`, full quickstart run

### Scope Notes

- Do not change the ffmpeg PTS-rewrite command, download logic, playlist generation, ad decisioning, or rendition ladder.
- Do not pass `--increment-version` or `--new-version` (version policy unchanged).
- Do not treat TSDuck's `audio stream 1 not found in PMT` stderr line as a preparation failure.
- Language normalization targets the first audio stream (`A1`); the remux already collapses the served segment to a single audio ES.
- The prep-version purge is broader than strictly required by FR-010 (invalidates all cached segments once) — this is intentional and permitted by the spec.

---

## Notes

- `[P]` marks tasks that can run concurrently (different files, no incomplete dependency)
- `[USx]` maps each story-phase task to `spec.md`
- Each story ends at an independently testable checkpoint
- Run the repository checks (`make fmt`, `make vet`, `make test`) before declaring the feature complete
- Interface details live in `contracts/README.md`; entity/validation rules in `data-model.md`; decisions and measured evidence in `research.md`
