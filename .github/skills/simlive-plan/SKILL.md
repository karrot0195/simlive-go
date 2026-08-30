---
name: simlive-plan
description: 'Generate a dated, well-structured feature plan file (./docs/ddMMyy-<purpose>.md) for the SimLive-Go HLS playout system. Use when the user asks to plan, scope, design, or break down a new feature/fix/refactor in the planner, engine, API, models, or storage layers. Triggers: plan, planning, plan file, ddMMyy, design doc, breakdown, scope, RFC, simlive plan, planner change, engine change.'
argument-hint: '<feature-purpose> [--short]'
user-invocable: true
disable-model-invocation: false
---

# SimLive-Go Plan Generator

## When to Use

Use this skill when the user asks to **plan, design, scope, or break down** a
change to the SimLive-Go codebase — whether it's a planner feature, an engine
behaviour tweak, an API endpoint, a model field, a rendition/tier change, a
verification harness update, or a refactor across `internal/**` and `cmd/**`.

Do **not** use this skill for:
- Questions about the codebase (read `AGENTS.md` + `docs/specification.md`).
- Direct implementation tasks (use the default agent or the
  `simlive-summary.instructions.md` workflow).
- Quick fixes / single-line edits (no plan needed).

## Inputs

The skill accepts the following from the user / prompt:
1. **`feature-purpose`** — a short kebab-case slug (e.g. `cold-start-stall-hardening`).
2. **`--short`** *(optional)* — emit the 1-page checklist template instead of
   the full 6-section plan. Default: full plan.

If the user did not supply them, the skill **must ask** via the `ask_questions`
tool with the two questions in §1 of the procedure.

## Outputs

A single file: **`./docs/ddMMyy-<feature-purpose>.md`** (date in UTC, today's
date if not specified), starting with a `## 0. Progress` checklist that the
implementer ticks off as the plan is executed.

Templates are bundled as one-level references:

- [Full plan template](./assets/plan-template-full.md) — 6 sections
  (Overview → Problem → Solution → File changes → Data flow → Verification →
  Known gaps), matches the existing `270826-…` and `290826-…` plans.
- [Short checklist template](./assets/plan-template-short.md) — 1-page
  problem + todo list + verification, for trivial changes.

## Procedure

1. **Gather inputs.**
   - If `feature-purpose` and shape (full/short) are not in the prompt, ask:
     - Q1: "What's the feature slug?" (e.g. `cold-start-stall-hardening`)
     - Q2: "Full plan or short checklist?" (full / short)
   - Confirm output path: `./docs/$(date -u +%d%m%y)-<feature-purpose>.md`
     (default to UTC date; warn if a file with that name already exists).

2. **Pre-flight context (read in parallel).**
   - `docs/specification.md` — canonical spec.
   - `.github/instructions/simlive-summary.instructions.md` — concise snapshot
     (architecture, tiers, env vars, post-task rule).
   - The **two previous plans** in `docs/` (look for `*-implement-*.md`) to
     stay consistent in tone and structure.
   - The relevant package files under `internal/<target>/` (and `cmd/<target>/`,
     `ai-sources/` if the change derives from reference code).

3. **Pick the template.**
   - Full → [assets/plan-template-full.md](./assets/plan-template-full.md)
   - Short → [assets/plan-template-short.md](./assets/plan-template-short.md)

4. **Scaffold the file.** Run the scaffolder so the boilerplate is consistent:

   ```bash
   ./scripts/scaffold-plan.sh <feature-purpose> [full|short]
   ```

   The script prints the absolute path of the created file so the agent can
   follow up with `insert_edit_into_file` for the body.

5. **Fill in the body.** Replace each `[TODO: …]` placeholder with content
   derived from the files read in step 2. Be **specific**:
   - Name functions / files / env vars.
   - Cite line numbers from existing code (use `path#Lstart-Lend`).
   - Reference [references/simlive-conventions.md](./references/simlive-conventions.md)
     for invariants (VOD↔ads resolution match, local `PtsOffset`, bounded
     active window, etc.).
   - Cross-link to the previous plans when the change continues their work
     (e.g. "continues `270826-…pts-sliding-window-logic`").

6. **The `## 0. Progress` section is mandatory.** The skill writes it as the
   first section of every plan file — implementers tick `[ ]` → `[x]` as they
   go. If the user wanted a "no progress tracking" file, they didn't want a
   plan — they wanted a one-shot prompt.

7. **Hand off.** Tell the user:
   - The path of the new plan file.
   - The 2-3 things they should review first (env-var changes, invariant
     breaches, verification steps that touch `./outputs`).
   - Whether the plan needs a manual cross-check against
     `docs/specification.md` §6.1 (quality tiers) or §8 (env vars).

## Conventions Enforced

These rules are non-negotiable for plans in this repo. They are derived from
the existing plans and the summary instructions; full detail lives in
[references/simlive-conventions.md](./references/simlive-conventions.md).

- **English only** in titles, prose, and file names.
- **No terminal commands** that start the planner or engine (destructive:
  planner rebuild flushes Redis + wipes `./outputs`).
- **Always include verification steps** — what logs to grep, what Redis
  commands to run, what boundary DTS to expect.
- **Always name the files** that change (use the `internal/<pkg>/<file>.go`
  table format from the existing plans).
- **Always cite the invariant** if the change touches rendition matching, the
  `PtsOffset` timeline, or the on-disk active window.
- **Filename pattern:** `./docs/ddMMyy-<feature-purpose>.md`
  (UTC date, kebab-case slug).
- **No code blocks for "where to put it"** — name the file and line range
  instead.

## Example Prompts to Try

- `Plan: cold-start-stall hardening for the engine window.`
- `Plan --short: rename SIMLIVE_KEEP_BEHIND default from 5 to 8.`
- `Scope out a verifier binary that checks playlist.m3u8 against the active
  window.`
- `Design doc: per-channel auto-recovery when the timeline Redis key expires.`
- `RFC: 720p end-to-end verification harness.`

## Related Customizations

- [`.github/instructions/simlive-summary.instructions.md`](../../instructions/simlive-summary.instructions.md)
  — always-on summary; plans must keep it in sync after implementation.
- `docs/specification.md` — the spec the plan must cross-check.
- `docs/* md` — all docs that are part of the project.
