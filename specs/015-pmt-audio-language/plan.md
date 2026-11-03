# Implementation Plan: Static PMT Audio Language

**Branch**: `012-pmt-audio-language` | **Date**: 2026-11-03 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/012-pmt-audio-language/spec.md`

## Summary

Normalize the ISO 639 language descriptor of the AAC audio elementary stream in every prepared segment's PMT to a single platform-wide value (default `eng`), so consecutive segments from different origins (VOD → ad → VOD) no longer present a PMT content change to the partner's demuxer.

The normalization is applied with TSDuck's existing PMT plugin (`-P pmt --audio-language <lang>`) in **all three** PMT paths (`ConfigurePmtForVodTs`, `ConfigurePmtForAdsTs`, `InjectSpliceEvent`), so no preparation path can emit a differently labelled PMT. The value is read from `PMT_AUDIO_LANGUAGE` (default `eng`), validated in Go (three lowercase ISO 639 letters) because both TSDuck and ffmpeg accept malformed values silently or inconsistently.

Cached pre-fix segments are invalidated by a per-channel preparation-version stamp (`{TMP_DIR}/{channel}/.prep_version`) that purges prepared artifacts when the stamp is missing or does not match the current language. Verification tooling is extended so the PMT consistency fingerprint includes the language descriptor and reports the differing elementary stream/descriptor instead of a blind "matches baseline".

## Technical Context

**Language/Version**: Go 1.26+ (module `simlive`)

**Primary Dependencies**: TSDuck `tsp` (verified 3.44-4676) for MPEG-TS/PMT manipulation; ffmpeg/ffprobe for PTS-offset remux and stream inspection; rclone for object storage; Redis for timeline and HLS playlist caches; `scte35-go` for HLS SCTE-35 tags. No new dependencies.

**Storage**: On-disk prepared-segment cache under `{TMP_DIR}/{channel}/{rendition}/seg_{seq}.ts` with zero-byte `.pts` markers; Redis keys for timeline (`simlive:timeline:*`) and playlists (`simlive:hls:*`).

**Testing**: `go test ./...` (unit tests for pure logic: language validation/command building, PMT fingerprint/diff, prep-version purge). Integration validation per Constitution Principle III requires the running planner+engine+API+Redis+Postgres+TSDuck stack; see [quickstart.md](quickstart.md).

**Target Platform**: Linux server (Docker Compose / systemd deployment under `config/`)

**Project Type**: Streaming server (simulated-live HLS playout engine)

**Performance Goals**: No measurable preparation latency increase. The change adds one flag to `tsp` invocations that already run; measured `tsp` invocation on a 700 KB segment is ~18 ms. No extra process per segment.

**Constraints**:
- Do not change the ffmpeg PTS-rewrite command, segment download logic, HLS playlist generation, ad decisioning, or rendition ladder.
- PMT must remain otherwise byte-identical to what the pipeline produces today (FR-006); only the language descriptor changes.
- `version_number` policy unchanged: no increment when content is unchanged (FR-008). Verified `--audio-language` does not bump the version.
- Normalization must be idempotent — re-running on an already-normalized segment produces identical bytes (FR-005).

**Scale/Scope**: All active channels and all active renditions (360p, 720p, 1080p). One normalization per segment preparation.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Compliance | Notes |
|-----------|------------|-------|
| **I. Schema-First Knowledge Management** | N/A | No knowledge-tree changes. |
| **II. Data-Plane vs Instruction-Plane Separation** | ✅ Pass | No `projects/` knowledge writes; spec artifacts stay under `specs/012-*`. |
| **III. Streaming-First Architecture** | ✅ Pass | Change is inside the playout PMT pipeline; integration validation against the running planner+engine+Redis+Postgres+TSDuck stack is required (quickstart scenarios 2–5). |
| **IV. Observability & Structured Logging** | ✅ Pass | Prep-version invalidation and language fallback are logged with `utils.Logger` structured fields (`channel`, `previous`, `current`, `lang`); existing PMT failure logs retained. |
| **V. Simplicity & YAGNI** | ✅ Pass | One validated env setting, one `tsp` flag per existing PMT command, one small cache-stamp helper. No new dependencies, no new service, no transcoding change. |
| **Architecture Constraints** | ✅ Pass | Configuration via `godotenv` + `internal/utils/envs.go`; TSDuck remains the single PMT writer; existing make targets used. |

**Gate Result**: ✅ All gates pass. No violations requiring complexity justification.

**Post-Design Re-check**: ✅ Still passes after Phase 1. The design touches only `internal/utils/tsduck.go`, `internal/utils/envs.go`, `internal/engine` (prep-version helper + one call site), `internal/verify/pmt.go`, and the two PMT verifier commands. No new services or dependencies.

## Project Structure

### Documentation (this feature)

```text
specs/012-pmt-audio-language/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── README.md
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/
├── utils/
│   ├── envs.go          # NEW: PMT_AUDIO_LANGUAGE resolution + ISO 639 validation
│   ├── tsduck.go        # MODIFIED: --audio-language on all three PMT commands
│   ├── envs_test.go     # NEW: language validation/default/fallback tests
│   └── tsduck_test.go   # NEW: command-builder tests (flag present, value normalized)
├── engine/
│   ├── engine.go        # MODIFIED: call ensurePreparationVersion in StartPlayoutLoop
│   ├── prep_version.go  # NEW: per-channel prep-version stamp + cache purge + Redis key cleanup
│   └── prep_version_test.go # NEW: purge/stamp behavior on a temp dir
├── verify/
│   ├── pmt.go           # MODIFIED: JSON ffprobe parse, language in StreamInfo/fingerprint, DiffPmt,
│   │                    #          PmtBaseline stores full PmtInfo
│   └── pmt_test.go      # NEW: fingerprint + diff tests using fixture JSON
cmd/
├── pmt-verify/main.go   # MODIFIED: print language per stream, diff elements on mismatch, -expect-lang
└── scte35-check/main.go # MODIFIED: descriptor lines in fingerprint; print differing lines on mismatch
config/
└── .env                 # MODIFIED: document PMT_AUDIO_LANGUAGE=eng
.env                     # MODIFIED: PMT_AUDIO_LANGUAGE=eng
.agents/skills/scte35-pmt-verify/SKILL.md  # MODIFIED: new language-drift scenario + -expect-lang usage
```

**Structure Decision**: Single Go project. PMT mutation stays centralized in `internal/utils/tsduck.go`; the cache-version concern lives next to the engine's cache/marker logic; the verification fingerprint stays in the shared `internal/verify` package with both PMT verifier commands updated.

## Design Outline

### 1. PMT language normalization (`internal/utils/tsduck.go`)

Add the validated language to every existing PMT command:

```text
tsp ... -P pmt --add-pid 0x1F00/0x86 --add-registration 0x43554549 \
        --audio-language <lang> -O file <out>
```

- `ConfigurePmtForVodTs`, `ConfigurePmtForAdsTs`, and `InjectSpliceEvent` all gain the flag.
- The two currently duplicated config functions delegate to one unexported command builder so a future path cannot omit the flag.
- `--audio-language` targets the first audio stream (`A1`); the pipeline's ffmpeg remux (`-c copy`, no `-map`) collapses to a single audio stream before this stage, so A1 is the only audio elementary stream in the served PMT (research Decision 3).
- Measured behavior (tsp 3.44): `eng` added when absent, `und`/`fra` replaced, already-`eng` unchanged (byte-identical), `version_number` stays `0`, other descriptors and ES entries preserved, exit code 0.

### 2. Configuration (`internal/utils/envs.go`)

`GetPmtAudioLanguage() string`:
- Reads `PMT_AUDIO_LANGUAGE`; default `eng`.
- Trims and lowercases.
- Accepts only `^[a-z]{3}$`; otherwise logs a warning via `utils.GetLogger("CONFIG")` and returns `eng`.
- Optional `Settings.PmtAudioLanguage` field is **not** added — the helper is called where needed (engine prep command builder, verifier default), matching the existing lazy-env pattern.

Add `PMT_AUDIO_LANGUAGE=eng` to `.env` and `config/.env` with a short comment.

### 3. Cached-segment invalidation (`internal/engine/prep_version.go`)

- Stamp file: `{TMP_DIR}/{channelID}/.prep_version`, content `v1;audio-language=<lang>`.
- `ensurePreparationVersion(ctx, logger, channelID)`:
  1. Read stamp; if it equals the expected value, return.
  2. Remove `{channelID}/{360p,720p,1080p}` directories and recreate them (removes `*.ts`, `*.pts`, temp files, per-rendition playlists).
  3. Delete `{channelID}/playlist.m3u8` and Redis keys `simlive:hls:{channel}` and `simlive:hls:{channel}:rendition:{r}` so no stale playlist is served in the gap.
  4. Write the stamp atomically (temp + rename).
  5. Log `[PMT] event=prep_version_invalidated` with old/new value and removed counts.
- Called once from `StartPlayoutLoop` after the channel/rendition directories are created, before the 500 ms ticker starts.
- Purge is broader than strictly "segments whose PMT lacks the language", which the spec permits ("re-prepared or invalidated"); it is simpler and guarantees no pre-fix PMT survives.

### 4. Verification tooling

`internal/verify/pmt.go`:
- Parse `ffprobe -print_format json -show_entries stream=index,codec_name,codec_type,id:stream_tags=language` into a struct; read the top-level `streams` array only (the `programs[].streams` duplicate has no tag data).
- `StreamInfo` gains `Language string` (empty when no descriptor).
- Fingerprint input becomes `pid:stream_type:language;` per stream (language `-` when absent), so `eng` vs `und` changes the fingerprint.
- New `DiffPmt(baseline, current *PmtInfo) []string` reporting per-stream differences such as `PID 0x0101 stream_type 0x0F: language "und" -> "eng"`, plus stream-added/stream-removed lines.
- `PmtBaseline.Check(info *PmtInfo) (baseline *PmtInfo, consistent bool, first bool)` stores the full baseline for diffing. Update the single caller.

`cmd/pmt-verify`:
- Print `language=<lang>` on each stream line; when a stream has no language descriptor, print `language=<none>`.
- New `-expect-lang` flag (default `utils.GetPmtAudioLanguage()`): warn per stream when the audio language differs from the expected value; print `✅ audio language eng on all N audio stream(s)` when it matches.
- On mismatch, print the `DiffPmt` lines (FR-009: identify the differing element without hex inspection).
- Load `.env` like the other commands (optional, warning only).

`cmd/scte35-check`:
- Include `ISO-639` / `Language:` lines in `computePmtFingerprint` so an `eng`/`und` flip is detected over HTTP.
- On mismatch, print the raw `tsp -P psi` lines that were added/removed versus the baseline.

`cmd/hls-watch` is unchanged (its fingerprint field is unused; no consistency verdict exists there).

### 5. Tests

- `internal/utils/envs_test.go`: default when unset, lowercase normalization (`ENG` → `eng`), invalid values (`english`, `xx`, empty) → `eng` (with warning path), valid custom code.
- `internal/utils/tsduck_test.go`: command builders include `--audio-language <lang>` and keep the existing `--add-pid/--add-registration` flags; language is interpolated after validation.
- `internal/verify/pmt_test.go`: fixture ffprobe JSON (with/without language, multi-stream) → `Language` populated; fingerprint differs for `eng` vs `und`; `DiffPmt` names the stream and descriptor change.
- `internal/engine/prep_version_test.go`: stale channel dir with `.ts`/`.pts`/temp files is purged and stamp written when missing/mismatched; matching stamp preserves files.

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| `tsp` version differences on deployment (verified on 3.44-4676) | Quickstart scenario 1 reproduces descriptor normalization directly; behavior is a documented `-P pmt` option. |
| Audio-less segment: TSDuck prints `audio stream 1 not found in PMT` to stderr but exits 0 and leaves the PMT unchanged | Existing code does not treat stderr text as failure; documented as a no-op (spec edge case). |
| Language value typo produces no descriptor (ffmpeg) or an error (tsp) | Go-side `^[a-z]{3}$` validation with lowercase normalization and `eng` fallback (FR-013). |
| Multi-audio source | ffmpeg remux (`-c copy`, no `-map`) selects one audio stream; the normalized PMT has a single audio ES, so A1 normalization covers every served audio stream. Verified with a 2-audio source. |
| Cache purge cost on upgrade | One-time purge per channel at loop start; window is bounded by `SIMLIVE_KEEP_BEHIND` + `SIMLIVE_DOWNLOAD_AHEAD`; playlists regenerate within one 500 ms tick. |
| Re-running the full PMT command duplicates the `CUEI` registration descriptor (pre-existing) | Unchanged: retries read the untouched input file, and each segment is configured once from source. Language normalization itself is idempotent. |

## Complexity Tracking

> No Constitution Check violations — section not required.

## Links

- Spec: [spec.md](spec.md)
- Research: [research.md](research.md)
- Data model: [data-model.md](data-model.md)
- Contracts: [contracts/README.md](contracts/README.md)
- Quickstart: [quickstart.md](quickstart.md)
- Related feature: [001-static-scte35-pmt](../001-static-scte35-pmt/spec.md)
- Verification skill: [scte35-pmt-verify](../../.agents/skills/scte35-pmt-verify/SKILL.md)
