---
name: simlive-doc-pass
description: 'Run the SimLive-Go end-of-task documentation pass — append a session entry to docs/memories.md and reconcile drift between code, docs/specification.md, .github/instructions/simlive-summary.instructions.md, and /memories/repo/simlive-go.md. Use when the user says "summary what changed / done / progress / improve next", "documentation pass", "sync the docs", "update summary / spec / instructions", "post-task rule", or invokes this skill explicitly after a code change. Triggers: doc pass, sync docs, summary, session log, memories, post-task, reconciliation.'
argument-hint: '[--scope {all|spec|instruction|memories|repo-memory}]'
user-invocable: true
disable-model-invocation: false
---

# SimLive-Go Documentation Pass

Run the **post-task documentation rule** for SimLive-Go. After any code change
in `internal/**` or `cmd/**`, the agent must reconcile the four canonical doc
files so they stay in sync with the code. This skill encapsulates the exact
checklist that was hand-rolled in the 2026-08-30 doc-pass (see
`docs/memories.md`).

## When to Use

Use this skill when the user asks to:

- "Summary what changed / done / progress / improve next on the session."
- "Update `docs/memories.md`, `docs/specification.md`, instructions."
- "Sync the docs / documentation pass / post-task rule."
- "Reconcile drift between code and docs."
- After any implementation completes, as the implicit cleanup step.

**Do NOT** use this skill for:

- Implementation tasks themselves — use the default agent or the
  `simlive-summary.instructions.md` workflow.
- Planning a new feature — use the `simlive-plan` skill instead.
- Quick fixes that touch no docs (no rule applies).

## Inputs

Optional flags (positional args also accepted):

| Flag | Effect |
|------|--------|
| `--scope all` | (default) reconcile all four doc files end-to-end. |
| `--scope spec` | only `docs/specification.md` |
| `--scope instruction` | only `.github/instructions/simlive-summary.instructions.md` |
| `--scope memories` | only `docs/memories.md` (session log) |
| `--scope repo-memory` | only `/memories/repo/simlive-go.md` |

If the user did not specify a scope, default to `--scope all`.

## The Four Canonical Doc Files

| File | Owns |
|------|------|
| `docs/specification.md` | Full architecture spec, env var table, data models, module summaries. |
| `.github/instructions/simlive-summary.instructions.md` | Concise code-synced snapshot, post-task rule, test inventory. |
| `docs/memories.md` | Append-only session log (`## YYYY-MM-DD HH:MM — <title>` headings). |
| `/memories/repo/simlive-go.md` | Cross-session "repo facts" memory — verified-against-code only. |

> **Invariant:** `AGENTS.md`, `HANDOFF.md`, `concept.md` do **not** exist in
> this workspace. Never reference them as if they do. Replace any such stale
> pointer with the canonical triple
> (`docs/specification.md` + `.github/instructions/simlive-summary.instructions.md`
> + `docs/memories.md`).

## Procedure

Run the five steps **in order**. Do not skip the verification step (5) — every
doc drift found but not actioned must be filed under "Notes — drift findings"
in the session entry so the next session can pick it up.

### Step 1 — Read context (parallel)

Read all four doc files plus the touched Go packages so you can verify each
doc claim against the code:

- `docs/specification.md`
- `.github/instructions/simlive-summary.instructions.md`
- `docs/memories.md` (last entry to find the convention)
- `/memories/repo/simlive-go.md` (use the `memory` tool with `command: view`)
- `git status` + `git diff --stat HEAD` to see what is uncommitted
- All modified / added Go files under `internal/**` and `cmd/**`

Do NOT run terminal commands that build / start the planner or engine — they
are destructive (`FLUSHALL` + `./outputs` wipe).

### Step 2 — Verify each doc claim against the code

For every fact claimed by the four doc files, confirm it matches the current
code. Build a mental list of:

- **Confirmed** (no action needed).
- **Drift — code matches, doc is stale** (will be patched in step 4).
- **Drift — doc matches, code is stale** (file a follow-up; the doc-pass
  does NOT modify code, only docs).
- **Ambiguous / aspirational** (cannot verify; file as follow-up).

Special attention to the **known drift hotspots** (verified during the
2026-08-30 pass):

| Hotspot | Common drift |
|---------|--------------|
| PTS rewrite shape | Often described as "two-anchor with continuity lift" but the code is **single-anchor** (`target = seg.PtsOffset → fallback MediaStart → fallback PtsStart`). |
| M1.5 segment cache | Often still described as per-rung `{ch}/{rend}/` subdirs; the actual M1.5 model is a **shared flat `{ch}/` directory** with 3 advertised rungs via master playlist. |
| `SIMLIVE_RENDITIONS` | Often described as "reserved for M2 / ignored"; the actual behaviour is "M1 allows exactly 1 rung, multi-rung values WARN-log once and use first". |
| API shape | Often described as `/{ch}/{rend}/playlist.m3u8`; the actual M1.5 shape is `/{ch}/playlist_<rend>.m3u8` (Gin radix-tree wildcard conflict avoidance). |
| Doc cross-refs | `AGENTS.md` / `docs/290826-chromium-playback-handoff.md` / `docs/290826-testing-guide.md` are referenced in older docs but do not exist on disk. Replace with the canonical triple or the actual existing plan (`docs/300826_0620-multi-rendition-abr-ladder.md`). Plan filenames use the `ddMMyy_HHmm-…` pattern (UTC, date + hour + minute). |

### Step 3 — Append the session entry to `docs/memories.md`

Use this template verbatim:

```markdown
## YYYY-MM-DD HH:MM — <short title>

**Context:** <what we were working on — 1–3 sentences>

**Change:**
- <bullet of work done>

**Files touched:**
- `path/to/file` (created | modified | deleted)

**Notes — verified state vs. doc:**
- <bullet confirming what the docs already said was correct>

**Notes — drift findings remaining (follow-up candidates):**
- <bullet of drift that was NOT actioned in this session>
```

Convention (established 2026-08-30):

- Heading format: `## YYYY-MM-DD HH:MM — <short title>` (UTC; sessions in
  `Asia/Saigon` should use local time).
- "Files touched" must list every modified/created file.
- "Notes" must surface doc drift findings discovered during the session,
  even if not actioned.
- Update `/memories/repo/simlive-go.md` **only when a fact is verified
  against code or docs in this session**; otherwise file a Notes item in
  `docs/memories.md` instead.

### Step 4 — Patch drift in scope

Edit only the files in the requested scope (or all four if `--scope all`):

| File | Typical patches |
|------|-----------------|
| `docs/specification.md` | Architecture sections (§1–§5), Redis keys (§6), env vars (§8), error handling (§9), data models, summary. |
| `.github/instructions/simlive-summary.instructions.md` | "Architecture snapshot", "Quality tiers", "Key env vars", "Operational Details", "Tests", "Post-task rule", "Additional env vars". |
| `docs/memories.md` | Append session entry (do NOT edit prior entries — append-only). |
| `/memories/repo/simories.md` | Replace `str_replace` only when a fact is **verified against code** in this session. Use the `memory` tool, not `replace_string_in_file`. |

For each patch, include 3–5 lines of unchanged context before and after the
target string so the `replace_string_in_file` match is unambiguous. Prefer
`multi_replace_string_in_file` when patching multiple sections of the same
file in one round.

Replace any stale `AGENTS.md` / non-existent doc reference with the canonical
triple (`docs/specification.md` + the summary instruction file +
`docs/memories.md`) or the actually-existing plan file.

### Step 5 — Verify the pass

Re-grep the workspace for known drift markers to confirm the patches landed:

```text
grep_search "AGENTS"        # in *.md — should remain only in historical plans + memories
grep_search "two-anchor"    # in docs/ — should be zero
grep_search "per-rung subdir" # in docs/specification.md — should be zero (M1.5 is shared)
```

Verify the build still passes (in your head — the agent must NOT run
`go build` / `go test` / planner / engine; tell the user to run
`GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...` if any code file
was touched). If only docs were touched, no build verification is needed.

## Output Format

Report:

1. **Session entry location** — `docs/memories.md` heading + lines.
2. **Drift patches applied** — bullet list of (file, section, before → after).
3. **Drift remaining / follow-ups** — bullet list (these go in the session
   entry's "Notes" too).
4. **Manual verification the user should run** — usually none if only docs
   changed; `go build` + `go test` if any code was touched.

## Gotchas — read before editing

- **The `memory` tool, not `replace_string_in_file`, edits `/memories/repo/simlive-go.md`.** The memory file system lives outside the workspace tree.
- **Do not delete or rewrite prior `docs/memories.md` entries.** Append-only by convention.
- **The implementer agent** (`.github/agents/simlive-implementer.agent.md`) already maintains the spec + summary files during implementation. This skill is the **end-of-task reconciliation pass**, not a substitute for the implementer's per-task updates. If the implementer has already updated everything correctly, this skill is a no-op (just append the session entry).
- **Historical plan files** (`docs/290826_1448-…`, `docs/300826_0213-…`, `docs/300826_0214-…`, `docs/300826_0236-…`) may reference the now-nonexistent `AGENTS.md`. **Do not edit historical plan files** — they are a record of what was done at the time. Only edit `docs/memories.md` going forward.
- **`docs/300826_0620-multi-rendition-abr-ladder.md`** is the M1.5 plan and is current — leave it alone unless explicitly asked.

## Examples

User: *"summary what changes, done, progress, what improve next on the session → update to summary changes into file:memories.md → update specification.md and instruction.md if needed → update related docs/* if needed"*

→ Invoke this skill with `--scope all` (default). Read context, verify against
code, append a session entry to `docs/memories.md`, patch any drift found in
the four canonical doc files, report.

User: *"just update docs/specification.md"*

→ Invoke this skill with `--scope spec`. Skip the other files.

User: *"log this session"*

→ Invoke this skill with `--scope memories`. Only append the session entry.

User: *"we just merged M2 — update the summary instructions"*

→ Invoke this skill with `--scope instruction` (after the implementer agent
has already updated the rest). Verify the summary file matches the new M2
shape and patch drift.