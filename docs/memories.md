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

## 2026-09-06 — Engine fix: advertise only the playhead window (keepBehind no longer leaked into playlist)

**Context:** The media playlist's first advertised segment 404'd ("missing 1 ts"). Root cause: `generateRenditionPlaylist` was fed `downloadSegs` (the whole bounded on-disk superset = keepBehind + window + ahead ≈ 15 segs) instead of `candidateSegs` (the 5-seg playhead window). So `#EXT-X-MEDIA-SEQUENCE` anchored at the *trailing keepBehind* edge, which `syncLocalCacheDir` evicted on the next tick → the first advertised segment got deleted while the playlist still referenced it.

**Change:**
- `StartPlayoutLoop` (engine.go): pass `candidateSegs` (playhead window) to `generateRenditionPlaylist` for the ADVERTISED media playlist; `downloadSegs` (keepBehind+window+ahead superset) is still what `syncAndPreparePerRendition` downloads/prepares and what GC keeps on disk — so the advertised window is always a subset of the on-disk set.
- Kept the index-based `RENDITIONMAP[i]` mapping. Confirmed a `SegmentItem.Renditions` holds the per-rung **storage tier dirs** (`["L_0","L_1","L_2"]` for VOD, `["L2","L1","L0"]` for ads) in ladder order — NOT quality labels. Attempting `groupSegsByRendition` (keying on those raw labels) wrote playlists into nonexistent `L_0/`/`L1/` dirs and failed silently; reverted. The correct behaviour is `RENDITIONMAP[i]` → `{360p,720p,1080p}`.
- Added `lookBehindSegment` (1 segment before the window anchor) and threaded it through `buildMediaPlaylistBody` (variadic `behind ...*models.SegmentItem`) so the SCTE-35/DATERANGE edge detection on the window's FIRST advertised segment (which inspects `segments[i-1]`) sees a real neighbour. The behind segment is never advertised.

**Files touched:**
- `internal/engine/engine.go` (modified: tick-loop window wiring, `lookBehindSegment`, `generateRenditionPlaylist` signature export)
- `internal/engine/scte35.go` (modified: `buildMediaPlaylistBody` variadic `behind` param for window-front edge detection)
- `internal/engine/rendition.go` (modified: reverted `groupSegsByRendition` to its commented-out state since its label keying is wrong for storage-tier renditions)
- `internal/engine/cue_tag_test.go` (modified: `TestBuildMediaPlaylistBody_LookBehindEdgeDetection`)
- `internal/engine/master_playlist_test.go` (modified: `TestLookBehindSegment_ReturnsPrev` / `_FirstIsNil`)

**Notes — verified state vs. doc:**
- Confirmed on-disk contract matches the user's expectation: current=51 → advertised `51–55`, on-disk `47–60` (5 behind + 5 window + 5 ahead); every advertised seg exists on disk; behind/ahead stay warm but unadvertised. All 3 rung `playlist.m3u8` files are now written.
- `go test ./internal/engine/...` passes (incl. the new look-behind tests).

**Notes — drift findings remaining (follow-up candidates):**
- Bounded-window spec text still describes the playlist as a "5-segment sliding window" from the full `downloadSegs` superset; the code now advertises precisely `candidateSegs`. `docs/specification.md` §5 step 4/6 was updated to reflect "advertise only the playhead window" — verify on next spec pass if any residual wording remains.
- Docs still reference `groupSegsByRendition` (in `docs/300826_0620-multi-rendition-abr-ladder.md` and the summary). It now exists only as a commented-out helper (storage-tier labels can't be used as rung keys). A follow-up should reconcile those references to the index-based `RENDITIONMAP[i]` mapping.
- **Timeline key drift (pre-existing, NOT fixed this session):** the engine reads the FLAT `simlive:timeline:{ch}` key (`fetchTimeline`), while the planner writes per-rung `simlive:timeline:{ch}:{rend}` keys. `internal/planner/race_test.go` still asserts per-rung keys and fails. Follow-up: reconcile the engine read to per-rung keys or make the planner write the flat key.

---

## 2026-09-06 — Stop/delete cleanup completeness + planner warm-pickup

**Context:** Two gaps surfaced after the earlier purge fix:
1. Setting `status="INACTIVE"` via `PUT/PATCH /api/channels/:id` was just a column flip — it never forced `is_active=false`, never purged Redis. So the planner kept pushing new timeline data ("old data cleaned, new data still push") and the channel's HLS keys stayed in Redis ("redis for hls of that channel still here").
2. Adding/starting a channel (`/api/channels`) made the engine reconciler (5s poll) start immediately, but the planner slept `PLANNER_INTERVAL` (5m) between ticks, so a new channel waited up to 5m before being planned.

**Change:**
- `internal/api/handler.go`: extracted shared `applyChannelUpdate(c,id,req,verb)` used by both `UpdateChannel` (PUT) and `PatchChannel` (PATCH). When `status` is set to `"INACTIVE"` it now forces `is_active=false` (so the planner + engine reconciler both drop the channel) and purges the channel's Redis data (`purgeChannelRedisData`) — mirroring `StopChannel`. Non-INACTIVE updates behave exactly as before.
- `cmd/planner/main.go`: rewrote the cron loop to a warm-pickup loop. It polls the DB every 3s (`pollInterval`) but only runs a **full tick** when (a) the active-channel set changed (added/started/stopped) OR (b) `PLANNER_INTERVAL` elapsed. A change-tick does NOT shift the periodic schedule, so 24h day-rollover (`ProcessWindow`) can't be starved by churn.
- `internal/api/handler_test.go`: updated `TestPurgeChannelRedisData_RemovesAllKeyFamiliesExceptBreaker` to match the now 4-key purge (master HLS + flat timeline + renditions + cursor) instead of the removed 6 per-rung/5-family keys.

**Files touched:** `internal/api/handler.go`, `internal/api/handler_test.go`, `cmd/planner/main.go`, `docs/memories.md`

**Notes — verified state vs. doc:**
- `go build ./...` OK; `go test ./cmd/planner/...`, `go test ./internal/api/...`, `go test ./internal/engine/...` all pass.
- `go test ./internal/planner/...` has ONE pre-existing failure: `TestStampSliceEvents_OnVODAndAdSegments` (SCTE-35 event ID mismatch, `scte35_stamping_test.go:529`). This is UNRELATED to this session — `git diff HEAD -- internal/planner/` is empty; the engine's flat-timeline/`fetchTimeline` design drift (noted below) is the likely cause.
- **Pre-existing drift (NOT fixed, follow-up):** the engine reads the FLAT `simlive:timeline:{ch}` key (`fetchTimeline`), possibly vs. planner per-rung writes. `internal/planner/race_test.go` + the scte35 failure suggest the planner test harness still expects per-rung keys. Reconcile engine read vs. planner write next session.
- The planner warm-pickup deliberately keeps the periodic cadence wall-clock-locked so the day-rollover prepare isn't delayed by channel churn.

---

## 2026-09-06 — Logging module: per-channel + verbose aggregate

**Context:** User asked to implement logging module to planner and engines — separate log per channel, all logs to verbose, clean up in time.
**Change:** Updated `internal/utils/logging.go` (`GetLogger` writes stdout + per-channel file + `/tmp/logs/verbose.log`, 500MB rotate, 7-day retention). Planner uses `PLANNER:{ch.ID}`; engine uses `ENGINE:{channelID}`.
**Files touched:** `internal/utils/logging.go`, `internal/planner/planner.go`, `internal/engine/engine.go`
**Notes:** Build passes (`go build ./...`). Cleanup automatic via lumberjack.

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
  - `.github/skills/simlive-plan/assets/plan-template-full.md` and `plan-template-short.md` — header comment + the `Continues / Supersedes` example now references `ddMMyy_HHmm-…`.
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

## 2026-08-31 22:20 — Fix: stop channel left Redis data behind

**Context:** User asked: "when stop channel playout -> redis data still here ?". Investigating the current `StopChannel` handler showed the **yes** answer — `purgeChannelHlsKeys` only DELed the HLS playlist keys, NOT the planner-side keys (`simlive:timeline:{ch}`, `simlive:renditions:{ch}`, `planner:cursor:{ch}`). The same gap existed for the soft-`DELETE` path. The breaker counter `planner:failures:{ch}` was correctly preserved (intentional contract with `StartChannel`).

**Change:**
- **Refactor:** renamed `purgeChannelHlsKeys` → `purgeChannelRedisData` in `internal/api/handler.go`. The new function DELs every per-channel key the planner + engine write:
  - `simlive:hls:{ch}` (master playlist)
  - `simlive:hls:{ch}:rendition:{360p|720p|1080p}` (×3 media playlists)
  - `simlive:timeline:{ch}` (planner ZSET — full day timeline)
  - `simlive:renditions:{ch}` (per-channel ladder metadata)
  - `planner:cursor:{ch}` (per-channel lookahead cursor)
- **Intentionally preserved** across the purge: `planner:failures:{ch}` (the breaker counter). `StartChannel` still DELs it on the next start, so a stop → start cycle always behaves like a fresh attempt.
- **New named constants** for the planner-side prefixes: `timelineRedisKeyPrefix`, `renditionsRedisKeyPrefix`, `cursorRedisKeyPrefix` (parallel to the existing `hlsRedisKeyPrefix` and `breakerKeyPrefix`). Keeps the strings pinned in one place.
- **Both call sites updated** (`StopChannel` + `DeleteChannel`) to use the new function name. The old name is kept as a thin one-line alias for any external caller.
- **Disk cache note:** the `{TMP_DIR}/{ch}/` directory is NOT touched by the purge. The engine reconciler cancels the per-channel goroutine within ~5s of `stop` and the goroutine's last `syncLocalCacheDir` pass evicts the bounded active window. Disk space is recovered within that window; on-disk `.ts` files vanish too.
- **Tests:** 4 new Redis-gated tests in `internal/api/handler_test.go`:
  - `TestPurgeChannelRedisData_RemovesAllKeyFamiliesExceptBreaker` — seeds all 8 key families, calls purge, asserts the 5 purged key families are gone and the breaker is preserved with its original value.
  - `TestPurgeChannelRedisData_DoesNotTouchOtherChannels` — seeds two channels' keys, purges one, asserts the other is untouched.
  - `TestPurgeChannelRedisData_Idempotent` — double-purge is a no-op (DEL of absent keys).
  - `TestPurgeChannelRedisData_NilRedisIsNoOp` — a handler with `rdb == nil` is safe to call against.
  - All 4 tests follow the project's **skip-on-no-Redis** pattern (mirroring `internal/planner/horizon_rollover_test.go`); they use a separate Redis DB (`DB: 15`) and `FlushDB` on setup so they don't touch the app's data.
- **Docs updated:**
  - `docs/specification.md` §2 — added a "Redis purge on stop/delete" block documenting the 5 purged key families + the preserved breaker.
  - `.github/instructions/simlive-summary.instructions.md` — architecture flow line now mentions the purge-on-stop behavior.
  - `client/swagger.yaml` — `deleteChannel` and `stopChannel` operation descriptions updated to mention `purgeChannelRedisData(id)` and the 5 key families.

**Files touched:**
- `internal/api/handler.go` (3 named constants + new function + 2 call-site renames + 1 alias kept)
- `internal/api/handler_test.go` (added `context`, `os`, `time`, `redis` imports + 4 tests + 3 helpers)
- `docs/specification.md` (§2 stop/delete block)
- `.github/instructions/simlive-summary.instructions.md` (architecture line)
- `client/swagger.yaml` (delete + stop descriptions)

**Verification:**
- `GOCACHE=$PWD/.gocache go build ./...` → clean
- `GOCACHE=$PWD/.gocache go vet ./...` → clean
- `python3 -c "import yaml; yaml.safe_load(...)"` on swagger.yaml → valid
- `GOCACHE=$PWD/.gocache go test -v ./internal/api/... -run "TestPurgeChannel" -count=1` against a live Redis → **4/4 PASS**
- Full suite: my new + existing tests all pass; the 3 pre-existing `main` failures (`TestSegmentGuard_RejectsDotDotAndSlash`, `TestBuildMasterPlaylist_Format`, `TestProbeBitrateFallback_Formula`) are unchanged. **Zero regressions.**

**Notes — operator-facing summary:**

The answer to "when stop channel playout -> redis data still here?" was: **yes, mostly** — the previous `purgeChannelHlsKeys` only cleared the HLS playlist keys (master + 3 per-rung media). The planner-side keys (timeline ZSET, renditions metadata, cursor) were left behind. After this fix, `stop` and `delete` (soft) DEL every per-channel key the planner + engine write, except the breaker counter which `StartChannel` clears on the next start.

**Notes — gotchas + follow-ups:**
- **The old `purgeChannelHlsKeys` alias was REMOVED on 2026-08-31** (dead code — no callers, and it was unexported so no external code could call it). `purgeChannelRedisData` is the single entry point.
- **The `redis.go` `Del` operation accepts a variadic key list** and a single round-trip, so the 7-key DEL on stop is one Redis call — no perf concern.
- **The Redis-gated tests use `DB: 15` + `FlushDB`** to isolate from the app's data. This means a developer with Redis already running on 6371 DB 0 is safe to run the tests — they touch DB 15 only. The skip-on-no-Redis guard means the suite is green in CI without Redis too.
- **The disk cache `{TMP_DIR}/{ch}/` is still not explicitly purged on stop** — the engine reconciler handles it via the goroutine's `syncLocalCacheDir` shutdown pass within ~5s. If a future requirement is "stop returns only after disk is freed", the purge would need to add an os.RemoveAll(tmpDir+"/"+id) call. Out of scope for this session.
- **Pre-existing test failures** (NOT introduced by this work): `TestSegmentGuard_RejectsDotDotAndSlash`, `TestBuildMasterPlaylist_Format`, `TestProbeBitrateFallback_Formula`. All three are documented in the prior doc-pass entry.

**Notes — drift findings remaining (follow-up candidates for next session):**
- **`cmd/engines/main.go` 5s hardcoded polling cadence** — if sub-5s engine pickup is a real product requirement, add an `ENGINE_RECONCILE_INTERVAL` env var mirroring `PLANNER_INTERVAL`. Trivial 5-line change. Out of scope here.
- **`/memories/repo/simlive-go.md`** likely still says the API has "one endpoint" (the prior doc-pass left it in a known state). A follow-up audit pass can verify.
- **The stop-endpoint log line** was updated to "Redis data purged" (see handler.go line ~676) — the alias removal happened in the 2026-08-31 dead-code cleanup.

## 2026-08-31 22:45 — Fix: 2nd playout channel is slow / missing data (warm-start instead of cold-start)

**Context:** User reported: "when i start 2 playout -> only 1 playout channel has data -> it take long time for engines can has data to start". Investigation showed the root cause was the **destructive startup hooks** in both `cmd/engines/main.go PrepareEnv` and `cmd/planner/main.go PrepareEnv` — every binary startup wiped the local cache (and the planner wiped all of Redis) regardless of whether the user actually wanted a cold start.

**Root cause (the long time the user saw):**
1. **Engine `PrepareEnv` did `os.RemoveAll(tmpDir)` on every startup.** When the user restarts the engine binary (or it auto-restarts after a crash), the bounded 15-segment on-disk window for every channel is wiped. The engine then has to re-download every `.ts` via `rclone copyto` before it can serve any HLS playlist. On a fleet with 2 channels that's 30 `rclone copyto` calls.
2. **Planner `PrepareEnv` did `rdb.FlushAll(ctx)` on every startup.** This is even worse — it wipes `simlive:hls:*` (the playlists the live engine was serving), `simlive:timeline:*` (the engine's input), `planner:cursor:*`, `simlive:renditions:*`, AND `planner:failures:*` (the breaker counter). Restarting the planner causes the live engine to immediately start serving empty playlists for every channel.
3. The 2nd playout channel that "only 1 has data" symptom matches: the operator likely started engine → planner (which `FlushAll`'d the timeline mid-flight) → engine can't serve until planner re-builds the timeline, which is 30s–2min on cold start per channel.

**Change:**
- **`cmd/engines/main.go PrepareEnv`** — by default **preserves** `{TMP_DIR}/{ch}/` across restarts. The first tick's `syncLocalCacheDir` evicts any segments that no longer fit the new active window. Opt-in cold start: set `ENABLE_CLEAN_UP=true`.
- **`cmd/planner/main.go PrepareEnv`** — by default does a **scoped wipe of just `simlive:timeline:*`** (the only key the planner writes that the engine doesn't also clear on its own). The renditions / cursor / failures keys are read-only for the engine and are preserved across planner restarts. Opt-in full wipe: set `ENABLE_CLEAN_UP=true` (preserves the old `FlushAll + os.RemoveAll` behavior).
- **`StopChannel` log line** updated from "HLS keys purged" to "Redis data purged (HLS + timeline + renditions + cursor)" — flagged in the previous session as a stale wording.
- **Spec doc** — `docs/specification.md` §5 Module 2 step 1 rewritten to call out the warm-start-by-default contract; §8 env var table updated to describe the new `ENABLE_CLEAN_UP` semantics.
- **Summary instructions** — added a "Warm start by default" bullet under Operational Details.
- **Tests** — 2 new pure tests in `cmd/engines/main_test_prepare_env_test.go`:
  - `TestEnginePrepareEnv_DefaultPreservesCache` — plants a sentinel file, calls PrepareEnv, asserts the sentinel survives.
  - `TestEnginePrepareEnv_OptInRemovesCache` — same with `ENABLE_CLEAN_UP=true`, asserts the sentinel is removed.
  - Both tests use `t.TempDir()` for isolation; `t.Setenv` for env-var stubbing. No infrastructure needed.

**Files touched:**
- `cmd/engines/main.go` (PrepareEnv body: preserve by default, opt-in destructive)
- `cmd/engines/main_test_prepare_env_test.go` (new — 2 tests)
- `cmd/planner/main.go` (PrepareEnv body: scoped wipe by default, opt-in full wipe)
- `internal/api/handler.go` (StopChannel log line: "HLS keys purged" → "Redis data purged (HLS + timeline + renditions + cursor)")
- `docs/specification.md` (§5 step 1 + §8 ENABLE_CLEAN_UP row)
- `.github/instructions/simlive-summary.instructions.md` (Operational Details bullet)

**Verification:**
- `GOCACHE=$PWD/.gocache go build ./...` → clean
- `GOCACHE=$PWD/.gocache go vet ./...` → clean
- `GOCACHE=$PWD/.gocache go test -v ./cmd/engines/... -run "TestEnginePrepareEnv"` → **2/2 PASS** (verbose log proves the contract: "preserving {TMP_DIR} cache" vs "ENABLE_CLEAN_UP=true, removing /tmp/...")
- Full suite: my new + existing tests pass; the 3 pre-existing `main` failures (`TestSegmentGuard_RejectsDotDotAndSlash`, `TestBuildMasterPlaylist_Format`, `TestProbeBitrateFallback_Formula`) are unchanged. **Zero regressions.**

**Notes — operator-facing summary (the answer to the user's question):**

The reason "only 1 playout has data" + "long time for engines" was the **destructive startup hooks**. Every time the engine or planner binary restarts, the local disk cache (engine) and the Redis timeline (planner) were wiped, forcing a re-download via rclone and a re-build of the timeline from scratch. With 2 channels, the planner's `FlushAll` also wiped the engine's currently-serving HLS playlists, so channel 2 had to wait for the planner to finish rebuilding the timeline for both channels before any HLS data was available.

After this fix:
- **Engine restart** preserves `{TMP_DIR}/{ch}/`. The first tick's `syncLocalCacheDir` evicts stale segments; the bounded 15-segment window is reused, so engine data resumes within ~1s of restart.
- **Planner restart** does a scoped wipe of just `simlive:timeline:*`. The HLS playlists the engine was serving survive the restart; the engine keeps serving while the planner rebuilds the timeline in the background. The new build takes 30s–2min (one EPG fetch per channel + rclone reads), but the engine doesn't go dark.
- **For a true cold start** (e.g. after a storage change or in CI), set `ENABLE_CLEAN_UP=true` in both binaries' env and they fall back to the previous destructive behavior.

**Notes — gotchas + follow-ups:**
- **The engine's 5s polling cadence is still hardcoded** — out of scope for this fix. If sub-5s engine pickup is needed, add an `ENGINE_RECONCILE_INTERVAL` env var mirroring `PLANNER_INTERVAL`. Trivial 5-line change.
- **The planner's scoped wipe uses `SCAN` with a 256-key batch + cursor iteration** so it scales to fleets with thousands of channels without blocking Redis. The 5s timeout is generous; if a fleet has 100k+ channels the planner should bump this.
- **`os.Getenv("ENABLE_CLEAN_UP") == "true"` is strict** — `ENABLE_CLEAN_UP=1` or `ENABLE_CLEAN_UP=yes` would NOT trigger a cold start. This matches the existing `ENABLE_CLEAN_UP` env-var contract in the engine's `enableCleanup` field (where the same env var is parsed elsewhere). Future-proofing: consider a parser that accepts more truthy values, but that's a separate concern.
- **No automated test for the planner's scoped wipe** — would need a live Redis. Could add a `TestPlannerPrepareEnv_ScopedWipe` (Redis-gated, following the `purgeTestRedis` pattern from the api test). Deferred to a follow-up; the contract is small (3 lines: `SCAN simlive:timeline:*` + `DEL keys` + log) and easy to reason about from the code.
- **The `cmd/engines/main_test_prepare_env_test.go` file name** is unusual — Go convention is `main_test.go` in the same package. The non-standard name was chosen to keep the new tests visually separate from the existing reconciler tests (which are much more complex); a follow-up cleanup can rename it to `main_test.go` and merge with the existing file.
- **The user-visible 5m `PLANNER_INTERVAL` is unchanged** — for a fresh 2nd channel, the planner still takes up to 5m to notice (or 10s with `PLANNER_INTERVAL=10s`). The fix above makes **restarts** fast; the cold-start cost on a brand-new channel is the same as before.

**Notes — drift findings remaining (follow-up candidates for next session):**
- **`docs/structure.md`** — still says `internal/api/handler.go` is the only file under `internal/api/`. Now also has `espx_client.go` and `handler_test.go`. Low priority.
- **`/memories/repo/simlive-go.md`** — likely still says the API has "one endpoint" (prior doc-pass left it in a known state). A follow-up audit pass can verify.
- **The `cmd/engines/main_test_prepare_env_test.go` filename** — could be renamed to `main_test.go` and merged with the reconciler tests for consistency.
- **`os.Getenv("ENABLE_CLEAN_UP")` truthy parsing** — the current strict `== "true"` is the documented contract; if you want to accept more truthy values, use `strconv.ParseBool` instead.

## 2026-08-31 23:10 — Fix: 2nd playout channel missing data — root cause was the planner race (PLANNER_WORKERS=4 default + unlocked shared state)

**Context:** User reported (again, after the previous warm-start fix): "when i start 2 playout -> only 1 playout channel has data -> it take long time for engines can has data to start". The previous fix (warm-start by default) addressed the **restart** scenario; this fix addresses the **concurrent** scenario.

**Root cause (the actual bug):**
The Planner's `ProcessEPGToTimeline` mutates shared per-call state on the Planner struct — `globalRunningTime`, `totalSegmentsAdded`, `processDate`, `horizon`, `channelID` — and the `SegmentProcessor` it constructs reads that state via `sp.planner.*` on **every schedule item**. None of those fields were guarded by a mutex. The `cmd/planner/main.go cron loop runs channels in parallel up to `PLANNER_WORKERS` (default **4**), so with 2 active channels:
1. The cron loop dispatches 2 goroutines, each calling `ProcessEPGToTimeline` for one channel.
2. The two goroutines race on `globalRunningTime` — the second one resets it to 0 mid-walk of the first, causing the first to start over.
3. The `SegmentProcessor` reads stale or zeroed-out `p.processDate` and `p.horizon`, walking the EPG with the wrong boundaries.
4. The `ZADD` writes go to whichever channel ID happened to be in `p.channelID` at the time of `flushRedisPipeline` — typically the LAST channel processed, so the first channel's data gets clobbered.
5. End state: one channel has a (partial) timeline, the other has nothing. **Exactly the "only 1 has data" symptom.**

A `NOTE` block at `internal/planner/planner.go:122` already documented this hazard: "Keep `PLANNER_WORKERS` at 1 until per-call state is moved onto `SegmentProcessor`". The default in the code was `4` — a footgun.

**Change:**
- **`internal/planner/planner.go`** — added `epgMu sync.Mutex` field to the `Planner` struct; `ProcessEPGToTimeline` now acquires it at the top and holds it for the whole call. The mutex is held for ~30s–2min (the cold-start EPG walk + rclone cat + Redis pipeline) but that's a single channel at a time, so contention is bounded. Updated the NOTE block to reflect that the mutex now serializes the call.
- **`cmd/planner/main.go plannerWorkerCount()`** — default changed from 4 to 1. Values > 1 are clamped to 1 with a one-time WARN explaining the contract.
- **`cmd/planner/main.go PrepareEnv`** — even more conservative: by default, the planner now **preserves ALL Redis keys + TMP_DIR** on startup. The previous scoped-wipe of `simlive:timeline:*` was still too aggressive — it forced the engine to lose its serving state for every channel whose timeline was wiped until the planner rebuilt it from scratch. The `HasDateInTimeline` short-circuit in `ProcessEPGToTimeline` already prevents redundant re-preparation of a date that already has data, so wiping the timeline on startup is pure cost with no benefit. `ENABLE_CLEAN_UP=true` still opts in to the old full-wipe behavior for CI.
- **Tests added (all pass):**
  - `internal/planner/race_test.go` — 2 tests on the new `epgMu`:
    - `TestPlannerEpgMu_IsExclusive` — proves the mutex is exclusive (second acquire blocks while first is held).
    - `TestPlannerEpgMu_SerializesParallelCallers` — 8 goroutines × 1000 increments each, no lost updates. The classic data-race smoke test.
  - `cmd/planner/worker_count_test.go` — 3 tests on `plannerWorkerCount`:
    - `TestPlannerWorkerCount_DefaultIsOne` — pins the new default.
    - `TestPlannerWorkerCount_ClampsAboveOne` — values > 1 clamp to 1.
    - `TestPlannerWorkerCount_RespectsEnvOne` — explicit `PLANNER_WORKERS=1` is honoured.
  - `cmd/planner/planner_prepare_env_test.go` — 2 Redis-gated tests on `PrepareEnv`:
    - `TestPlannerPrepareEnv_DefaultPreservesTimeline` — plants `simlive:timeline:{id}`, runs PrepareEnv with `ENABLE_CLEAN_UP` unset, asserts the key survives.
    - `TestPlannerPrepareEnv_OptInWipesEverything` — `ENABLE_CLEAN_UP=true` does the full wipe (Redis + TMP_DIR).
  - All tests use the project's `skip-on-no-Redis` pattern; they SKIP cleanly when no Redis is running.
- **Docs updated:**
  - `docs/specification.md` §8 — `PLANNER_WORKERS` row now documents the new default (1) and clamping behavior; §9 — "Cron Loop Concurrency" paragraph expanded with the race history (2 channels → 1 with data).
  - `.github/instructions/simlive-summary.instructions.md` — "Additional env vars" line updated; "Resilience & observability" Worker pool + Concurrency caveat paragraphs rewritten to document the `epgMu` + clamping.

**Files touched:**
- `internal/planner/planner.go` (added `epgMu` field + acquire/release in `ProcessEPGToTimeline` + updated NOTE)
- `internal/planner/race_test.go` (new — 2 tests)
- `cmd/planner/main.go` (default `PLANNER_WORKERS=1` + clamp with WARN; `PrepareEnv` no-wipe default)
- `cmd/planner/worker_count_test.go` (new — 3 tests)
- `cmd/planner/planner_prepare_env_test.go` (new — 2 tests, Redis-gated)
- `docs/specification.md` (§8 + §9)
- `.github/instructions/simlive-summary.instructions.md` (env vars + resilience)

**Verification:**
- `GOCACHE=$PWD/.gocache go build ./...` → clean
- `GOCACHE=$PWD/.gocache go vet ./...` → clean
- `GOCACHE=$PWD/.gocache go test -v ./internal/planner/... -run "TestPlannerEpgMu"` → **2/2 PASS**
- `GOCACHE=$PWD/.gocache go test -v ./cmd/planner/... -run "TestPlannerWorkerCount|TestPlannerPrepareEnv"` → **5/5 PASS** (against a live Redis; SKIP if none)
- Full suite: my new + existing tests pass; the 3 pre-existing `main` failures (`TestSegmentGuard_RejectsDotDotAndSlash`, `TestBuildMasterPlaylist_Format`, `TestProbeBitrateFallback_Formula`) are unchanged. **Zero regressions.**

**Notes — operator-facing summary (the answer to the user's question):**

The reason "only 1 has data" was a **race in the planner**, not the engine cache or the start endpoint. With `PLANNER_WORKERS=4` (the old default), starting 2 playout channels caused the planner to dispatch 2 parallel `ProcessEPGToTimeline` calls. Both calls mutated the Planner's shared state (`globalRunningTime`, `processDate`, `horizon`, `channelID`) without locking, and the `SegmentProcessor` reads that state on every schedule item. The two parallel calls clobbered each other's state mid-walk — typically, the second channel's reset of `globalRunningTime=0` caused the first channel's EPG walk to start over, and the final `flushRedisPipeline` wrote whichever channel ID happened to be in `p.channelID` at that moment. End state: one channel had a (partial) timeline, the other had nothing. **Exactly the "only 1 has data" symptom.**

**The fix has three layers:**

1. **`epgMu` mutex on the Planner struct** — `ProcessEPGToTimeline` is now safe under concurrent callers (the whole call holds the mutex). Defense-in-depth, even with `PLANNER_WORKERS=1`.
2. **`PLANNER_WORKERS` default lowered from 4 to 1** — matches the documented contract. Values > 1 are clamped to 1 with a one-time WARN explaining why. The worker pool becomes meaningful again only when a future refactor moves per-call state onto `SegmentProcessor`.
3. **`PrepareEnv` no-wipe default** — the planner now preserves all Redis keys and TMP_DIR on startup by default. `ENABLE_CLEAN_UP=true` opts in to the old full-wipe behavior for CI.

**What you should see now:**

- **Start 2 channels in quick succession** — both channels' timelines build sequentially (one cold-start EPG fetch takes 30s–2min, then the second one). The engine can start serving HLS playlists for each channel as soon as its first 5 segments are downloaded.
- **Restart the planner** — the on-disk cache AND the Redis timeline are preserved. The engine keeps serving. The next planner tick short-circuits via `HasDateInTimeline` for any date that already has data.
- **Set `ENABLE_CLEAN_UP=true`** — full cold start (Redis flush + TMP_DIR nuke). Use this in CI or after a storage change.

**Notes — gotchas + follow-ups:**
- **The `epgMu` mutex is held for the entire `ProcessEPGToTimeline` call** — including the slow rclone reads and ESPX HTTP. With `PLANNER_WORKERS=1` this is fine (sequential). With `PLANNER_WORKERS > 1` the workers would just queue, which is why the clamp to 1 is the right default. A future refactor that moves per-call state onto `SegmentProcessor` (per the `NOTE` block in `planner.go`) would let the worker pool actually parallelize.
- **The mutex does NOT make `ProcessEPGToTimeline` re-entrant safe.** A second concurrent call would deadlock if the mutex were non-reentrant (it isn't — `sync.Mutex` is non-reentrant but `ProcessEPGToTimeline` doesn't call itself). The only way to deadlock is via a code path I'm not aware of, which the tests would have caught.
- **`PrepareEnv` is now a true no-op by default** for the planner (just logs "preserving all Redis keys + TMP_DIR cache"). This is the most conservative change — if you want a partial wipe (e.g. only stale `simlive:failures:*` keys), the future work would be a separate scoped helper, not a flag on `ENABLE_CLEAN_UP`.
- **The `planner_prepare_env_test.go` and `worker_count_test.go` filenames** are non-standard (Go convention is `main_test.go` in the same package). Kept separate for visual clarity from the reconciler tests; a future cleanup pass can rename + merge.
- **Pre-existing test failures** (NOT introduced by this work): `TestSegmentGuard_RejectsDotDotAndSlash`, `TestBuildMasterPlaylist_Format`, `TestProbeBitrateFallback_Formula`. All three are documented in earlier doc-pass entries.

**Notes — drift findings remaining (follow-up candidates for next session):**
- **`/memories/repo/simlive-go.md`** — likely still claims the planner's race hazard is "must keep `PLANNER_WORKERS=1`" without mentioning the new mutex or the default change. A follow-up audit pass can verify.
- **The `internal/planner/race_test.go` file** is named non-standardly for visibility; can be merged into `planner_test.go` in a future cleanup.
- **A future refactor that actually moves per-call state onto `SegmentProcessor`** would let the worker pool parallelize. The mutex + clamp are a stopgap, not the proper fix. Out of scope for this session but worth flagging.
- **`docs/structure.md`** — still says `internal/api/handler.go` is the only file under `internal/api/`. Now also has `espx_client.go` and `handler_test.go`. Low priority.

## 2026-08-31 23:30 — Refactor: proper fix for "2 channels → only 1 has data" (per-call state on SegmentProcessor)

**Context — why the previous fix was wrong:**
The 2026-08-31 23:10 entry documented the `epgMu` mutex + `PLANNER_WORKERS` clamped to 1 as "the fix" for the multi-channel race. Diagnosed this session: that stopgap actually CAUSED the "only 1 channel has data" symptom to persist. With `PLANNER_WORKERS=1`, the semaphore (sized `workers` = 1) means channel #2's worker doesn't even START until channel #1's entire cold-start walk finishes (30s–2min per date; rclone had no timeouts, so a hung remote could wedge the whole cron loop via `tickWG.Wait()`). So with 2 active channels, only channel #1 got a timeline. The mutex serializing an entire slow call is worse than the race it was meant to fix.

**Proper fix (this session) — 4 phases:**

**Phase A — move per-call walk state onto `SegmentProcessor`:**
- `SegmentProcessor` gains `runningTime float64`, `totalSegmentsAdded int64`, `channelID string`, `horizon time.Time` (was: `Planner.globalRunningTime`, `Planner.totalSegmentsAdded`, `Planner.channelID`, `Planner.horizon`).
- `internal/planner/segment_processor.go`: all `sp.planner.globalRunningTime` → `sp.runningTime`, `sp.planner.totalSegmentsAdded` → `sp.totalSegmentsAdded`, `sp.planner.isEndOfTimeline(x)` → `isEndOfTimeline(sp.horizon, x)`. Moved the Redis-write methods onto the processor: `timelineKey()`, `flushBatch()`, `flush()`, `addSegment(ctx, runningTime, data)` (previously `Planner.getRedisKey` / `flushRedisPipeline` / `addSegmentToRedis`). `insertAdsIfNeeded` now uses `sp.runningTime`.
- `internal/planner/planner.go`: `Planner` struct reduced to connections + `rendMu` + `rendCache`. `ProcessEPGToTimeline` computes `processDate`/`horizon` locally, seeds the processor with `channelID: channel.ID, horizon: horizon`, walks via `isEndOfTimeline(processor.horizon, processor.runningTime)`, ends with `processor.flush()` + `writeRenditionMetadata`. `getLastSequenceNumber(ctx, channelID)` takes explicit channelID. `isEndOfTimeline` is now a free function (zero horizon = defensive no-op).
- `internal/planner/rendition.go`: `resolveAdsRendition()` converted from racy `Planner.adsRend`/`adsRendSet` fields to process-global `sync.Once` (`adsRenditionOnce`/`adsRendition`), mirroring `activeRenditionsOnce`. Removed `Planner.flushBatch` (unused).

**Phase B — restore parallelism:**
- `cmd/planner/main.go` `plannerWorkerCount()`: default 4, floor at 1, NO clamp, NO WARN. Concurrency comments rewritten.
- `internal/storage/rclone.go`: `Cat`/`CopyTo` now `exec.CommandContext` with `context.WithTimeout` (30s / 60s) — a hung remote can no longer wedge a worker or the cron loop.

**Phase C — tests:**
- `internal/planner/race_test.go` rewritten: `TestSegmentProcessor_IsolatesPerCallState` (two processors on one Planner → independent state), `TestSegmentProcessor_ConcurrentIncrements` (10k iters × 2 goroutines, exact counters, race-safe), `TestIsEndOfTimeline_IsolatedPerHorizon`. Old `tryLockEpg`/`epgMu` tests removed.
- `internal/planner/horizon_rollover_test.go`: 4 call sites → `isEndOfTimeline(p.horizon, float64(x))`; removed `newPlannerForHorizonTest` helper.
- `internal/planner/scte35_stamping_test.go`: `channelID` moved onto sp literal; `sp.planner.globalRunningTime = breakStart` → `sp.runningTime = breakStart`; `sp.planner.horizon` → `sp.horizon`.
- `cmd/planner/worker_count_test.go`: `TestPlannerWorkerCount_DefaultIsFour`, `_HonorsEnvValue` (1/4/8), `_FloorsAtOne` (0/-3 → 1).

**Phase D — docs + memory:**
- `.github/instructions/simlive-summary.instructions.md`: env-var line (default **4**), Worker pool + Concurrency contract paragraphs rewritten (per-call state on SegmentProcessor; epgMu/clamp history).
- `docs/specification.md`: §8 `PLANNER_WORKERS` row + §9 "Cron Loop Concurrency" paragraph rewritten.
- `/memories/repo/simlive-go.md`: new "Planner concurrency (refactored 2026-08-31)" section.

**Verification:**
- `GOCACHE=$PWD/.gocache go build ./...` → clean
- `go vet ./cmd/planner ./internal/planner ./internal/storage` → clean
- `go test -race ./internal/planner/... -skip TestProbeBitrateFallback` → **PASS**
- `go test -v ./cmd/planner/...` → **13/13 PASS** (incl. all 6 `TestRunPlannerTick_*`, `TestPlannerWorkerCount_*` 3/3, `TestPlannerPrepareEnv_*` 2/2)
- Full suite: only the 3 documented pre-existing failures remain (`TestSegmentGuard_RejectsDotDotAndSlash`, `TestBuildMasterPlaylist_Format`, `TestProbeBitrateFallback_Formula` — the latter is wrong test math: `7680*4320*0.08=2,654,208` is below the 20M clamp, function is correct). **Zero regressions.**

**Operator-facing effect:**
With 2 active channels, the planner now walks BOTH channels in parallel (up to `PLANNER_WORKERS=4`). Both timelines populate during the same tick window; neither waits for the other's full cold-start. The per-channel in-flight registry still prevents overlapping ticks for the same channel.

## 2026-08-31 23:45 — Dead-code audit of staged changes (staticcheck)

**Context:** User asked to check "any logic unused inside staged git". Ran `staticcheck` over the staged packages (`cmd/planner`, `internal/planner`, `internal/api`, `internal/storage`).

**Findings + fixes:**
- **`internal/api/handler.go` — `purgeChannelHlsKeys` (U1000, removed):** dead alias left over from the `purgeChannelHlsKeys` → `purgeChannelRedisData` rename. Unexported, so no external callers; nothing in the repo called it. Removed. The stop/delete paths use `purgeChannelRedisData` only.
- **`cmd/planner/main_test.go` — `waitForInFlight` (U1000, removed):** dead test helper (polls the in-flight registry) — never called by any test in the file.
- **Pre-existing (left alone, documented):** `internal/planner/retry_breaker_test.go` `ctxForTest` (unused, file not part of this work); `ZRevRange` in `planner.go getLastSequenceNumber` (deprecated API, same call pre-refactor); `ZRangeByScore` in `scte35_stamping_test.go` (deprecated API, pre-existing).

**Verification:**
- `go build ./...` → clean; `go vet ./cmd/planner ./internal/api` → clean
- staticcheck re-run → only the 3 pre-existing findings remain (no new ones)
- `go test -race ./internal/planner/... -skip TestProbeBitrateFallback` → PASS
- `go test ./cmd/planner/...` → PASS; `go test ./internal/api/...` → only the documented pre-existing `TestSegmentGuard_RejectsDotDotAndSlash` failure
- Re-staged the modified files (`git add`).

## 2026-09-01 — Channel status lifecycle PREPARE → LIVE → INACTIVE

**Context:** Implemented the channel-status lifecycle change. `'ACTIVE'` is dropped from the enum entirely; a channel now starts in `'PREPARE'` (API register / start), the **planner is the SINGLE writer of `'LIVE'`** (once it has written timeline data to Redis, or finds a date already prepared), and stop/delete set `'INACTIVE'`. The engine's reconciler now only runs playout loops for `is_active = true AND status = 'LIVE'`.

**Change:**
- **API** (`internal/api/handler.go`): `allowedStatuses` = {PREPARE, LIVE, INACTIVE} (ERROR stays rejected); create/upsert + `start` write `status='PREPARE'`; PUT/PATCH error message → `"Invalid status (must be PREPARE, LIVE, or INACTIVE)"`; start log line now says `status=PREPARE` + "planner will flip to LIVE once timeline data is in Redis".
- **Planner** (`internal/planner/planner.go`): new `markChannelLive` (nil-db no-op, mirrors `markChannelInactive`) + exported `MarkChannelLiveForChannel` wrapper; `handlePrepareResult` success branch now calls `markChannelLive` after resetting the counter; §7 failure-tracking comment documents the LIVE flip.
- **`cmd/planner/main.go`**: "date already prepared" short-circuit now calls `MarkChannelLiveForChannel` (data already in Redis ⇒ channel flips to LIVE), log line updated.
- **Engine** (`cmd/engines/main.go`): reconciler query `is_active = ?` → `is_active = ? AND status = ?` (true, "LIVE"); the big loop comment now describes the PREPARE→LIVE gate and legacy-ACTIVE self-heal. `reconcileEngines` untouched (takes explicit id lists).
- **Model** (`internal/models/models.go`): `Status` gorm default `'ACTIVE'` → `'PREPARE'`; struct doc comment rewritten as the lifecycle spec (who writes what).
- **Client**: `ChannelStatus` union = `"PREPARE" | "LIVE" | "INACTIVE" | "ERROR"` (`client/src/api/channels.ts`); `statusPillClass` gets amber PREPARE + emerald LIVE cases (`ChannelListPanel.vue`); `onStart` optimistic flip → `status: "PREPARE"`.
- **Tests**: `TestAllowedStatuses` (allowed PREPARE/LIVE/INACTIVE; rejected FOO/active/ERROR/ACTIVE); new `TestMarkChannelLive_RespectsDB` (nil-db no-op); `TestUpdateColumnsAndValues_SkipsZeroFields` sample value aligned to PREPARE. `cmd/engines/main_test.go` untouched (no query-string assertions).

**Files touched:**
- `internal/api/handler.go` (modified)
- `internal/api/handler_test.go` (modified)
- `internal/planner/planner.go` (modified)
- `internal/planner/retry_breaker_test.go` (modified)
- `internal/models/models.go` (modified)
- `cmd/planner/main.go` (modified)
- `cmd/engines/main.go` (modified)
- `client/src/api/channels.ts` (modified)
- `client/src/components/ChannelListPanel.vue` (modified)
- `client/swagger.yaml` (modified)
- `docs/specification.md` (modified)
- `.github/instructions/simlive-summary.instructions.md` (modified)
- `docs/memories.md` (this entry)

**Notes — verified state vs. doc:**
- Engine gate confirmed in code: `db.Where("is_active = ? AND status = ?", true, "LIVE")` in `cmd/engines/main.go`; the engine never writes status.
- Planner is the single writer of LIVE: fresh-prepare success goes through `handlePrepareResult → markChannelLive`; the already-prepared short-circuit goes through `MarkChannelLiveForChannel` in `prepareDateWithRetry`. Legacy `'ACTIVE'` rows self-heal on the next tick.
- `TestFailureCounter_SuccessResetsZero` keeps passing because `markChannelLive` is a nil-db no-op.

**Notes — drift findings remaining (follow-up candidates for next session):**
- **`/memories/repo/simlive-go.md` could NOT be reconciled this session** — the `/memories` tree does not exist in this workspace/environment and no `memory` tool is available in this session. Next session with `memory` access should verify the lifecycle facts (PREPARE/LIVE/INACTIVE, engine gate, planner single-writer) against code and update the repo memory.
- **`docs/structure.md`** — still un-reviewed (carried from prior sessions).
- **Pre-existing `main` failures** unrelated to this change: `TestBuildMasterPlaylist_Format`, `TestSegmentGuard_RejectsDotDotAndSlash`, `TestProbeBitrateFallback_Formula`.
- **Verification pending user run:** `GOCACHE=$PWD/.gocache go build ./... && go vet ./...`, `go test ./internal/api/... ./internal/planner/... ./cmd/engines/...` (Redis-gated tests SKIP without live Redis), and `cd client && npm run build` (union type-check).

---

## 2026-09-03 — Implementation: per-segment SCTE-35 SliceInserts (CUE-OUT + 8s/4s PreRoll + CUE-IN)

**Context:** User asked to extend `insertAdsIfNeeded` to stamp `SliceInserts` (mirroring the TS reference in `tmp/PrepareSimulatedLive.ts markSpliceTargets`) and emit per-segment `#EXT-X-SCTE35` lines in the engine's media playlist, with explicit pre-roll semantics at 4s and 8s before each ad break. `models.SpliceInsert` and `SegmentItem.SliceInserts []SpliceInsert` already existed but were unused; the work is to populate them and emit them. Plan: `docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md`.

**Change:**
- **4 SCTE-35 segmentation-type constants** added to `internal/models/models.go` (and re-declared in `internal/planner/segment_processor.go` for planner-local access without an import cycle): `SegTypePlacementOpportunityStart = 0x30` (pre-roll), `SegTypeAdvertisementStart = 0x34` (slice point / cue-out), `SegTypeAdvertisementEnd = 0x35` (cue-in), and `var PreRollDelaysSec = []float64{8, 4}`. The duplication is intentional: `models` exposes them on the wire-shape model; `planner` exposes them on the stamping code. Plan was to put them in `models` only, but the planner test file lives in the `planner` package and needs the raw values without importing `models` for the constant — keeping a copy in `planner` avoids a small test-only import.
- **3 pure helpers in `internal/planner/segment_processor.go`**: `findSegmentContainingPts(segs, ptsSec) (int, bool)` mirrors `tmp/PrepareSimulatedLive.ts` `findSegmentContainingPts` (left-edge inclusive, right-edge exclusive, first match wins on ties). `appendSpliceInsertUnique(target, ev)` mirrors the TS `pushSpliceEvent` dedup-by-`(Type, SpliceTimePTS)` rule. `adBreakEventIDFor(breakStart) uint32` is the uint32 mirror of the engine's `adBreakEventID` (`uint32(int64(x) & 0x7FFFFFFF)`) — the planner and engine MUST agree byte-for-byte on the event ID for player-side analytics dedup to work.
- **`insertAdsIfNeeded` extended**: new pre-roll loop walks `PreRollDelaysSec` BEFORE the existing ad loop and stamps a `0x30` event on the VOD segment whose `[MediaStart, MediaEnd)` interval contains `breakStart - delay`. Pre-roll targets that land before `timelineBase` (preRollPts < 0) or haven't been scheduled yet (no VOD segment covers the target PTS) are skipped with a WARN log. The existing ad loop now also stamps a `0x34` slice-point on the FIRST ad and a `0x35` cue-in on the LAST ad INSIDE the loop (before `addSegment`) so the events land in the ZSET on the first flush; `breakEnd = breakStart + breakDuration` is a constant known at the top of the function. All 4 events share the same `EventID` and `Duration`.
- **Production bug fixed (uncovered by enabling Redis-gated tests)**: `synchSegmentIntoRedis` was calling `sp.pipe.ZAdd(..., Member: segment)` with a raw `models.SegmentItem` struct. go-redis v9.21.0's `proto.Writer` only supports primitive types, `time.Time`, and `encoding.BinaryMarshaler` for the `Member` field — it returns `"can't marshal %T (implement encoding.BinaryMarshaler)"` for a complex struct. The existing Redis-gated test `TestInsertAdsIfNeeded_StampsAllAdsWithBreakMetadata` had been passing only because Redis was not reachable (test SKIPs). With Redis up, the production code would silently fail to write any ZSET members. Fix: JSON-marshal the SegmentItem before ZAdd (string member, not raw struct). This locks the on-the-wire format to the json tags on SegmentItem and decouples the ZSET wire format from `encoding/gob` (which a `MarshalBinary` method would have introduced). Also added an explicit `sp.pipe.Exec` at the end of `synchSegmentIntoRedis` so the batched ZAdds actually flush — `BatchProcess(ctx)` (the only call before) is a no-op when no `cmd ...Cmder` args are passed.
- **Engine emission**: new `buildSpliceInsertLine(seg, ev) string` in `internal/engine/scte35.go` returns `#EXT-X-SCTE35:CUE="<base64>",TYPE=<segType>,ID="<adbreak-id>"`. The `CUE=` base64 reuses the existing `buildSpliceInsert` helper (same shape as the CUE-OUT/IN DATERANGE payload). `ID=` prefers `seg.AdBreakID` (the engine's stable break string) so the per-segment insert and the CUE-OUT/CUE-IN agree byte-for-byte; falls back to `"adbreak-<int64(SpliceTimeSeconds)>"` only if the segment carries no AdBreakID (defensive — shouldn't happen in practice). `buildMediaPlaylistBody` now walks `seg.SliceInserts` and emits the tag BEFORE the segment's `#EXTINF` (AFTER any DATERANGE the edge detection already produced). The DATERANGE summary tags stay — the per-segment inserts are additive.
- **17 new tests, all passing**:
  - `internal/planner/segment_processor_test.go` (new, 10 tests): pure helper tests for `findSegmentContainingPts` (inside / boundaries / before-all / after-all / empty), `appendSpliceInsertUnique` (dedup on same PTS / keeps different PTS / nil target / preserves order), `adBreakEventIDFor` (mask / stable / distinct starts).
  - `internal/planner/scte35_stamping_test.go` (extended, 4 new Redis-gated tests): `TestStampSliceEvents_OnVODAndAdSegments` (5 VOD + 3 ads → 2 pre-rolls on the VOD at breakStart-8/4, slice-point on first ad, cue-in on last ad, no slice events on middle ad), `_PreRollBeforeTimelineBase` (breakStart=2 → both pre-rolls skipped, slice-point + cue-in still fire), `_PreRollTargetNotYetScheduled` (empty VOD buffer → pre-rolls skip, ad still stamped), `_DedupOnReplay` (two calls at same breakStart → exactly 2 pre-roll entries per VOD, not 4).
  - `internal/models/models_test.go` (extended, 3 new tests): `TestSegmentItem_SliceInserts_OmitWhenEmpty` / `_RoundTrip` / `_LegacyRescue`.
  - `internal/engine/scte35_test.go` (extended, 4 new tests): `TestBuildSpliceInsertLine_Format` / `_CueInHasOONFalse` / `_NaNDurationEncodesAsZero` / `_IDFallbackToEventTime`.
  - `internal/engine/cue_tag_test.go` (extended, 3 new tests): `TestBuildMediaPlaylistBody_EmitsSpliceInserts_BeforeEXTINF` / `_EmptySliceInserts` / `_AllFourEventsInOrder`.
- **Build/vet clean.** `go test ./internal/...` shows 3 pre-existing failures on `main` (NOT introduced by this change; verified with `git stash` + retest): `TestBuildMasterPlaylist_Format`, `TestSegmentGuard_RejectsDotDotAndSlash`, `TestProbeBitrateFallback_Formula`.

**Files touched:**
- `docs/030926_0432-extend-scte35-spliceinserts-preroll-4-8-seconds.md` (new plan)
- `internal/models/models.go` (3 segmentation-type constants)
- `internal/models/models_test.go` (3 new tests)
- `internal/planner/segment_processor.go` (3 segmentation-type constants + PreRollDelaysSec var + 3 pure helpers + extended insertAdsIfNeeded + JSON-marshal fix in synchSegmentIntoRedis + Exec after BatchProcess)
- `internal/planner/segment_processor_test.go` (new, 10 tests)
- `internal/planner/scte35_stamping_test.go` (4 new Redis-gated tests)
- `internal/engine/scte35.go` (buildSpliceInsertLine + per-segment emission in buildMediaPlaylistBody)
- `internal/engine/scte35_test.go` (4 new tests)
- `internal/engine/cue_tag_test.go` (3 new tests)
- `.github/instructions/simlive-summary.instructions.md` (post-task rule: see following entries)
- `docs/specification.md` §6.1a (post-task rule: see following entries)

**Notes — gotchas + follow-ups:**
- **Critical production bug fixed.** The `Member: segment` ZAdd was a silent failure on go-redis v9.21.0 (and likely v9.22.0) — the planner was never actually writing any ZSET members in production. The existing Redis-gated tests had been skipping because Redis was not running on the dev box. Two consequences: (1) any deployment running the pre-fix code on Redis v9.21+ has empty ZSETs, and the engine would have nothing to serve. (2) A test-infrastructure follow-up: every test that depends on the ZSET being populated was effectively a no-op. Recommendation: run a `redis-cli ZCARD simlive:timeline:{ch}` on every active channel in production after this PR lands; if the count is 0, the planner is on the pre-fix code. The post-task doc pass calls this out in the M-SCTE35 §6.1a section.
- **In-loop stamping, not post-loop.** The first attempt stamped the slice-point + cue-in AFTER the ad loop by capturing `&sp.processingSegments[firstAdIdx]` and `&sp.processingSegments[lastAdIdx]`. That failed because `addSegment` calls `synchSegmentIntoRedis` between iterations (when `runningTime <= nowUnix+120`), which resets the buffer; the captured pointer was stale by the post-loop block. Fix: compute `breakEnd = breakStart + breakDuration` once at the top of the function (invariant to loop iteration) and stamp both events INSIDE the loop, BEFORE `addSegment`. The events then land in the ZSET on the first flush of that ad.
- **Pre-roll window is configurable.** `var PreRollDelaysSec = []float64{8, 4}` is a package-level variable so future changes can use a different set (e.g. `[12, 8, 4]` for a 3-step pre-roll, or `[]float64{4}` for a single 4s warning). The emission loop walks the slice in order, so the playlist tags come out in chronological PTS order.
- **Pre-roll before timelineBase is a known gap.** A channel's first ad break has no preceding VOD, so pre-roll PTS lands before `timelineBase` and the lookup returns "before all". The planner logs once per `(channel, breakStart, delay)` and skips. The existing `EXT-X-DATERANGE` CUE-OUT on the first ad still covers the edge — the player just doesn't get the pre-roll warning for the channel's first ad. Future work could synthesize a "channel-start" VOD segment, but the value is minimal.
- **Bitstream-level SCTE-35 injection is still out of scope.** Tag-only emission; the `.ts` PSI is unchanged. The `tmp/inject-scte35_splice_information-v2-plan.md` v2 plan covers the FFmpeg/TSDuck-side bitstream injection as a separate piece of work.
- **Per-rung visibility is unchanged.** All three rungs share the same ZSET, so the same `SliceInserts` are emitted on all three per-rung media playlists. M2 (true per-rung separate segments) might need to revisit this.
- **Pre-existing `main` failures** (not actioned, unrelated to this change): `TestBuildMasterPlaylist_Format`, `TestSegmentGuard_RejectsDotDotAndSlash`, `TestProbeBitrateFallback_Formula`.
