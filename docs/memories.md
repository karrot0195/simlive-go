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