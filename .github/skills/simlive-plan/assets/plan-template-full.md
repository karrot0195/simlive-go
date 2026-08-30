<!--
  Full plan template for SimLive-Go feature plans.
  The scaffolder copies this file into ./docs/ddMMyy_HHmm-<purpose>.md and
  replaces the [TODO: …] placeholders. Do not edit in place — edit the
  template.
-->

# [TODO: Feature title]

**Plan version:** 0.1  
**Date:** [TODO: YYYY-MM-DD]  
**Author:** [TODO: name / handle]  
**Status:** Draft · (later: Approved · Implemented · Verified)
90826_1448
> Continues / Supersedes [TODO: link to previous plan if any, e.g. `270826-…`]

---

## 0. Progress

Track the work; tick as you go.

- [ ] Read pre-flight context (`docs/specification.md`, summary instructions,
      previous plan, target package).
- [ ] Drafted §1–§7 below.
- [ ] Reviewed by [TODO: reviewer].
- [ ] Implementation started.
- [ ] Implementation merged.
- [ ] Verification (§6) passed end-to-end.
- [ ] Post-task docs updated (`docs/specification.md` + summary instructions).

---

## 1. Problem Statement

What is broken, missing, or suboptimal today? Include:
- A short narrative of the user-visible symptom.
- The technical cause (cite file:line if known).
- Why the current behaviour is wrong / costly.

---

## 2. Solution Overview

2-3 paragraphs. Cover the **shape** of the fix:
- High-level approach (don't dump code here).
- Which modules own the change.
- Which invariants are preserved (or added) — cross-check
  [references/simlive-conventions.md](../references/simlive-conventions.md).

---

## 3. What Changed (by file)

| File | Change |
|------|--------|
| `[TODO: path/in/pkg.go]` | [TODO: one-liner] |
| `[TODO: another.go]` | [TODO: one-liner] |
| `docs/specification.md` | [TODO: which § changes] |
| `.github/instructions/simlive-summary.instructions.md` | [TODO: which section] |

Use sub-headings for the bigger areas (e.g. `### internal/planner/planner.go`).

---

## 4. Data Flow

A `text` block diagram showing how data moves after the change. Keep it ASCII.

```text
[upstream]
   │
   ▼
[changed component]
   │
   ▼
[downstream]
```

---

## 5. Configuration / Env Vars / Constants

If the change introduces, removes, or renames an env var, constant, or
config field, list it here. Otherwise write "no env-var changes".

| Name | Default | Description |
|------|---------|-------------|
| `[TODO]` | `[TODO]` | [TODO] |

---

## 6. Verification Plan

Numbered, executable steps. Include:
- Build command (`GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...`).
- Logs to grep.
- Redis commands to run.
- Probe steps (only if the change touches playback).
- What "success" looks like (table of metrics, like the existing plans' §6).

---

## 7. Known Artifacts / Remaining Work

Honest list of what the change **does not** fix. Future-work candidates.

---

## References

- [TODO: link to canonical spec section]
- [TODO: link to related plan]
- [TODO: link to external doc]
