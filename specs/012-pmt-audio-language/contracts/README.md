# Interface Contracts: Static PMT Audio Language

**Date**: 2026-11-03 | **Feature**: [Static PMT Audio Language](../spec.md)

The project exposes no new HTTP API. The contracts below cover the interfaces the feature touches: the TSDuck PMT command, the environment configuration, the on-disk cache stamp, and the verifier tooling output.

---

## Contract 1: PMT configuration command (`internal/utils/tsduck.go`)

**Functions**:
```go
func ConfigurePmtForVodTs(parentCtx context.Context, seg models.SegmentItem, inputFile, outputFile string) error
func ConfigurePmtForAdsTs(parentCtx context.Context, seg models.SegmentItem, inputFile, outputFile string) error
func InjectSpliceEvent(parentCtx context.Context, inputFile, outputFile string, se models.SpliceInsert) error
```

**Shared command shape** (one unexported builder feeds all three):
```sh
tsp [-a 1/10] -I file "<input>" \
    -P pmt --add-pid 0x1F00/0x86 --add-registration 0x43554549 \
           --audio-language <lang> \
    [-P spliceinject --service 1 --pid 0x1F00 --files "<xml>" --wait-first-batch] \
    -O file "<output>"
```

**Invariants**:
- `<lang>` is the value returned by `utils.GetPmtAudioLanguage()`; it matches `^[a-z]{3}$` before interpolation.
- The audio language descriptor on the audio elementary stream is added when absent, replaced when different, and left single/unchanged when it already equals `<lang>`.
- `version_number` is not modified (`--increment-version`/`--new-version` are not passed).
- All other PMT fields are unchanged (ES list, stream types, PIDs, SCTE-35 ES, CUEI).
- Audio-less input: the command exits 0 and the PMT is unchanged (TSDuck emits a non-fatal `audio stream 1 not found` line on stderr).
- Timeouts: 5 s for PMT configuration, 10 s for splice injection (unchanged).
- Failure handling is unchanged: caller retries once, then removes the temp file and skips the segment with a structured log entry.

**Idempotency**: Re-running the language normalization on an already-normalized segment is byte-identical. (The pre-existing `--add-registration` duplication on repeated full runs is unchanged and is avoided because each segment is configured once from source.)

---

## Contract 2: Environment configuration

**Variable**: `PMT_AUDIO_LANGUAGE`

| Property | Value |
|----------|-------|
| Required | No |
| Default | `eng` |
| Accepted | Three ASCII letters, case-insensitive (`eng`, `fra`, `deu`, …) |
| Normalization | Trimmed and lowercased |
| Invalid / empty | Falls back to `eng`; emits a warning through `utils.Logger` |
| Scope | Platform-wide; applies to every channel and rendition |
| Propagation | Read by the engine when building PMT commands and used as the default for `pmt-verify -expect-lang` |

**Documentation**: present in `.env` and `config/.env`:
```sh
# ISO 639 language code written into every PMT audio descriptor
PMT_AUDIO_LANGUAGE=eng
```

---

## Contract 3: Preparation-version stamp (on-disk)

**Path**: `{TMP_DIR}/{channelID}/.prep_version`

**Content**: single line, `v1;audio-language=<lang>`

**Semantics**:
- Read once at channel playout-loop start, after the channel/rendition directories exist.
- Missing or differing content → purge `{TMP_DIR}/{channelID}/{rendition}/` for `360p`, `720p`, `1080p`; remove `{TMP_DIR}/{channelID}/playlist.m3u8`; delete Redis keys `simlive:hls:{channelID}` and `simlive:hls:{channelID}:rendition:{rendition}`; write the expected content atomically.
- Matching content → no cache action.
- Purge removes `*.ts`, `*.pts`, temp artifacts, and per-rendition playlists; segments are re-downloaded from source.
- Failure to write the stamp is logged and does not stop the playout loop (the next start retries).

---

## Contract 4: Verifier tooling

### `cmd/pmt-verify`

**Flags**:

| Flag | Default | Behavior |
|------|---------|----------|
| `-channel` | interactive prompt | Channel directory to watch under `./tmp/simlive-go/` |
| `-res` | `360p` | Rendition subdirectory |
| `-pmt` | `true` | PMT inspection and consistency check |
| `-tsp` | `false` | Add raw `tsp -P psi` summary |
| `-expect-lang` | `PMT_AUDIO_LANGUAGE` or `eng` | **NEW** Expected audio language |

**Output additions**:
- Per stream: `• PID 0x0101 stream_type 0x0F language=eng codec=aac`
- Language verdict: `✅ audio language eng on all 1 audio stream(s)` or `⚠️ audio language und on PID 0x0101 (expected eng)`
- On mismatch, after `❌ PMT DIFFERS from baseline <fp>`, one line per differing element, e.g.:
  ```
     └─ PID 0x0101 stream_type 0x0F: language "und" -> "eng"
  ```

**Fingerprint contract**: `sha256("pid:stream_type:language;" …)[:6]`; language `-` when absent. A descriptor-only change must change the fingerprint.

### `cmd/scte35-check`

**Fingerprint contract**: the existing raw `tsp -P psi` line concatenation now also includes lines containing `ISO-639` and `Language:`. On a baseline mismatch, the tool prints the lines added/removed relative to the baseline so the differing descriptor is identifiable.

---

## Contract 5: TSDuck `tsp` (external dependency)

**Version**: verified with 3.44-4676.

**Relevant options**:
- `-P pmt --audio-language language-code[:audio-type[:location]]` — default location `A1` (first audio stream).
- `-P pmt --add-pid <pid>/<stream_type>`, `--add-registration <hex>` — unchanged.
- `-P psi -O drop` — descriptor dump used by verification.

**Observed semantics relied upon**:
- `--audio-language` is idempotent and does not bump `version_number`.
- Out-of-range audio position produces a non-fatal error line (exit 0).
- Uppercase codes are written verbatim → argument must be lowercased by the caller.
