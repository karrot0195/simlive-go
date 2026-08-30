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
  - `/memories/repo/simlive-go.md` references `docs/290826-chromium-playback-handoff.md` and `docs/290826-testing-guide.md` (neither file exists) — replaced with pointers to the actually-existing plan (`docs/300826-multi-rendition-abr-ladder.md`) and to this log.
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
  - Plan: `docs/300826-multi-rendition-abr-ladder.md` (headline: "M1.5 SHIFT — 1 shared segment cache + 3 advertised rungs, M2 deferred").
- **PTS rewrite is single-anchor**, NOT two-anchor:
  - `internal/engine/engine.go` `rewritePtsOffset` body: `target := seg.MediaStart; if seg.PtsOffset != nil { target = *seg.PtsOffset }`, then `offset = target - firstDTS` (or `target - seg.PtsStart/90000` on probe failure). No continuity lift against the previous segment's last DTS, no serialized `rewriteChain`, no `-itsoffset`. Repo memory line 14 was corrected.
- **`SIMLIVE_RENDITIONS` behaviour:** M1 only allows 1 rung. If set to `"360p,720p"`, logs WARN once per process (sync.Once-guarded) and uses `["360p"]`. Empty / unset → uses the `SIMLIVE_QUALITY` tier label (fallback `360p`). M1.5 ignores multi-rung values because the engine serves ONE shared segment cache.
- **`PLANNER_WORKERS` caveat unchanged:** `Planner` shared state is unlocked; keep `PLANNER_WORKERS=1` until per-call state moves onto `SegmentProcessor`. The implementer agent gotchas + this log + the instruction file all carry this caveat.

**Notes — drift findings remaining (follow-up candidates for next session):**
- The `simplive-implementer.agent.md` still has a "Per-rung directories must be `MkdirAll`'d" gotcha that is moot under the current M1.5 shared-cache model — kept verbatim because it documents the M2 fallback if anyone lifts the shared cache to per-rung subdirs.
- `docs/structure.md` not reviewed this session; was not in the user's request scope. Out of scope but worth a future audit.
- The agent gotcha "Gin radix-tree wildcard conflicts" is still valid and was the cause of the bugfix mentioned in `docs/300826-multi-rendition-abr-ladder.md` §0; no change needed.
- No M2 follow-up items have landed yet. The plan §0 marks verification as "Pending user run of `GOCACHE=$PWD/.gocache go test ./internal/...` and `make start-all` smoke test" — recommend doing that as the first item in the next implementation session.