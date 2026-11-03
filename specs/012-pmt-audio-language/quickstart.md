# Quickstart: Static PMT Audio Language Validation

**Date**: 2026-11-03 | **Feature**: [Static PMT Audio Language](spec.md)

## Prerequisites

- Go 1.26+ toolchain
- TSDuck `tsp` in `PATH` (verified 3.44-4676)
- `ffmpeg` / `ffprobe` in `PATH`
- `rclone` configured for the project's object storage
- Docker Compose stack (Postgres 15, Redis 7) and a running planner + engine
- A channel with a nearby ad break (so a VOD → ad → VOD transition occurs)

## Setup

```sh
# Unit checks (language validation, command builders, fingerprint/diff, cache purge)
make test
make vet

# Build and start the stack
make build
make start-all

# Confirm the configured platform language
grep PMT_AUDIO_LANGUAGE .env        # default: eng
```

## Validation Scenarios

### Scenario 1: Descriptor normalization on a single segment

**Objective**: Prove the prepared PMT declares the configured language regardless of what the source PMT carried.

**Steps**:
1. Pick a prepared segment: `{TMP_DIR}/{channel_id}/{rendition}/seg_N.ts`.
2. Dump its PMT:
   ```sh
   tsp -I file "<segment.ts>" -P psi -O drop 2>/dev/null
   ```
3. Confirm the audio elementary stream carries exactly one descriptor:
   ```
   Elementary stream: type 0x0F (MPEG-2 AAC Audio), PID: 0x0101 (257)
   - Descriptor 0: ISO-639 Language (0x0A, 10), 4 bytes
     Language: eng, Type: 0x00 (undefined)
   ```
4. Confirm `Version: 0` (or the pre-existing value) is unchanged and the SCTE-35 ES + `CUEI` registration descriptor are still present.

**Expected**: One ISO 639 descriptor with the configured value (`eng`), no duplicate descriptor, no other PMT change. See [contracts/README.md](contracts/README.md) Contract 1.

---

### Scenario 2: Consecutive segments across an ad break are identical

**Objective**: Verify the partner-visible defect is gone: no audio-language drift between VOD and ad segments.

**Steps**:
1. Identify a VOD segment before an ad break and the first ad segment after it (engine logs: `channel playout`, segment sequence; prepared files under the rendition directory).
2. Extract both PMTs:
   ```sh
   tsp -I file "<vod_seg.ts>" -P psi -O drop 2>/dev/null > /tmp/pmt_vod.txt
   tsp -I file "<ad_seg.ts>"  -P psi -O drop 2>/dev/null > /tmp/pmt_ad.txt
   diff /tmp/pmt_vod.txt /tmp/pmt_ad.txt
   ```
3. Optionally compare the full PMT section bytes:
   ```sh
   tsp -I file "<vod_seg.ts>" -P tables --pid 0x1000 -O drop 2>/dev/null > /tmp/tab_vod.txt
   tsp -I file "<ad_seg.ts>"  -P tables --pid 0x1000 -O drop 2>/dev/null > /tmp/tab_ad.txt
   diff /tmp/tab_vod.txt /tmp/tab_ad.txt
   ```

**Expected**: No differences in the PMT content, including the audio language descriptor. `version_number` is identical in both (no spurious increments).

---

### Scenario 3: Live consistency check with `pmt-verify`

**Objective**: Exercise the descriptor-aware fingerprint and the `-expect-lang` confirmation.

**Steps**:
1. Run the watcher against every rendition of the channel:
   ```sh
   go run ./cmd/pmt-verify -channel "<channel_id>" -res 360p  -expect-lang eng -tsp
   go run ./cmd/pmt-verify -channel "<channel_id>" -res 720p  -expect-lang eng
   go run ./cmd/pmt-verify -channel "<channel_id>" -res 1080p -expect-lang eng
   ```
2. Watch the output for at least one ad break.

**Expected**:
- First segment: `📌 PMT baseline set`.
- Every subsequent segment: `✅ PMT matches baseline <fp>`.
- Each run prints `✅ audio language eng on all N audio stream(s)`.
- No `❌ PMT DIFFERS from baseline` line.
- No `⚠️ audio language ... (expected eng)` line.

---

### Scenario 4: Verifier catches a deliberate `eng`/`und` flip

**Objective**: Prove a descriptor-only difference is no longer invisible (FR-009 / SC-004).

**Steps**:
1. Copy two prepared segments into a scratch directory.
2. Rewrite the language of one copy to `und`:
   ```sh
   tsp -I file "<copy_a>.ts" -P pmt --audio-language und -O file "<copy_b>.ts"
   ```
3. Point `pmt-verify` at a directory containing both files (e.g. a temporary channel directory under `./tmp/simlive-go/`), or run the underlying check directly on each file.

**Expected**:
- The fingerprint differs between the two segments (`eng` vs `und`).
- On the second file the tool prints `❌ PMT DIFFERS from baseline <fp>` followed by a line naming the element, e.g. `PID 0x0101 stream_type 0x0F: language "und" -> "eng"`.
- No false `✅ PMT matches baseline`.

---

### Scenario 5: Cached pre-fix segments are invalidated

**Objective**: Verify no served segment retains a pre-fix PMT after rollout (FR-010).

**Steps**:
1. Before upgrading, note existing prepared files and the absence of a stamp:
   ```sh
   ls -la "{TMP_DIR}/{channel_id}/" "{TMP_DIR}/{channel_id}/360p/" | head
   ```
2. Deploy the new engine binary and restart `engines` (`make restart` or the service manager).
3. Inspect the channel directory:
   ```sh
   cat "{TMP_DIR}/{channel_id}/.prep_version"
   ls "{TMP_DIR}/{channel_id}/360p/"
   ```
4. Inspect the engine log for the invalidation event:
   ```sh
   grep "prep_version_invalidated" logs/engines.log
   ```

**Expected**:
- Stamp contains `v1;audio-language=eng`.
- Previously prepared `seg_*.ts` / `seg_*.ts.pts` files are gone and replaced by freshly prepared segments within a few playout ticks.
- Log line `[PMT] event=prep_version_invalidated` shows the old/new version.
- Restarting the engine a second time (same language) does **not** purge again.

---

### Scenario 6: Invalid configuration falls back to `eng`

**Objective**: Verify FR-013.

**Steps**:
1. Set `PMT_AUDIO_LANGUAGE=english` (or leave empty) in `.env`.
2. Restart the engine and wait for segment preparation.
3. Inspect the engine log and a prepared PMT.

**Expected**:
- A warning is logged that the configured language is invalid and `eng` is used.
- Every prepared PMT declares `Language: eng`.
- No preparation failures or skipped segments.

---

### Scenario 7: Edge cases (audio-less and AC-3)

**Objective**: Confirm normalization does not create or corrupt streams.

**Steps**:
1. Inspect a prepared segment whose source has AC-3 audio: the AC-3 registration descriptor must remain and the ISO 639 descriptor must read the configured language.
2. Inspect a prepared video-only segment: the PMT must not gain an audio elementary stream.

**Expected**: No new ES entries; codec-specific descriptors intact; no skipped segments attributable to the language step. (A benign `audio stream 1 not found` line may appear on stderr for audio-less input.)

---

## Expected Outcomes Summary

| Scenario | Pass Criteria |
|----------|---------------|
| 1 | Exactly one ISO 639 descriptor with the configured value; other PMT fields unchanged |
| 2 | PMT content identical across the VOD → ad boundary, including the language descriptor |
| 3 | `✅ PMT matches baseline` and `✅ audio language eng` on all renditions |
| 4 | Descriptor-only change produces a mismatch naming the stream and descriptor |
| 5 | Stamp written; stale segments purged once; no repurge on same-language restart |
| 6 | Invalid value → warning + `eng` in every PMT |
| 7 | No added/removed ES entries; framework descriptors preserved |

## References

- [Specification](spec.md)
- [Implementation plan](plan.md)
- [Research and decisions](research.md)
- [Data model](data-model.md)
- [Interface contracts](contracts/README.md)
- [Verification skill](../../.agents/skills/scte35-pmt-verify/SKILL.md)
- [Related feature: static SCTE-35 PMT](../001-static-scte35-pmt/quickstart.md)
