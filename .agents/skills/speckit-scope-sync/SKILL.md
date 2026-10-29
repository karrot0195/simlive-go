---
name: "speckit-scope-sync"
description: "Capture requests raised during implementation that are not covered by spec.md, plan.md, or tasks.md, sync them back into those artifacts, or advise creating a new spec when totally out of scope. Use when an implementation request seems unspecified or out of scope."
compatibility: "Requires spec-kit project structure with .specify/ directory"
metadata:
  author: "local"
  source: "custom"
---

## User Input

```text
$ARGUMENTS
```

You **MUST** consider the user input before proceeding (if not empty).

## Goal

During `/speckit-implement`, when a user asks for something not defined in `spec.md`, `plan.md`, or `tasks.md`, capture it and synchronize it back into the appropriate artifact. If the request is completely out of scope for the current feature, advise the user to run a new `/speckit-specify` instead of silently expanding scope.

## Operating Constraints

- **No silent scope creep**: Do not implement unspecified work without first recording it.
- **Prefer sync over ad-hoc edits**: Append traceable items to `tasks.md` (and note in spec/plan when needed) rather than changing code directly.
- **Out-of-scope boundary**: If the request belongs to a different feature/domain, do not append; recommend `/speckit-specify`.

## Execution Steps

### 1. Load Current Feature Artifacts

Run `python3 .specify/scripts/python/check_prerequisites.py --json --require-tasks --include-tasks` and resolve `SPEC`, `PLAN`, `TASKS`.

### 2. Classify the Request

Compare the user request against the artifacts:

- **In-scope but unspecified**: The request fits the current feature's domain but is not captured in spec/plan/tasks. → Append a task to `tasks.md` under a new "Scope Sync" note, and optionally annotate `spec.md`/`plan.md`.
- **Already covered**: The request maps to an existing task/requirement. → Point to that task; no changes.
- **Totally out of scope**: The request belongs to a different feature/domain. → Do not modify artifacts; advise running `/speckit-specify` for a new feature.

### 3. Sync In-Scope Requests

For each in-scope but unspecified request:

1. Append to the end of `tasks.md` a new section `## Scope Sync (captured during implementation)` if not present, with checklist items:
   ```
   - [ ] Txxx <imperative description> (captured request: <short quote>)
   ```
2. Add a short note to `spec.md` Assumptions or a new "Captured Requests" subsection listing the request.
3. Report what was captured and where.

### 4. Handle Out-of-Scope Requests

For totally out-of-scope requests:

- Do not modify artifacts.
- Respond: "This request is outside the current feature's scope. Run `/speckit-specify` to create a new feature spec for it."

### 5. Completion

Report a summary: captured items (with locations) and any out-of-scope recommendations. No code changes.