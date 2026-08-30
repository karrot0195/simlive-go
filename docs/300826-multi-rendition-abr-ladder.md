# Multi-Rendition ABR Ladder — Detach + Map Per-Rendition Pipeline

**Plan version:** 0.2
**Date:** 2026-08-30
**Author:** (agent, via simlive-plan skill)
**Status:** M1.5 implemented; M2 deferred

> **M1.5 SHIFT (2026-08-30):** Original M1 said "1 active rung, per-rung
> subdirs". After first integration the user re-scoped:
> **one physical segment cache + 3 advertised rungs**. The engine downloads
> ONE set of `seg_N.ts` files (from the active rung's VOD dir) and serves
> them as 3 rung playlists (`playlist_360p.m3u8`, `playlist_720p.m3u8`,
> `playlist_1080p.m3u8`) + 1 master (`playlist.m3u8`). This is functionally
> a "fake ABR" — only one quality is physically streamed, but the player
> can exercise variant switching against 3 advertised `STREAM-INF` lines.
> All rungs share the same `{TMP_DIR}/{ch}/seg_N.ts` files. M2 (true
> per-rung separate segments) is now an explicit follow-up, not the
> default. The sections below retain the M1 framing where it doesn't
> contradict M1.5; the headline differences are:
>
> | M1 (original) | M1.5 (shipped) |
> |---|---|
> | `{ch}/{rend}/seg_N.ts` per rung | `{ch}/seg_N.ts` shared |
> | `{ch}/{rend}/playlist.m3u8` | `{ch}/playlist_<rend>.m3u8` |
> | Master has 1 `STREAM-INF` (M1) | Master has 3 `STREAM-INF` |
> | `simlive:renditions:{ch}` = 1 entry | `simlive:renditions:{ch}` = 3 entries |
> | API: `/{ch}/{rend}/playlist.m3u8` | API: `/{ch}/playlist_<rend>.m3u8` |
>
> **Scope (revised):** M1.5 = full master-playlist shape + 3 advertised
> rungs + shared segment cache + `playlist_<rend>.m3u8` API shape. M2 =
> actually per-rung separate segment files (drops the shared cache) +
> per-rung subdir layout. M2 is a follow-up; the M1.5 code intentionally
> keeps the data model and code shape compatible with M2 (no rework
> needed beyond changing the segment cache keying).
>
> **Note on "320p":** the storage ladder and `qualityTierMap`
> (`internal/planner/planner.go#L82-L87`) are 360p (`L_0`), 720p (`L_1`),
> 1080p (`L_2`). There is no 320p tier. This plan standardizes on
> **360p / 720p / 1080p**; if 320p is a real requirement (a new `L_*` dir on
> storage), say so — it is a planner-only table addition, not an architecture
> change.

---

## 0. Progress

Track the work; tick as you go.

- [x] Read pre-flight context (`docs/specification.md`, summary instructions,
      previous plans `270826-…`, `290826-…`, target packages).
- [x] Drafted §1–§7 below.
- [x] Reviewed by user (critical fixes applied: stale line refs,
      `ladderCache` coexists with `rendCache`, `writeRenditionMetadata`
      placement, legacy-timeline flush on first post-upgrade planner tick;
      substantive: master refresh on empty window, per-rung `.pts` filter,
      `probeBitrate` primary path, `playlist`-as-rendition path guard,
      `syncLocalCacheDir` per-rung recursion decision).
- [x] Unit-test plan folded into new §6.0 (`planner/rendition_test.go`,
      `models/models_test.go`, `engine/master_playlist_test.go`,
      `api/handler_test.go`).
- [x] M1.5 implementation merged (post user re-scope: shared segment cache
      + 3 advertised rungs + `playlist_<rend>.m3u8` API shape; M2 deferred).
      Files touched: `internal/models/models.go`,
      `internal/planner/{planner,segment_processor,rendition}.go`,
      `internal/engine/{engine,rendition}.go`, `cmd/{api,planner}/main.go`,
      `docs/specification.md`, `.github/instructions/simlive-summary.instructions.md`,
      `.github/agents/simlive-implementer.agent.md` (gotchas section).
- [x] Bugfix pass: removed Gin radix-tree wildcard conflict (legacy 2-param
      `/live/{ch}/{segment}` removed; routes reshaped to `playlist_<rend>`),
      added per-rung `MkdirAll` for downloads (now moot under shared cache,
      but kept in `prepareSegment` for race safety), hardened
      `prepareSegment` dest-verify after `rclone copyto`.
- [ ] Verification (§6) passed end-to-end (unit tests + planner/engine probes).
      Pending user run of `GOCACHE=$PWD/.gocache go test ./internal/...`
      and `make start-all` smoke test.

---

## 1. Problem Statement

Today the whole pipeline is **single-rendition**:

- The planner resolves **one** (VOD, ads) pair per stream
  (`resolveRenditions`, `internal/planner/planner.go#L315`), writes members
  with no rendition dimension into `simlive:timeline:{ch}`, and the engine
  serves **one** media `playlist.m3u8` plus flat `seg_N.ts` files in
  `{TMP_DIR}/{channelID}/` (`internal/engine/engine.go` `StartPlayoutLoop`).
- Players therefore always get the same quality. There is no master playlist,
  no variant switching, and no way to offer 360p/720p/1080p to the same
  channel.

Three things are missing to reach an ABR ladder — and all three are what
"detach and map correct" means:

1. **No rendition dimension in the data model.** `SegmentItem`
   (`internal/models/models.go#L22-L44`) has no rendition field, so one
   timeline cannot carry several variants of the same slot.
2. **Engine state is not detached per rendition.** `seg_{seq}.ts`, the
   `channelState`, the `inFlight` dedupe, `syncLocalCacheDir`, and the
   playlist writer all assume one flat directory. Two renditions would
   collide on `seg_N.ts` filenames and overwrite each other's playlists.
3. **No correct URI mapping.** The API (`cmd/api/main.go#L58-L99`) only knows
   `/live/{ch}/playlist.m3u8` (served from Redis `simlive:hls:{ch}`) and
   `/live/{ch}/seg_N.ts` (flat dir). A master playlist referencing
   `360p/playlist.m3u8` would 404.

**"For now just support 1 rendition"** — M1 lands the detach/map architecture
while keeping a single active rendition (exactly today's output quality, but
served through the new master-playlist shape). M2 then flips the loop to 3
rungs with no redesign.

---

## 2. Solution Overview

**M1 = detach + map, one rung active.**

1. **Model.** `SegmentItem` gains `Rendition string` (`json:"rendition,
   omitempty"`). Legacy members (no field) default to `360p` in the engine.
2. **Planner tags + publishes metadata.** Every member (VOD *and* ads, see
   `processSegment` `internal/planner/segment_processor.go#L140` and
   `insertAdsIfNeeded` `#L175`) carries the active tier label. A new per-channel
   key `simlive:renditions:{ch}` stores the resolved ladder as JSON
   (`rendition`, `vod_dir`, `ads_dir`, `width`, `height`, `bandwidth`) so the
   engine builds the master playlist **without probing at runtime**. Resolution
   and (new) bitrate are probed once by the planner — reusing the existing
   `probeRenditions` machinery.
3. **Engine detaches per rendition.** Each active rung gets its own subdir
   `{TMP_DIR}/{ch}/{rend}/` with its own `seg_{seq}.ts` (+`.pts`), its own
   media `playlist.m3u8` (Redis `simlive:hls:{ch}:{rend}`), its own
   `channelState`, `inFlight` keys (full dest path already separates them) and
   cleanup pass. A **master playlist** at the channel root (`playlist.m3u8`,
   Redis `simlive:hls:{ch}`) maps each variant with
   `#EXT-X-STREAM-INF:BANDWIDTH=…,RESOLUTION=WxH` → `{rend}/playlist.m3u8`.
4. **API maps the 3-URI shape.** `playlist.m3u8` (master, unchanged route),
   new `/{rend}/playlist.m3u8` and `/{rend}/{segment}` routes with the same
   `..`/`/` guards as today.

**Invariants preserved (see `.github/skills/simlive-plan/references/simlive-conventions.md`):**

- **Per-rung VOD↔ads resolution match.** Each ladder rung keeps its own
  equal-resolution (VOD, ads) pair — the existing invariant now applies per
  rung instead of per channel.
- **One shared local `PtsOffset` timeline.** All rungs of the same slot share
  the same `MediaStart`/`PtsOffset` (the planner writes one timeline), so the
  variants are synchronized and players can switch rungs at segment
  boundaries seamlessly. The 2^33/90000 wrap avoidance is untouched.
- **Bounded on-disk window per rung.** Disk budget becomes
  `N × (keepBehind + PlaylistWindowSize + downloadAhead)` where N = active
  rungs (M1: 15 files total).

**M2 (follow-up, "we expect 3: 360p, 720p, 1080p").** `resolveRenditions`
fills the whole ladder (3 pairs per stream instead of 1), members are written
per rung, `simlive:renditions:{ch}` carries 3 entries, the engine loops the
`SIMLIVE_RENDITIONS` list (3 dirs, 3 media playlists, 3-variant master). M1's
detach/map is the exact seam M2 expands — no structural change.

---

## 3. What Changed (by file)

### `internal/models/models.go`

| Area | Change |
|------|--------|
| `SegmentItem` (`#L22-L44`) | Add `Rendition string \`json:"rendition,omitempty"\`` — empty = legacy = engine default `360p` |

### `internal/planner/planner.go`

| Area | Change |
|------|--------|
| New const | `renditionMetadataKey = "simlive:renditions:%s"` |
| New type | `RenditionMeta{ Rendition, VodDir, AdsDir string; Width, Height int; Bandwidth int64 }` (JSON member of the metadata key) |
| New helper `activeRenditions()` | Reads `SIMLIVE_RENDITIONS` (comma list). Unset → single rung = `SIMLIVE_QUALITY` tier label, fallback `360p`. M1: `len > 1` → WARN + use first (M2 lifts this) |
| `resolveRenditions` (`#L315` → actual ~307) | Keep signature & existing `rendCache map[string]renditionPair` (legacy single-pair cache). Add a **second** cache alongside: `ladderCache map[string]map[string]renditionPair` keyed `streamID → tierLabel → pair`. M1 fills `ladderCache[streamID][activeTier]` only. Both caches coexist — do not collapse `rendCache` |
| `resolveRenditionsFor(streamID, tier)` (new thin wrapper) | Returns `ladderCache[streamID][tier]`, probing + populating on miss. Calls `probeRenditions` only once per `(streamID, tier)`. The `segmentProcessor` caller is `resolveRenditionsFor(streamID, sp.currentRendition)` (NOT the legacy `resolveRenditions(streamID)`) so `ladderCache` keys on the active tier |
| `resolveAdsRendition` (`#L270`) | Refactor core into `resolveAdsRenditionForTier(tier) string` with `adsRendByTier map[string]string` cache; keep `resolveAdsRendition()` as a wrapper for the active tier (used by `loadAdSegments` `#L489`) |
| New `probeBitrate(remotePath)` | Primary path: ffprobe `-show_entries format=bit_rate` on the first VOD segment (15s timeout, same failure semantics as `probeResolution`). **Fallback only on probe failure** — return `clamp(w*h*0.08, 100_000, 20_000_000)` from a pure helper `probeBitrateFallback(w, h int) int64`. The fallback under-estimates high-profile encodes, so prefer the probed value; the clamp keeps master `BANDWIDTH` sane if the probe returns a corrupt value. Factor the ffprobe stdout parsing into `parseBitrateStdout([]byte) (int64, error)` for unit testing |
| New `writeRenditionMetadata(ctx, chID)` | Builds `[]RenditionMeta` for the active rung(s) from `ladderCache` + probes; **single** `SET simlive:renditions:{ch} <json> EX 86400` (no pipeline). Called from `ProcessEPGToTimeline` as the **last line before `return p.flushRedisPipeline(...)`** (call site is `planner.go:~184`) so the planner tick ends with metadata persisted — one `SET` per `(channel, date)`, no batching needed |

### `internal/planner/segment_processor.go`

| Area | Change |
|------|--------|
| `SegmentProcessor` | New field `currentRendition string`, set from `sp.planner.activeRenditions()[0]` at the top of `processStreamSegments` (actual `#L43`); the processor then calls `sp.planner.resolveRenditionsFor(streamID, sp.currentRendition)` (not `resolveRenditions(streamID)`) so `ladderCache` keys on the active tier |
| `processSegment` (`#L140` → actual `#L128`) | Set `Rendition: sp.currentRendition` on the member |
| `insertAdsIfNeeded` (`#L175` → actual `#L161`) | Ad members inherit `Rendition: sp.currentRendition` (ads dir already comes from the tier via `resolveAdsRenditionForTier`) |
| Legacy timeline flush | First planner tick after M1 ships must retag (or evict) members in `simlive:timeline:{ch}` that lack `Rendition`. Simplest: `prepareDate` calls a new `hasLegacyTimeline(ch)` (one-line check on the first ZSET member's `Rendition` field); if true, `DeleteTimeline(ch)` (exported wrapper) `DEL`s the key and lets `ProcessEPGToTimeline` repopulate with tags. Matches the spirit of the "planner rebuild flushes Redis" caveat documented in the summary instructions' post-task rule |

### `internal/engine/engine.go`

| Area | Change |
|------|--------|
| Const | `DefaultRendition = "360p"` |
| `Engine` fields | `rendMetaCache map[string][]RenditionMeta` (channel → 3-rung list) loaded lazily from Redis `simlive:renditions:{ch}` on first encounter; `rendMetaMu` guards the cache. NO per-engine `renditions []string` — the engine uses the hard-coded `ActiveLadder` (360p/720p/1080p) when the Redis key is absent (cold start before the planner has run) |
| `channelState` key | `states` map keyed `channelID` only (M1.5 shared segment cache = single state per channel; the M1 `{ch}:{rend}` keying was reverted because one set of files = one window) |
| `StartPlayoutLoop` | M1.5 SHARED CACHE: after unmarshal + sort, walks the FULL flat segment list once (no per-rung split). `groupSegsByRendition` is still called for legacy `Rendition` rescue (empty → `DefaultRendition = "360p"`) but the actual download/GC/playlist emission is a single pass against the channel root. `activeRungMeta(ctx, chID)` returns the ladder (planner metadata → `ActiveLadder` fallback) and drives the 3 per-rung media playlist emissions + the master |
| `downloadAndPrepareSegments` / `prepareSegment` | Called ONCE per tick with the channel-level `downloadSegs` (single set of `seg_N.ts` for all 3 rungs). `inFlight` keys are still full paths but the path is just `{channelDir}/seg_N.ts` (no `{rend}/` subdir). `prepareSegment` now also does a `MkdirAll` of `channelDir` (defensive) + a post-`CopyTo` `Stat` verify to close a race where `syncLocalCacheDir` from the next tick could GC the file between Stat and ffmpeg |
| `generateHLSPlaylist` | Signature → `(ctx, channelID, rendition, segments)`; the `.pts` marker filter checks `{channelDir}/seg_N.ts.pts` (channel root, NOT a `{rend}/` subdir — all rungs share the same markers). Writes `{channelDir}/playlist_<rend>.m3u8` (e.g. `playlist_360p.m3u8`) + Redis `simlive:hls:{ch}:rendition:{rend}` (24h TTL). Each playlist references the SAME `seg_N.ts` files |
| `generateMasterPlaylist(ctx, channelID, meta)` | Called every tick AFTER the per-rung playlist loop; **always writes** `{channelDir}/playlist.m3u8` + Redis `simlive:hls:{ch}` even when the candidate window is empty (player reload never 404s — uses the supplied `meta` list as the ladder source). Rungs with `width=0` or `bandwidth=0` are skipped. Includes `#EXT-X-INDEPENDENT-SEGMENTS` (free; helps hls.js variant switching — promoted from M2 to M1) |
| `syncLocalCacheDir` | FREE FUNCTION (not a method) called ONCE per tick on the channel root: `syncLocalCacheDir(channelDir, activeMap)`. Preserves `playlist.m3u8` + `playlist_<rend>.m3u8` files (they're not `.ts`/`.ts.pts`); GC's stale `seg_N.ts` + `.pts` markers. The active set is keyed on `seg_N.ts` (no rung prefix). `inFlight` keys (full dest path) are channel-rooted, so per-rung dedupe is no longer needed |

### `cmd/api/main.go`

| Area | Change |
|------|--------|
| `GET /live/:channel_id/playlist.m3u8` (`#L58`) | Unchanged route — now serves the **master** playlist (engine writes `simlive:hls:{ch}` as master). Falls back to on-disk `{TMP_DIR}/{ch}/playlist.m3u8` when Redis is cold |
| New route | `GET /live/:channel_id/playlist_:rendition` → Redis `simlive:hls:{ch}:rendition:{rend}`, fallback disk `{TMP_DIR}/{ch}/playlist_{rend}.m3u8`; same `..`/`/` guards on **both** params; the `:rendition` Gin wildcard captures the value BEFORE the literal `.m3u8` suffix (e.g. `playlist_360p.m3u8` → `rend="360p"` after `strings.TrimSuffix(".m3u8")`); reject any `rend` that isn't `360p`/`720p`/`1080p` with `404` (NOT `400` — typo'd rung names return "Unknown rendition" not "Invalid path") |
| Segment route | `GET /live/:channel_id/:segment` (M0 2-param, unchanged shape) serves `seg_N.ts` from the channel root. No per-rung subdir under M1.5. The M1 3-param `/:rendition/:segment` was removed because the shared cache means all rungs reference the same files |

**Gin radix-tree note:** the rung-playlist route is registered BEFORE the
2-param segment route in `cmd/api/main.go` so it takes precedence. The
`playlist_:rendition` shape (wildcard before the `.m3u8` literal) is the
M1.5 alternative to `/{ch}/{rend}/playlist.m3u8`, which would conflict
with `/{ch}/{segment}` at depth 2 (Gin refuses two distinct wildcard
names at the same depth). The `playlist_` prefix also removes the need
for the `rendition == "playlist"` reject that the M1 design needed.

### Docs (post-task, §7)

| File | Section |
|------|---------|
| `docs/specification.md` | §5 Module 2 steps 4–6 (per-rung dirs, master playlist), §6.1 (per-rung pair + ladder note), §7 diagram, §8 env table |
| `.github/instructions/simlive-summary.instructions.md` | Architecture snapshot (master playlist + per-rung dirs), env var line, invariant wording |

---

## 4. Data Flow

```text
[ESPX EPG]
   │
   ▼
[Planner]
   ├─ resolveRenditions(streamID) ──► tier ladder {360p:(L_0,L2), 720p:(L_1,L1), 1080p:(L_2,L0)}   (M1: 1 rung)
   ├─ rclone cat {stream}/{vodRend}/vod.m3u8 (+ ads {adsRend}) ──► SegmentItem{Rendition, PtsOffset, MediaStart, …}
   ├─ ZADD simlive:timeline:{ch}        (score = MediaStart — SAME local timeline across rungs)
   └─ SET  simlive:renditions:{ch}      (JSON [{rendition, vod_dir, ads_dir, width, height, bandwidth}])
                          │
                          ▼
[Engine] 1s tick
   ├─ ZRangeByScore [now−300, now+120] ──► group by seg.Rendition
   ├─ per-rung {TMP_DIR}/{ch}/{rend}/
   │    ├─ seg_{seq}.ts (+ .pts)   ← rclone copyto → ffmpeg -c copy -copyts -output_ts_offset (local PtsOffset)
   │    └─ playlist.m3u8 (media, 5 segs, prepared only)   + SET simlive:hls:{ch}:{rend}
   └─ playlist.m3u8 (MASTER: #EXT-X-STREAM-INF per rung)  + SET simlive:hls:{ch}
                          │
                          ▼
[API] :8080
   GET /live/{ch}/playlist.m3u8          ──► master (Redis simlive:hls:{ch})
   GET /live/{ch}/{rend}/playlist.m3u8   ──► media  (Redis simlive:hls:{ch}:{rend} / disk)
   GET /live/{ch}/{rend}/seg_{seq}.ts    ──► file   ({TMP_DIR}/{ch}/{rend}/seg_{seq}.ts)
                          │
                          ▼
                   [hls.js player]
```

---

## 5. Configuration / Env Vars / Constants

| Name | Default | Description |
|------|---------|-------------|
| `SIMLIVE_RENDITIONS` | unset | Comma-separated ladder, e.g. `360p,720p,1080p`. **M1: exactly 1 allowed** (WARN + use first if more). Unset → single rung = `SIMLIVE_QUALITY` tier, fallback `360p`. Parsed result is cached on the `Planner` (or behind `sync.Once`) so the WARN log fires **once per process**, not per tick × per channel |
| Redis `simlive:renditions:{ch}` | — | New per-channel ladder metadata JSON (rendition, vod/ads dirs, w×h, bandwidth); engine reads it to build the master playlist |
| Redis `simlive:hls:{ch}` | — | **Semantics change:** now holds the **master** playlist (was the media playlist). Only consumer is the API route `#L58` — verified |
| Redis `simlive:hls:{ch}:{rend}` | — | New per-rung media playlist |
| `SIMLIVE_QUALITY`, `SIMLIVE_VOD_RENDITION`, `SIMLIVE_ADS_RENDITION` | existing | Unchanged — still select the **active** rung in M1 |

No other env-var changes. `SIMLIVE_KEEP_BEHIND` / `SIMLIVE_DOWNLOAD_AHEAD`
stay per-rung (disk budget N×15).

---

### 6.0 Unit Tests

Pure-Go unit tests gate the new logic. Tests that touch Redis skip themselves
when `REDIS_ADDR` is unreachable (pattern from `horizon_rollover_test.go`).
**No test artifacts in `./outputs`** — the engine wipes `TMP_DIR` on startup.

| File | Test | Asserts |
|------|------|---------|
| `internal/planner/rendition_test.go` (new) | `TestActiveRenditions_DefaultsToSIMLIVEQUALITY` | `SIMLIVE_RENDITIONS` unset → single entry = label of `SIMLIVE_QUALITY`, fallback `360p` |
| | `TestActiveRenditions_ParsesCommaList` | `"720p"` → `[]string{"720p"}`; whitespace trimmed; empty entries dropped |
| | `TestActiveRenditions_WarnsOnMultipleRungs` | `"360p,720p"` → returns `["360p"]`; emits exactly one WARN log line (capture via `log.SetOutput` to a `bytes.Buffer`); second call does NOT emit another WARN (sync.Once) |
| | `TestResolveRenditionsFor_ReusesProbes` | Calling twice for the same `(streamID, tier)` does NOT call `probeRenditions` a second time. Inject a counting `probeFunc` field on `Planner` for the test (default `p.probeRenditions`); assert the counter |
| | `TestProbeBitrateFallback_Formula` | `probeBitrateFallback(640, 360) == clamp(640*360*0.08, 100_000, 20_000_000)`; values outside the clamp range are bounded (pure helper) |
| | `TestParseBitrateStdout_ParsesFfprobe` | `parseBitrateStdout([]byte("1234567\n"))` → `1234567, nil`; trailing whitespace tolerated; empty input → error; non-numeric → error |
| | `TestWriteRenditionMetadata_SerializesJSON` | Output JSON has `rendition`, `vod_dir`, `ads_dir`, `width`, `height`, `bandwidth` for the active rung(s); JSON re-marshals to identical content |
| | `TestRenditionCache_CoexistsWithLadderCache` | `rendCache` (legacy single-pair) and `ladderCache` (per-tier) both populated; `resolveRenditionsFor(streamID, tier)` reads from `ladderCache`; `resolveRenditions(streamID)` reads from `rendCache`; legacy callers bypass cleanly |
| `internal/models/models_test.go` (new — currently none) | `TestSegmentItem_Rendition_OmitsWhenEmpty` | `Rendition: ""` → JSON does NOT contain `"rendition"` key (`omitempty` contract) |
| | `TestSegmentItem_Rendition_Roundtrip` | Marshal → unmarshal preserves `Rendition: "720p"` |
| | `TestSegmentItem_LegacyUnmarshal_NoRenditionField` | JSON without `rendition` key decodes to `Rendition: ""` (the engine's `DefaultRendition` rescue path) |
| `internal/engine/master_playlist_test.go` (new) | `TestGenerateMasterPlaylist_Format` | Output begins with `#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-INDEPENDENT-SEGMENTS\n`; one `#EXT-X-STREAM-INF:BANDWIDTH={bw},RESOLUTION={w}x{h}` per active rung; rung order = ladder order |
| | `TestGenerateMasterPlaylist_SkipsIncompleteRungs` | Rung with `width=0` or `bandwidth=0` is omitted from `STREAM-INF` lines |
| | `TestGenerateMasterPlaylist_RefsRelativePath` | Stream URI = `{rend}/playlist.m3u8` (no leading slash, no `http://`); matches what the API route serves |
| | `TestGenerateMasterPlaylist_WritesEvenWhenNoCandidates` | Called with no recent candidates → still writes the master using last-known rung metadata (player reload must not 404) |
| | `TestGenerateHLSPlaylist_PerRungDir` | `generateHLSPlaylist(ctx, chID, "360p", segs)` writes to `{TMP_DIR}/{ch}/360p/playlist.m3u8` and sets `simlive:hls:{ch}:360p` (NOT `simlive:hls:{ch}`); `.pts` marker filter checks `{rend}/seg_N.ts.pts` |
| | `TestGroupSegsByRendition_Defaults` | `[]SegmentItem{{Rendition:""}}` → one group keyed `360p` (`DefaultRendition`); preserves sequence order within group |
| | `TestGroupSegsByRendition_MultiRung` | Two renditions (`360p` + `720p`) → two groups; sequence order preserved within each group; total segments equal input length (no segment dropped) |
| | `TestSyncLocalCacheDir_PerRung` | Channel root has subdir `360p/` with one stale `.ts` not in `activeMap` → that file is removed; `720p/` subdir is left alone (its own `activeMap` is the caller's responsibility) |
| `internal/api/handler_test.go` (extend or new — `internal/api/` currently has no `_test.go`) | `TestPlaylistGuard_RejectsPlaylistAsRendition` | `GET /live/{ch}/playlist/playlist.m3u8` → `400` |
| | `TestPlaylistGuard_RejectsDotDot` | `GET /live/{ch}/360p/../playlist.m3u8` → `400` (verify Gin's URL normalization; if Gin collapses `..` before the handler, add an explicit raw-path check) |
| | `TestPlaylistGuard_RejectsSlashInRendition` | `GET /live/{ch}/360p/sub/playlist.m3u8` → `404` (no matching route) |
| | `TestSegmentGuard_RejectsNonTS` | `GET /live/{ch}/360p/seg_1.exe` → `404` |

Run with:

```bash
GOCACHE=$PWD/.gocache go test ./internal/planner/... ./internal/engine/... ./internal/models/... ./internal/api/... -run '^Test'
```

The existing tests (`horizon_rollover_test.go`, `retry_breaker_test.go`)
already establish the skip-on-no-Redis pattern; new tests follow the same
convention (use `testing.Short()` or a small `requireRedis(t)` helper).
The new `models_test.go` and `master_playlist_test.go` have **no** Redis
dependency at all and run unconditionally.

---

## 6. Verification Plan

> Planner rebuild flushes Redis and wipes `./outputs` — run the planner
> **on demand** only, and never leave artifacts in `./outputs`.

1. **Build**
   ```bash
   GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...
   ```
   Clean.

2. **Planner (on demand).** Start planner once. Then:
   - `redis-cli -p 6371 GET simlive:renditions:{ch}` → 1-entry JSON with
     `rendition`, `vod_dir`, `ads_dir`, `width`, `height`, `bandwidth`.
   - `redis-cli -p 6371 ZRANGE simlive:timeline:{ch} 0 0` → member carries
     `"rendition":"360p"` (or the configured tier); VOD and ad members both
     tagged.

3. **Engine.** With `SIMLIVE_RENDITIONS` unset:
   - Logs: `grep -E "\[rendition=|master"` shows per-rung lines.
   - `ls {TMP_DIR}/{ch}/` → `playlist.m3u8` (master) + `360p/` subdir.
   - `ls {TMP_DIR}/{ch}/360p/` → `playlist.m3u8` + ≤15 `seg_N.ts` + `.pts`.

4. **HTTP mapping (the "map correct" check).**
   - `curl -s http://localhost:8080/live/{ch}/playlist.m3u8` → `#EXTM3U` +
     one `#EXT-X-STREAM-INF:BANDWIDTH=…,RESOLUTION=…` + `360p/playlist.m3u8`.
   - `curl -s http://localhost:8080/live/{ch}/360p/playlist.m3u8` → media
     playlist (5 segs, `#EXT-X-MEDIA-SEQUENCE` matches).
   - `curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/live/{ch}/360p/seg_N.ts` → `200`.
   - Path-guard regression: `…/../360p/playlist.m3u8` → `400`.

5. **Boundary DTS (invariant).** `ffprobe` `dts_time` of the first video
   packet of adjacent VOD↔ads segments at 360p → continuous (gap < 1 frame),
   same as the `probe/result.txt` check on the current single-rendition path.

6. **Player probe.** `probe/probe_server.py` + `probe/player-test.html` —
   hls.js loads the master, follows `360p/playlist.m3u8`, plays with no
   404s / `bufferStalledError`.

7. **Cleanup bound.** With `SIMLIVE_KEEP_BEHIND=5`, `SIMLIVE_DOWNLOAD_AHEAD=5`:
   `ls {TMP_DIR}/{ch}/360p/ | grep -c '\.ts$'` ≤ 15 after a few ticks.

Success metrics: master serves 1 `STREAM-INF`; media playlists serve prepared
segments only; per-rung on-disk window ≤ 15; playback continuous at 360p.

---

## 7. Known Artifacts / Remaining Work

- **M2 — full ladder (360p/720p/1080p) is NOT in M1.** Planner fills
  `ladderCache` for all 3 tiers, engine loops `SIMLIVE_RENDITIONS`, master
  gets 3 `STREAM-INF` entries. Requires per-rung ad insertion
  (`resolveAdsRenditionForTier` already shapes this).
- **`BANDWIDTH` is probed/estimated, not read from upstream.** The per-rung
  `vod.m3u8` carries no `#EXT-X-STREAM-INF`; M2 could parse a top-level master
  manifest if one exists on storage (`{streamID}/vod.m3u8`).
- **"320p" vs 360p.** The ladder is 360p/720p/1080p today. A true 320p rung
  needs a new `L_*` dir + a `qualityTierMap` row (planner-only).
- **Legacy Redis timelines** (no `rendition` field): first planner tick after
  upgrade detects them via a new `hasLegacyTimeline(ch)` (one-line check on
  the first ZSET member's `Rendition` field), `DEL`s `simlive:timeline:{ch}`,
  and lets `ProcessEPGToTimeline` repopulate with tags. Engine's startup
  cleanup wipes `{TMP_DIR}/{ch}` so legacy flat-dir files vanish at the same
  time. Players reload once.
- **`CODECS` on the master** is left for M2 (per-rung CODECS strings need a
  probe that M1 doesn't do). **`#EXT-X-INDEPENDENT-SEGMENTS` IS in M1`**
  (one-line addition, helps hls.js variant switching without re-probing).
- **API behavior change:** `simlive:hls:{ch}` now holds the master, so any
  external consumer of that raw key sees a different payload (only the API
  route reads it — confirmed in `cmd/api/main.go`).
- **`rendition == "playlist"` guard.** The 2-param `/live/{ch}/{segment}`
  route and the 3-param `/live/{ch}/{rend}/playlist.m3u8` route are disjoint
  in Gin's router, but a request like `/live/{ch}/playlist/playlist.m3u8`
  would otherwise match the 3-param route with `rendition="playlist"` and
  try to serve `{ch}/playlist/playlist.m3u8`. Explicit `400` on this input
  (covered by `TestPlaylistGuard_RejectsPlaylistAsRendition` in §6.0).

---

## References

- `docs/specification.md` §5 (Module 2), §6.1 (quality tiers), §7, §8 (env vars).
- `docs/270826-implement-pts-sliding-window-logic.md` (PtsOffset + quality
  tiers — this plan extends its rendition logic).
- `docs/290826-implement-prepare-ts-file.md` (engine window — this plan
  detaches it per rung).
- `.github/skills/simlive-plan/references/simlive-conventions.md`
  (invariants §1–§3).
