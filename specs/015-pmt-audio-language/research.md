# Research: Static PMT Audio Language

**Date**: 2026-11-03 | **Feature**: [Static PMT Audio Language](spec.md)

## Context

The partner reported that the PMT content still changes between segments: the ISO 639 language descriptor of the AAC audio stream alternates between `eng` and `und`, while the SCTE-35 elementary stream stays constant. The partner's demuxer treats any PMT content change (including descriptors) as a reset and drops its ad state.

All PMT mutation in the repo is TSDuck `tsp` invocations in `internal/utils/tsduck.go`; there is no Go MPEG-TS library. The PMT is produced by ffmpeg's mpegts muxer during `rewritePtsOffset` and then modified by `-P pmt` (and `-P spliceinject`) for each of three paths: VOD without splice events, ad without splice events, and splice-injected segments.

All experiments below were run on the sample segment `seg_86187.ts` (H.264 + AAC `eng` + SCTE-35 `0x1F00`, `tsp` 3.44-4676, ffmpeg/ffprobe installed).

## Decisions

### Decision 1: Normalize at the TSDuck PMT stage with `-P pmt --audio-language`

- **Decision**: Add `--audio-language <lang>` to the `-P pmt` plugin in all three existing PMT commands (`ConfigurePmtForVodTs`, `ConfigurePmtForAdsTs`, `InjectSpliceEvent`). Collapse the two duplicated `ConfigurePmtFor*` command builders into one unexported builder so the flag cannot be omitted on one path.
- **Evidence**: `tsp -P pmt --help` (3.44) documents:
  ```
  --audio-language language-code[:audio-type[:location]]
      Specifies the language for an audio stream in the PMT. ...
      The default location is "A1", ie. the first audio stream inside the PMT.
  ```
  Applying `-P pmt --audio-language eng` to segments whose source PMTs carried `eng`, `und`, no descriptor, and `fra` produced **byte-identical files** (same MD5) for all four variants. Applying it twice to an already-normalized file produced the same bytes. The `version_number` remained `0` in every case. Combining `--add-pid 0x1F00/0x86 --add-registration 0x43554549 --audio-language eng` in one plugin invocation produced output byte-identical to running the existing command followed by a separate language pass.
- **Rationale**: PMT mutation stays in the one module that already owns it, the operation is idempotent and deterministic (which is exactly the spec's invariant), and no additional process is spawned. The measured `tsp` run on a 700 KB segment is ~18 ms.
- **Alternatives considered**:
  1. **ffmpeg metadata during remux** — `ffmpeg -c copy -metadata:s:a language=eng` before the `-f mpegts` output. Verified to work (including replacement of `und`/`fra`, AC-3 descriptor preservation, and byte-identical output across variants). Rejected because it binds normalization to ffmpeg muxer metadata behavior and moves the responsibility out of the PMT stage; any future preparation path that bypasses the remux (or a remux flag change) would silently break the invariant.
  2. **Patch PMT bytes in Go** — requires a new MPEG-TS dependency or hand-rolled section parsing; violates the constitution's dependency/simplicity constraints.
  3. **Remove and re-add the descriptor** (`--remove-descriptor 0x0A --audio-language eng`) — unnecessary because `--audio-language` already replaces the value and stays idempotent; removing all language descriptors would also strip descriptors from subtitle/other streams.

### Decision 2: Configuration via `PMT_AUDIO_LANGUAGE`, validated in Go

- **Decision**: `utils.GetPmtAudioLanguage()` reads `PMT_AUDIO_LANGUAGE`, trims, lowercases, accepts `^[a-z]{3}$`, and falls back to `eng` with a structured warning otherwise. Documented in `.env` and `config/.env`.
- **Evidence**:
  - `tsp` rejects a 2-letter code (`* Error: pmt: invalid value "xx"`), while `--audio-language ENG` is accepted and writes an **uppercase** `Language: ENG` descriptor (not a valid ISO 639 presentation).
  - ffmpeg `-metadata:s:a language=english` silently writes **no** descriptor at all.
  - Both engines therefore need Go-side validation/normalization before the command is built.
- **Rationale**: FR-013 requires an operator-configurable platform value with a safe fallback and a warning. The helper matches the existing lazy `GetEnv`/`GetEnvInt` pattern in `internal/utils/envs.go` (no new `Settings` field).
- **Alternatives considered**: per-channel/per-asset override (explicitly out of scope in the spec clarifications); hard-coding `eng` (rejected by the user's environment-configuration preference).

### Decision 3: Multi-audio and non-AAC behavior

- **Decision**: Normalize the first audio stream (`A1`, the `--audio-language` default) and rely on the existing remux to define the served audio set. Do not add per-audio-position options.
- **Evidence**:
  - The engine's remux is `ffmpeg -i <seg> -c copy ... -f mpegts` with no `-map`, so ffmpeg's default stream selection emits exactly one audio stream. A synthetic 2-audio source produced a remux with a single audio ES.
  - `--audio-language eng:0:A2` on a single-audio PMT prints `* Error: pmt: audio stream 2 not found in PMT` (exit code still 0). Enumerating audio positions would therefore require an extra PSI pass per segment just to avoid error noise — unjustified when the remux already collapses to one audio stream.
  - With AC-3 audio (stream_type `0x81`), `--audio-language eng` preserved the AC-3 registration descriptor and set only the ISO 639 descriptor, so codec-specific descriptors are not corrupted.
- **Rationale**: The normalized PMT is produced after remux, so A1 is the whole audio set in the served output. This satisfies "each audio elementary stream is normalized" for the artifact that is actually served, without extra passes.
- **Alternative considered**: repeat `--audio-language <lang>:0:A<n>` for n = 1..N. Rejected: N is not known without an additional TSDuck/ffprobe pass, and out-of-range positions emit errors.

### Decision 4: Audio-less segments are a non-fatal no-op

- **Decision**: Keep the flag applied unconditionally.
- **Evidence**: On a video-only segment, `tsp -P pmt --audio-language eng` prints `* Error: pmt: audio stream 1 not found in PMT` to stderr, exits with status 0, and writes the output with an unchanged PMT (no audio ES added, nothing removed).
- **Rationale**: The existing wrappers either ignore stderr (`ConfigurePmtFor*` check only the process exit code) or inspect stdout (`InjectSpliceEvent`), so preparation is not failed. The spec's no-audio edge case (no-op, no audio stream added) holds.
- **Note for implementation**: do not start treating stderr text as a failure without filtering this message.

### Decision 5: Preserve the PMT version-number policy

- **Decision**: Do not pass `--increment-version` or `--new-version`.
- **Evidence**: The language pass leaves `Version: 0` unchanged, including when replacing `und` with `eng` and when re-running on an already-normalized segment. Combined with the static PMT strategy from `specs/001-static-scte35-pmt`, every served segment converges to the same PMT content, so no legitimate content change occurs and no version bump is required (FR-008).
- **Rationale**: Matches the established policy and the partner requirement that content be static; bumping per preparation would create spurious version churn.

### Decision 6: Invalidate prepared segments with a per-channel preparation-version stamp

- **Decision**: `{TMP_DIR}/{channelID}/.prep_version` contains `v1;audio-language=<lang>`. At playout-loop start, a missing/mismatched stamp causes the rendition directories to be purged and recreated, the channel/master playlists and Redis HLS keys to be deleted, and the new stamp to be written.
- **Evidence**: Cache validity today is marker-plus-size only (`seg_N.ts.pts` exists and the file is ≥ 1000 bytes); the key is effectively `(channel, rendition, sequence)`, so preparation-logic changes never invalidate anything. Re-processing an already-prepared file in place is unsafe because it already carries the added SCTE-35 ES and `CUEI` descriptor (`--add-registration` duplicates on re-runs), so the purge must remove the `.ts` files too, forcing a clean re-download from source.
- **Rationale**: One-time, channel-scoped cost; deterministic; no per-tick verification cost. The spec permits re-preparing or invalidating cached segments, not only the mismatching ones.
- **Alternatives considered**:
  1. **Verify each cached segment's PMT on the 500 ms tick** — would spawn hundreds of `tsp` processes per second across channels/renditions; rejected.
  2. **Marker filename versioning only** (`seg_N.ts.pts.v2`) — leaves the stale `.ts` in place; `prepareSegmentFromSource` skips the download when the file exists and would re-process already-prepared content; rejected.
  3. **Rewrite `TMP_DIR` on deploy** — operationally fragile; does not handle engine-only restarts or a language change without a directory change.

### Decision 7: Extend the verification fingerprint with the language descriptor

- **Decision**: Make `internal/verify.InspectPmt` parse ffprobe JSON and include `StreamInfo.Language` in the fingerprint; add `DiffPmt` and make `PmtBaseline` retain the full `PmtInfo`. In `cmd/pmt-verify`, add `-expect-lang` (default from `PMT_AUDIO_LANGUAGE`) and print the differing element. Extend `cmd/scte35-check`'s text fingerprint with `ISO-639` / `Language:` lines and print the differing lines on mismatch.
- **Evidence**:
  - Current fingerprint is `sha256("%04x:%02x;" per stream)` — PID + stream_type only. An `eng`/`und` flip hashes identically; that is exactly why every existing check reports "PMT matches baseline" while the partner sees resets.
  - `ffprobe -show_entries stream=...:stream_tags=language -of json` exposes `streams[].tags.language` (`eng`, absent for a missing descriptor). The JSON top-level `streams` array is clean; `programs[].streams` duplicates carry no tags, so parsing must use the top-level array (or dedupe by index).
  - `tsp -I file <seg> -P psi -O drop` prints `Descriptor N: ISO-639 Language (0x0A, 10)` + `Language: eng`, which `scte35-check`'s current line filter (`stream_type` / `Registration` / `Format identifier`) ignores.
  - `cmd/test-stream` already parses ISO 639 language but is undocumented, requires Redis+rclone, and only asserts an expected value rather than cross-segment stability.
- **Rationale**: FR-009/SC-004 require descriptor-aware detection and identification of the differing element; FR-012/SC-001 require an operator-visible confirmation of the normalized value.
- **Alternatives considered**:
  1. **Fingerprint raw `tsp -P psi` output in `internal/verify`** — captures all descriptors but introduces a TSDuck dependency into the default `InspectPmt` path (currently ffprobe-only) and relies on text stability across `tsp` versions; `scte35-check` already uses the raw text approach, so the two tools would converge on text fingerprinting. Deferred; ffprobe JSON is sufficient for the defect class and keeps `pmt-verify` runnable without TSDuck unless `-tsp` is set.
  2. **Full PMT byte comparison** — no stable, descriptor-aware extraction primitive currently exists in the repo; `tsp -P psi` text is close but version-fragile. Language-in-fingerprint plus per-stream diff satisfies the acceptance scenarios.
  3. **Update `cmd/hls-watch`** — its `Fingerprint` field is dead and it has no consistency verdict; out of scope (YAGNI).

## Experiment Log

| # | Input PMT language | Command | Result |
|---|--------------------|---------|--------|
| 1 | `eng` | `-P pmt --audio-language eng` | byte-identical to input |
| 2 | `und` | same | `eng`, byte-identical to #1 output |
| 3 | none | same | descriptor added `eng`, byte-identical to #1 output |
| 4 | `fra` | same | replaced with `eng`, byte-identical to #1 output |
| 5 | `eng` | run twice | byte-identical, single descriptor, `Version: 0` |
| 6 | two AAC streams | `--audio-language eng` | A1 set to `eng`, A2 untouched |
| 7 | two AAC streams | `--audio-language eng:0:A1 --audio-language eng:0:A2` | both set to `eng` |
| 8 | single AAC | `--audio-language eng:0:A2` | `* Error: pmt: audio stream 2 not found`, exit 0 |
| 9 | video only | `--audio-language eng` | `* Error: pmt: audio stream 1 not found`, exit 0, PMT unchanged |
| 10 | AC-3 (0x81) | `--audio-language eng` | AC-3 registration descriptor preserved, ISO 639 set |
| 11 | `none` | `--add-pid ... --add-registration ... --audio-language eng` | identical to sequential runs |
| 12 | `UND`/`ENG` casing | `--audio-language ENG` | writes `Language: ENG` (uppercase) — Go must lowercase |
| 13 | invalid (`xx`, `english`) | tsp | exit 1, invalid value error (no output) |
| 14 | ffmpeg `-metadata:s:a language=english` | remux | no descriptor written, no error |
| 15 | 2-audio source | `ffmpeg -c copy ... -f mpegts` | output has a single audio ES (default stream selection) |

## Dependencies

- **TSDuck `tsp`** (already required at runtime): `-P pmt --audio-language`, `-P psi` for verification.
- **ffprobe** (already used by `internal/verify`): JSON stream/tag output.
- No new Go module dependencies.

## Best Practices

- Validate and lowercase the language in Go; never pass raw operator input to `tsp`.
- Keep `tsp` command construction in `internal/utils/tsduck.go` only.
- Keep normalization idempotent and free of version bumps, matching the static-PMT strategy.
- Log cache invalidation and configuration fallback with structured fields.
- Treat cached segments as derived data: a preparation-schema change justifies a purge, not in-place mutation.
