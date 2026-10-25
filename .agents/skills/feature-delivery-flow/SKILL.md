---
name: "feature-delivery-flow"
description: "Standardized end-to-end flow for delivering any feature from a raw request: intake → specify → clarify → plan → tasks → implement → analyze → review → verify, with gates, resume, and a final compare-all verification."
metadata:
  author: "simlive"
  source: "requests/"
---

# Feature Delivery Flow Skill

Standardized, resumable, gated flow for turning any raw request into a verified implementation. Single source of truth: the request file under `requests/`.

## When to Use

- A new partner feedback / raw request arrives.
- Resuming an in-progress request (read `CURRENT_FLOW` from its request file).
- Re-running verification against a completed request.

## Prerequisites

- Request file exists under `requests/` in the standard template format (see `.specify/templates/request-template.md`).
- Speckit skills available: `speckit-specify`, `speckit-clarify`, `speckit-plan`, `speckit-tasks`, `speckit-implement`, `speckit-analyze`.
- Verification skill exists or will be created under `.agents/skills/<slug>-verify/`.

## Inputs

- `request_file`: path to the request markdown file (e.g. `requests/231026-partner-feedback-number-version-pmt-static.md`).

## Flow

Read the request file first. Note `STATUS` and `CURRENT_FLOW`. Jump to the matching step. If `STATUS == done`, go to Step 8 (re-verify).

### Step 0 — Intake (STATUS: intake)

1. If the input is raw text (not a request file), scaffold a new request file from `.specify/templates/request-template.md` under `requests/<date>-<slug>.md`.
2. Fill frontmatter (`DATE`, `SLUG`, `BRANCH`), `RAW REQUEST`, and a first-pass `SUMMARY` + `EXPECTED DONE`.
3. Create a verification skill stub from `.specify/templates/verify-skill-template.md` at `.agents/skills/<slug>-verify/SKILL.md`.
4. Add the request to `requests/README.md` index.
5. Update `STATUS: specify`, `CURRENT_FLOW: speckit-specify`, append LOGS entry.
6. **GATE**: confirm intake summary with user before specifying.

### Step 1 — Specify (GATE) (STATUS: specify)

1. Run `speckit-specify` seeded from the request's SUMMARY + EXPECTED DONE (acceptance criteria become user stories).
2. Review the generated `spec.md` with the user.
3. **GATE**: user approves spec → set `STATUS: clarify`, `CURRENT_FLOW: speckit-clarify`, append LOGS. On reject, revise and re-gate.

### Step 2 — Clarify (OPTIONAL) (STATUS: clarify)

1. If OPEN QUESTIONS has any `PENDING`, run `speckit-clarify` to resolve them; record answers in the request file.
2. If no pending questions, ask user whether to skip; if yes, skip.
3. **GATE (soft)**: all OPEN QUESTIONS must be answered (no PENDING) before proceeding.
4. Set `STATUS: plan`, `CURRENT_FLOW: speckit-plan`, append LOGS.

### Step 3 — Plan (GATE) (STATUS: plan)

1. Run `speckit-plan`.
2. Review plan against constitution and spec.
3. **GATE**: user approves plan → set `STATUS: tasks`, `CURRENT_FLOW: speckit-tasks`, append LOGS.

### Step 4 — Tasks (GATE) (STATUS: tasks)

1. Run `speckit-tasks`.
2. Ensure a final "Verification against partner request" task exists mapping ACs to evidence.
3. **GATE**: user approves task list → set `STATUS: implement`, `CURRENT_FLOW: speckit-implement`, append LOGS. Do NOT run speckit-implement yet.

### Step 5 — Implement (GATE) (STATUS: implement)

1. **GATE**: confirm with user "Proceed to speckit-implement now? (code changes will begin)" before running. On confirm, continue; on decline, stay in tasks.
2. Run `speckit-implement`.
3. Set `STATUS: analyze`, `CURRENT_FLOW: speckit-analyze`, append LOGS.

### Step 6 — Analyze (GATE) (STATUS: analyze)

1. Run `speckit-analyze`.
2. Resolve any CRITICAL/HIGH findings; re-run if needed.
3. **GATE**: user approves analysis → set `STATUS: review`, `CURRENT_FLOW: speckit-review`, append LOGS.

### Step 7 — Review (GATE) (STATUS: review)

1. Code review against constitution + spec acceptance scenarios.
2. **GATE**: user approves → set `STATUS: verify`, `CURRENT_FLOW: verify`, append LOGS.

### Step 8 — Verify (GATE) (STATUS: verify)

1. Run the per-request verification skill (`.agents/skills/<slug>-verify/SKILL.md`).
2. Compare implementation vs partner request: every AC in EXPECTED DONE must be verified with evidence.
3. Produce `verification-report.md` under the spec dir using `.specify/templates/verification-report-template.md`.
4. **GATE**: verdict must be PASS and all AC checkboxes checked. On FAIL, fix and re-verify.
5. On PASS: set `STATUS: done`, `CURRENT_FLOW: done`, append LOGS; update request index.

## Resume / Status Check

- Read `STATUS` and `CURRENT_FLOW` from the request file; jump to that step.
- To check all requests: scan `requests/` and report each request's SLUG, STATUS, CURRENT_FLOW, and the next step to run.
- Suggest the next command based on CURRENT_FLOW:
  - intake → scaffold request (Step 0)
  - specify → run speckit-specify
  - clarify → run speckit-clarify (or skip with confirmation)
  - plan → run speckit-plan
  - tasks → run speckit-tasks
  - implement → confirm, then run speckit-implement
  - analyze → run speckit-analyze
  - review → code review
  - verify → run verification skill
  - done → re-verify if needed
- Never skip a GATE; if a gate was not recorded as passed, re-run it.

## Status Transitions & Logging

After every step, update the request file:
- `STATUS` → new value
- `CURRENT_FLOW` → next step id
- `LAST UPDATE` → today
- Append to `LOGS`: `- [YYYY-MM-DD HH:MM] <step>: <what changed>`

## Linking Convention

Ensure bidirectional links exist:
- Request file `LINKS` section ↔ spec/plan/tasks headers ↔ skill `## Source Request` ↔ verification report.

## Done Criteria

A request is `done` iff:
- Verification report verdict == PASS
- All AC checkboxes in EXPECTED DONE are checked
- No OPEN QUESTION is PENDING
- `STATUS == done`

## Notes

- **GATE means: at the end of the step, stop and wait for explicit user confirmation before transitioning to the next step. Never auto-proceed past a GATE.**
- Gates are mandatory; OPTIONAL steps may be skipped only with explicit user confirmation.
- Open questions must be solved (answered) before leaving clarify.
- All flow documents must link together; broken links fail the verify step.
